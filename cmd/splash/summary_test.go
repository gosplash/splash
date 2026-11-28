package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryCLI(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "local.splash", `module library
type Item { count: Int }
fn evaluate(item: Item, cb: fn() -> Int) needs DB.read -> Int { return cb() }
`)
	path := writeSplashFile(t, dir, "app.splash", "use local as alias\nfn main() {}\n")
	read := func(args ...string) ([]byte, summaryTestReport) {
		t.Helper()
		cmd := exec.Command(binary, append([]string{"summary", path, "--json"}, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		var report summaryTestReport
		if err != nil || stderr.Len() != 0 || json.Unmarshal(out, &report) != nil {
			t.Fatalf("summary %v: %v, stdout %s, stderr %s", args, err, out, &stderr)
		}
		if report.SchemaVersion != 1 || report.EntryPath != path || report.Identity == nil ||
			report.Identity.SourceID == "" || report.Identity.AnalysisID == "" || report.Result == nil ||
			report.Diagnostics == nil || len(report.Diagnostics) != 0 {
			t.Fatalf("unexpected envelope: %s", out)
		}
		return out, report
	}

	first, full := read("--module", "library", "--limit", "100")
	second, _ := read("--limit", "100", "--module", "library")
	if !bytes.Equal(first, second) {
		t.Fatalf("option order affected output: %s vs %s", first, second)
	}
	if full.Result.Module != "library" || full.Result.SourcePath != "local.splash" ||
		full.Result.TotalDeclarations != 2 || full.Result.OmittedDeclarations != 0 || len(full.Result.Declarations) != 2 {
		t.Fatalf("unexpected module result: %s", first)
	}
	if bytes.Contains(first, []byte(`"file":`)) || bytes.Contains(first, []byte(`"body":`)) {
		t.Fatalf("summary leaked repeated file paths or bodies: %s", first)
	}
	record, fn := full.Result.Declarations[0], full.Result.Declarations[1]
	if record.Kind != "record" || record.Name != "Item" || record.Line != 2 || record.Column != 1 ||
		record.Signature != nil || record.EffectCoverage != nil ||
		fn.Kind != "function" || fn.Name != "evaluate" || fn.Line != 3 || fn.Signature == nil ||
		fn.Signature.ReturnType != "Int" || len(fn.DeclaredAuthority) != 1 || fn.DeclaredAuthority[0] != "DB.read" ||
		fn.InferredEffects == nil || len(fn.InferredEffects) != 0 || fn.EffectCoverage == nil ||
		fn.EffectCoverage.Status != "incomplete" || fn.EffectCoverage.Gap.Reason != "unresolved call target" ||
		fn.EffectCoverage.Gap.Source.Start.Line != 3 || fn.EffectCoverage.Gap.Source.Start.Column < 1 ||
		fn.EffectCoverage.Gap.Source.End == nil {
		t.Fatalf("unexpected declaration projection: %s", first)
	}

	_, short := read("--module", "library", "--limit", "1")
	if len(short.Result.Declarations) != 1 || short.Result.OmittedDeclarations != 1 ||
		short.Result.TotalDeclarations != 2 || short.Result.Declarations[0].EffectCoverage != nil ||
		*short.Identity != *full.Identity {
		t.Fatalf("unexpected truncation: %+v", short.Result)
	}

	_, entry := read()
	if entry.Result.Module != "" || entry.Result.SourcePath != "app.splash" ||
		len(entry.Result.Declarations) != 1 || entry.Result.Declarations[0].Name != "main" ||
		entry.Result.Declarations[0].EffectCoverage == nil || entry.Result.Declarations[0].EffectCoverage.Status != "complete" {
		t.Fatalf("unexpected entry result: %+v", entry.Result)
	}
}

type summaryTestReport struct {
	SchemaVersion int
	EntryPath     string
	Identity      *struct{ SourceID, AnalysisID string }
	Result        *struct {
		Module, SourcePath                     string
		TotalDeclarations, OmittedDeclarations int
		Declarations                           []struct {
			Kind, Name                         string
			Line, Column                       int
			Signature                          *struct{ ReturnType string }
			DeclaredAuthority, InferredEffects []string
			EffectCoverage                     *struct {
				Status string
				Gap    struct {
					Reason string
					Source checkTestRange
				}
			}
		}
	}
	Diagnostics []checkTestDiagnostic
}

func TestSummaryCLIDefaultLimit(t *testing.T) {
	binary := buildSplashCLI(t)
	var source strings.Builder
	for i := range 105 {
		fmt.Fprintf(&source, "fn declaration%d() {}\n", i)
	}
	path := writeSplashFile(t, t.TempDir(), "app.splash", source.String())
	for _, tc := range []struct {
		args []string
		want int
	}{{nil, 20}, {[]string{"--limit", "100"}, 100}} {
		out, err := exec.Command(binary, append([]string{"summary", path, "--json"}, tc.args...)...).Output()
		var report summaryTestReport
		if err != nil || json.Unmarshal(out, &report) != nil || report.Result == nil ||
			report.Result.TotalDeclarations != 105 || report.Result.OmittedDeclarations != 105-tc.want ||
			len(report.Result.Declarations) != tc.want || report.Result.Declarations[tc.want-1].Name != fmt.Sprintf("declaration%d", tc.want-1) {
			t.Fatalf("limit args %v: %v, %.600s", tc.args, err, out)
		}
	}
}

func TestSummaryCLIDiagnosticsAndMisuse(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "lib.splash", "module shared\nfn helper() {}\n")
	path := writeSplashFile(t, dir, "app.splash", "module shared\nuse lib as alias\nfn main() {}\n")
	broken := writeSplashFile(t, dir, "broken.splash", "fn main( {\n")
	for _, tc := range []struct{ path, name, code, phase string }{
		{path, "shared", "MODULE_AMBIGUOUS", "inspection"},
		{path, "alias", "MODULE_NOT_FOUND", "inspection"},
		{path, "lib", "MODULE_NOT_FOUND", "inspection"},
		{broken, "shared", "PARSE_ERROR", "parse"},
		{filepath.Join(dir, "missing.splash"), "shared", "SOURCE_READ_ERROR", "input"},
	} {
		cmd := exec.Command(binary, "summary", tc.path, "--json", "--module", tc.name)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		var exit *exec.ExitError
		var report summaryTestReport
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || stderr.Len() != 0 ||
			json.Unmarshal(out, &report) != nil || report.Result != nil ||
			(report.Identity != nil) != (tc.phase == "inspection") ||
			len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != tc.code ||
			report.Diagnostics[0].Phase != tc.phase || report.Diagnostics[0].Severity != "error" {
			t.Fatalf("expected %s: %v, stdout %s, stderr %s", tc.code, err, out, &stderr)
		}
	}

	for _, args := range [][]string{
		nil, {path}, {path, "--module", "shared", "--json"},
		{path, "--json", "--module"}, {path, "--json", "--module", ""},
		{path, "--json", "--module", "--limit"}, {path, "--json", "--module", "a", "--module", "a"},
		{path, "--json", "--limit"}, {path, "--json", "--limit", "0"}, {path, "--json", "--limit", "-1"},
		{path, "--json", "--limit", "101"}, {path, "--json", "--limit", "one"},
		{path, "--json", "--limit", "1", "--limit", "2"}, {path, "--json", "--unknown"},
	} {
		cmd := exec.Command(binary, append([]string{"summary"}, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(out) != 0 ||
			!strings.Contains(stderr.String(), "usage: splash summary") {
			t.Fatalf("expected usage for %v: %v, stdout %s, stderr %s", args, err, out, &stderr)
		}
	}
}

func TestSummaryAcceptsDashPrefixedEntryPath(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "--app.splash", "module app\nfn main() {}")
	command := exec.Command(binary, "summary", "--app.splash", "--json")
	command.Dir = dir
	raw, err := command.Output()
	var report summaryTestReport
	if err != nil || json.Unmarshal(raw, &report) != nil ||
		report.EntryPath != "--app.splash" || report.Result == nil || report.Result.Module != "app" {
		t.Fatalf("expected positional entry path: %s (%v)", raw, err)
	}
}
