package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

type inspectTestReport struct {
	SchemaVersion int
	EntryPath     string
	Query         string
	Identity      struct{ SourceID, AnalysisID string }
	Contract      *struct {
		Symbol      string
		Declaration checkTestPoint
		Signature   struct {
			Parameters []struct {
				Name, Type   string
				TypeIdentity *testTypeIdentity
			}
			ReturnType         string
			ReturnTypeIdentity *testTypeIdentity
		}
		DeclaredAuthority, InferredEffects []string
		EffectCoverage                     struct {
			Status string
			Gap    *struct {
				Reason string
				Source checkTestRange
			}
		}
	}
	Diagnostics []checkTestDiagnostic
}

func TestInspectFunctionContractCLI(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	lib := writeSplashFile(t, dir, "stock.splash", `module inventory
expose reconcile
use std/ai
type Batch { quantity: Int }
fn reconcile(batch: Batch) needs DB.read, AI -> Int {
    return ai.prompt<Int>("classify")
}
fn with_gap(cb: fn() -> Int) needs Net -> Int { return cb() }
`)
	path := writeSplashFile(t, dir, "main.splash", `module main
use stock as local
fn reconcile() -> Int { return 0 }
fn main() { println(reconcile()) }
`)

	report, first := readInspectJSON(t, binary, path, "inventory.reconcile", true)
	if _, next := readInspectJSON(t, binary, path, "inventory.reconcile", true); !bytes.Equal(first, next) {
		t.Fatalf("nondeterministic report:\n%s\n%s", first, next)
	}
	c := report.Contract
	if c == nil || c.Symbol != "inventory.reconcile" || c.Declaration.File != lib || c.Declaration.Line != 5 ||
		len(c.Signature.Parameters) != 1 || c.Signature.Parameters[0].Name != "batch" ||
		c.Signature.Parameters[0].Type != "Batch" || c.Signature.ReturnType != "Int" {
		t.Fatalf("wrong resolved function or signature: %+v", c)
	}
	if !reflect.DeepEqual(c.DeclaredAuthority, []string{"DB.read", "AI"}) ||
		!reflect.DeepEqual(c.InferredEffects, []string{"AI"}) ||
		c.EffectCoverage.Status != "complete" || c.EffectCoverage.Gap != nil {
		t.Fatalf("wrong effect contract: %+v", c)
	}
	report, _ = readInspectJSON(t, binary, path, "inventory.with_gap", true)
	c = report.Contract
	if c == nil || c.EffectCoverage.Status != "incomplete" || c.EffectCoverage.Gap == nil ||
		c.EffectCoverage.Gap.Reason != "unresolved call target" || c.EffectCoverage.Gap.Source.Start.File != lib ||
		c.EffectCoverage.Gap.Source.End == nil || !reflect.DeepEqual(c.InferredEffects, []string{}) {
		t.Fatalf("expected a known empty lower bound and a gap with provenance: %+v", c)
	}
	report, _ = readInspectJSON(t, binary, path, "reconcile", true)
	if report.Contract == nil || report.Contract.Symbol != "main.reconcile" ||
		!reflect.DeepEqual(report.Contract.DeclaredAuthority, []string{}) {
		t.Fatalf("expected the unqualified entry function: %+v", report.Contract)
	}
	report, _ = readInspectJSON(t, binary, path, "local.reconcile", false)
	if report.Contract != nil || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "FUNCTION_NOT_FOUND" {
		t.Fatalf("import alias should not select a contract: %+v", report)
	}
}

func TestInspectJSONUnavailableContract(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	for _, module := range []string{"first.splash", "second.splash"} {
		writeSplashFile(t, dir, module, "module shared\nfn find() -> Int { return 1 }\n")
	}
	ambiguous := writeSplashFile(t, dir, "main.splash", "module main\nuse first as a\nuse second as b\nfn main() {}\n")
	broken := writeSplashFile(t, dir, "broken.splash", "module broken\nfn main() { let n: Int = \"text\" }\n")
	malformed := writeSplashFile(t, dir, "malformed.splash", "module malformed\nfn main( {\n")
	for _, tc := range []struct{ path, query, code, phase string }{
		{ambiguous, "shared.find", "FUNCTION_AMBIGUOUS", "inspection"},
		{broken, "broken.main", "TYPE_MISMATCH", "type"},
		{malformed, "malformed.main", "PARSE_ERROR", "parse"},
		{filepath.Join(dir, "missing.splash"), "missing.main", "SOURCE_READ_ERROR", "input"},
	} {
		report, _ := readInspectJSON(t, binary, tc.path, tc.query, false)
		if len(report.Diagnostics) == 0 {
			t.Fatalf("want %s with no contract, got %+v", tc.code, report)
		}
		hasIdentity := report.Identity.SourceID != "" && report.Identity.AnalysisID != ""
		if hasIdentity != (tc.phase == "inspection") {
			t.Errorf("identity presence should follow capture success: %+v", report)
		}
		for _, diag := range report.Diagnostics {
			if diag.Code != tc.code || diag.Phase != tc.phase || diag.Severity != "error" {
				t.Errorf("want %s/%s error, got %+v", tc.code, tc.phase, diag)
			}
		}
	}
}

func TestInspectIdentitiesAcrossInvocations(t *testing.T) {
	binary := buildSplashCLI(t)
	fixture := func(dir string) string {
		writeSplashFile(t, dir, "left.splash", "module shared\ntype Item { count: Int }\nfn read(x: Item) -> Item { return x }\n")
		writeSplashFile(t, dir, "right.splash", "module shared\ntype Item { label: String }\nfn lookup(x: Item) -> Item { return x }\n")
		writeSplashFile(t, dir, "bridge.splash", "module bridge\nuse left as local\nfn pass(x: local.Item) -> local.Item { return x }\n")
		return writeSplashFile(t, dir, "main.splash", `module app
use left as one
use right as two
use bridge
fn nested(cb: fn(List<one.Item?>) -> Result<one.Item, two.Item>) {}
fn opaque(x: Mystery) {}
fn main() {}
`)
	}
	dir := t.TempDir()
	entry := fixture(dir)
	report, _ := readInspectJSON(t, binary, entry, "shared.read", true)
	alias, _ := readInspectJSON(t, binary, entry, "bridge.pass", true)
	other, _ := readInspectJSON(t, binary, entry, "shared.lookup", true)
	copy, _ := readInspectJSON(t, binary, fixture(t.TempDir()), "shared.read", true)

	identity := report.Identity
	if identity.SourceID == "" || identity.AnalysisID == "" || identity.SourceID == identity.AnalysisID ||
		identity != alias.Identity || identity != other.Identity || identity != copy.Identity {
		t.Fatalf("identity depends on selector or checkout: %+v, %+v, %+v, %+v", identity, alias.Identity, other.Identity, copy.Identity)
	}
	relative := exec.Command(binary, "inspect", "./main.splash", "shared.read", "--json")
	relative.Dir = dir
	out, err := relative.Output()
	var relativeReport inspectTestReport
	if err != nil || json.Unmarshal(out, &relativeReport) != nil || relativeReport.Identity != identity {
		t.Fatalf("relative path changed identity: %v, %s", err, out)
	}

	left := report.Contract.Signature.Parameters[0]
	right := other.Contract.Signature.Parameters[0]
	if left.Type != "Item" || right.Type != "Item" || left.TypeIdentity == nil || right.TypeIdentity == nil ||
		left.TypeIdentity.Kind != "record" || left.TypeIdentity.Path != "left.splash" || left.TypeIdentity.Name != "Item" ||
		right.TypeIdentity.Kind != "record" || right.TypeIdentity.Path != "right.splash" ||
		!reflect.DeepEqual(left.TypeIdentity, report.Contract.Signature.ReturnTypeIdentity) ||
		!reflect.DeepEqual(left.TypeIdentity, alias.Contract.Signature.Parameters[0].TypeIdentity) ||
		!reflect.DeepEqual(left.TypeIdentity, copy.Contract.Signature.Parameters[0].TypeIdentity) {
		t.Fatalf("wrong nominal identities for same-name records and aliases: %+v, %+v", left, right)
	}
	nested, _ := readInspectJSON(t, binary, entry, "app.nested", true)
	wantNested := &testTypeIdentity{
		Kind: "function",
		Parameters: []*testTypeIdentity{{Kind: "list", Arguments: []*testTypeIdentity{
			{Kind: "optional", Arguments: []*testTypeIdentity{left.TypeIdentity}},
		}}},
		ReturnType: &testTypeIdentity{Kind: "result", Arguments: []*testTypeIdentity{left.TypeIdentity, right.TypeIdentity}},
	}
	if got := nested.Contract.Signature.Parameters[0].TypeIdentity; !reflect.DeepEqual(got, wantNested) {
		t.Errorf("wrong nested type identity: got %+v, want %+v", got, wantNested)
	}
	opaque, _ := readInspectJSON(t, binary, entry, "app.opaque", true)
	if got := opaque.Contract.Signature.Parameters[0]; got.Type != "Unknown" || got.TypeIdentity != nil {
		t.Errorf("expected display only for unsupported checked type: %+v", got)
	}
	dependencyOut, err := exec.Command(binary, "callees", entry, "app.main", "--json").Output()
	var dependency inspectTestReport
	if err != nil || json.Unmarshal(dependencyOut, &dependency) != nil || dependency.Identity != identity {
		t.Fatalf("dependency query identity disagrees with inspect: %v, %s", err, dependencyOut)
	}

	lib := filepath.Join(dir, "left.splash")
	src, err := os.ReadFile(lib)
	if err != nil {
		t.Fatal(err)
	}
	writeSplashFile(t, dir, "left.splash", string(src)+"// changed bytes only\n")
	changed, _ := readInspectJSON(t, binary, entry, "shared.read", true)
	if changed.Identity.SourceID == identity.SourceID || changed.Identity.AnalysisID == identity.AnalysisID ||
		!reflect.DeepEqual(changed.Contract.Signature.Parameters[0].TypeIdentity, left.TypeIdentity) {
		t.Fatalf("source edit failed to change the digests or moved the declaration identity: %+v", changed)
	}
}

type testTypeIdentity struct {
	Kind, Name, Path string
	Arguments        []*testTypeIdentity
	Parameters       []*testTypeIdentity
	ReturnType       *testTypeIdentity
}

func readInspectJSON(t *testing.T, binary, path, query string, success bool) (inspectTestReport, []byte) {
	t.Helper()
	command := exec.Command(binary, "inspect", path, query, "--json")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if success {
		if err != nil {
			t.Fatalf("inspect failed: %v, stderr: %s", err, &stderr)
		}
	} else {
		var failure *exec.ExitError
		if !errors.As(err, &failure) || failure.ExitCode() != 1 {
			t.Fatalf("inspect exit error = %v, stderr: %s", err, &stderr)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("inspect wrote to stderr: %s", &stderr)
	}
	var report inspectTestReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid inspect report %q: %v", &stdout, err)
	}
	if report.SchemaVersion != 1 || report.EntryPath != path || report.Query != query || report.Diagnostics == nil {
		t.Fatalf("unexpected inspect report: %+v", report)
	}
	if !success {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil || string(fields["contract"]) != "null" {
			t.Fatalf("failed inspection must include explicit null contract: %s", &stdout)
		}
	}
	return report, stdout.Bytes()
}
