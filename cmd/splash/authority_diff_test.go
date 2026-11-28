package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type testAuthorityReport struct {
	SchemaVersion int
	Base, Head    struct {
		Verdict     string
		Identity    *struct{ AnalysisID string }
		Diagnostics []checkTestDiagnostic
	}
	Comparison *struct {
		DeclaredAuthority struct {
			Status  string
			Changes []struct {
				Function       struct{ Path, Module, Name string }
				Added, Removed []string
				Before, After  *struct{ Line, Column int }
			}
		}
		PublicSignatures struct {
			Complete bool
			Status   string
			Changes  []struct {
				Function      struct{ Path, Module, Name string }
				Kind          string
				Before, After *struct {
					Parameters []struct{ TypeIdentity struct{ Name, Path string } }
				}
			}
		}
	}
}

func runTestAuthorityDiff(t *testing.T, binary, base, head string, wantExit int) (testAuthorityReport, []byte) {
	t.Helper()
	cmd := exec.Command(binary, "authority", "diff", base, head, "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	exit := 0
	if err != nil {
		var process *exec.ExitError
		if !errors.As(err, &process) {
			t.Fatal(err)
		}
		exit = process.ExitCode()
	}
	var report testAuthorityReport
	if exit != wantExit || stderr.Len() != 0 || json.Unmarshal(raw, &report) != nil || report.SchemaVersion != 1 {
		t.Fatalf("authority diff exit %d, want %d: %s, stderr %s", exit, wantExit, raw, stderr.String())
	}
	return report, raw
}

func TestAuthorityDiffPreservesDeclarationOwnershipAndNominalBindings(t *testing.T) {
	binary := buildSplashCLI(t)
	create := func(t *testing.T, selected, grant string) string {
		t.Helper()
		dir := t.TempDir()
		writeSplashFile(t, dir, "books.splash", "module shared\nexpose Item\ntype Item { count: Int }\nfn fetch() needs "+grant+" {}\n")
		writeSplashFile(t, dir, "warehouse.splash", "module shared\nexpose Item\ntype Item { count: Int }\nfn fetch() needs Net {}\n")
		return writeSplashFile(t, dir, "main.splash", "module app\nuse books as left\nuse warehouse as right\nfn receive(item: "+selected+".Item) -> Int { return item.count }\n")
	}
	base, head := create(t, "left", "Net"), create(t, "right", "DB.read")
	report, raw := runTestAuthorityDiff(t, binary, base, head, 0)
	if report.Comparison == nil || report.Base.Verdict != "verified" || report.Head.Verdict != "verified" ||
		report.Base.Identity == nil || report.Head.Identity == nil || report.Base.Identity.AnalysisID == report.Head.Identity.AnalysisID {
		t.Fatalf("wrong verification evidence: %s", raw)
	}
	authority, signatures := report.Comparison.DeclaredAuthority, report.Comparison.PublicSignatures
	if authority.Status != "changed" || len(authority.Changes) != 1 || signatures.Status != "changed" || len(signatures.Changes) != 1 {
		t.Fatalf("expected one authority and one signature change: %s", raw)
	}
	grant := authority.Changes[0]
	if grant.Function.Path != "books.splash" || grant.Function.Module != "shared" || grant.Function.Name != "fetch" ||
		!reflect.DeepEqual(grant.Added, []string{"DB.read"}) || !reflect.DeepEqual(grant.Removed, []string{"Net"}) ||
		grant.Before == nil || grant.After == nil || grant.Before.Line != 4 || grant.After.Line != 4 {
		t.Fatalf("authority was attributed to the wrong declaration: %s", raw)
	}
	signature := signatures.Changes[0]
	if signature.Kind != "modified" || signature.Function.Path != "main.splash" ||
		signature.Function.Name != "receive" || signature.Before == nil || signature.After == nil ||
		len(signature.Before.Parameters) != 1 || len(signature.After.Parameters) != 1 || signature.Before.Parameters[0].TypeIdentity.Path != "books.splash" ||
		signature.After.Parameters[0].TypeIdentity.Path != "warehouse.splash" {
		t.Fatalf("nominal type rebinding was not reported: %s", raw)
	}
	if _, again := runTestAuthorityDiff(t, binary, base, head, 0); !bytes.Equal(raw, again) {
		t.Fatal("authority diff produced nondeterministic output")
	}
}

func TestAuthorityDiffUsesCheckedEffectsAndMarksUnmodeledSignatureEvidence(t *testing.T) {
	binary := buildSplashCLI(t)
	create := func(t *testing.T, alias, grant string, generic bool) string {
		t.Helper()
		dir := t.TempDir()
		writeSplashFile(t, dir, "lib.splash", "module stock\ntype Item { count: Int }\n")
		source := "module app\nuse lib as " + alias + "\nfn get(item: " + alias + ".Item) needs " + grant + " -> Int { return item.count }\n"
		if generic {
			source += "fn identity<T>(value: T) -> T { return value }\n"
		}
		return writeSplashFile(t, dir, "main.splash", source)
	}
	for _, generic := range []bool{false, true} {
		base := create(t, "first", "DB", generic)
		head := create(t, "other", "DB.read, DB.write", generic)
		report, raw := runTestAuthorityDiff(t, binary, base, head, 0)
		if report.Comparison == nil || report.Comparison.DeclaredAuthority.Status != "unchanged" {
			t.Fatalf("equivalent effect contracts must compare equal: %s", raw)
		}
		signatures := report.Comparison.PublicSignatures
		if !generic && signatures.Status != "unchanged" {
			t.Fatalf("alias spelling changed the checked signature identity: %s", raw)
		}
		if generic && (signatures.Status != "indeterminate" || len(signatures.Changes) != 1 ||
			signatures.Changes[0].Kind != "indeterminate" || signatures.Changes[0].Function.Name != "identity") {
			t.Fatalf("unsupported signature comparison must be explicit: %s", raw)
		}
	}
}

func TestAuthorityDiffRequiresBothCapturesToVerify(t *testing.T) {
	binary := buildSplashCLI(t)
	base := writeSplashFile(t, t.TempDir(), "main.splash", "module app\nfn main() {}")
	for _, tc := range []struct{ source, verdict string }{
		{"module app\nredline fn denied() {}\nagent fn report() { denied() }", "rejected"},
		{"module app\nagent fn report(cb: fn() -> Int) -> Int { return cb() }", "indeterminate"},
	} {
		head := writeSplashFile(t, t.TempDir(), "main.splash", tc.source)
		report, raw := runTestAuthorityDiff(t, binary, base, head, 1)
		if report.Base.Verdict != "verified" || report.Head.Verdict != tc.verdict ||
			report.Comparison != nil || len(report.Head.Diagnostics) == 0 {
			t.Fatalf("unverified source yielded a comparison: %s", raw)
		}
	}
	cmd := exec.Command(binary, "authority", "diff", base)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err == nil || len(raw) != 0 || !strings.Contains(stderr.String(), "usage: splash authority diff") {
		t.Fatalf("expected usage failure: %s (%v), stderr %s", raw, err, stderr.String())
	}
}

func TestAuthorityDiffKeepsKnownChangesAlongsideUncertainty(t *testing.T) {
	binary := buildSplashCLI(t)
	const source = `module app
expose %s, identity
fn add() needs Net {}
fn removed() needs DB.read {}
fn identity<T>(value: T) -> T { return value }
`
	base := writeSplashFile(t, t.TempDir(), "main.splash", fmt.Sprintf(source, "removed"))
	head := writeSplashFile(t, t.TempDir(), "main.splash", fmt.Sprintf(source, "add"))
	report, raw := runTestAuthorityDiff(t, binary, base, head, 0)
	if report.Comparison == nil || report.Comparison.DeclaredAuthority.Status != "unchanged" {
		t.Fatalf("visibility is independent of declared grants: %s", raw)
	}
	signatures := report.Comparison.PublicSignatures
	if signatures.Complete || signatures.Status != "changed" || len(signatures.Changes) != 3 {
		t.Fatalf("expected known changes and incomplete evidence: %s", raw)
	}
	kinds := make(map[string]string)
	for _, change := range signatures.Changes {
		kinds[change.Function.Name] = change.Kind
	}
	if !reflect.DeepEqual(kinds, map[string]string{"add": "added", "removed": "removed", "identity": "indeterminate"}) {
		t.Fatalf("wrong public signature changes: %s", raw)
	}
}
