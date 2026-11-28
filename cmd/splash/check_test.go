package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

type checkTestPoint struct {
	File         string
	Line, Column int
}

type checkTestRange struct {
	Start checkTestPoint
	End   *checkTestPoint
}

type checkTestDiagnostic struct {
	Code, Phase, Severity, Message string
	ExpectedType, ActualType       string
	PolicyRuleID                   string
	Source                         *checkTestRange
	Effects                        []string
	Witness                        []struct {
		Kind, Target string
		Source       checkTestRange
		Declaration  *checkTestPoint
	}
}

func TestCheckJSONVerdicts(t *testing.T) {
	binary := buildSplashCLI(t)
	for _, tc := range []struct {
		name, source, verdict string
		module                string
		codes, phases         []string
	}{
		{name: "verified", source: "fn main() {}", verdict: "verified"},
		{name: "parse violation", source: "fn main( {", verdict: "rejected", codes: []string{"PARSE_ERROR"}, phases: []string{"parse"}},
		{name: "type violation", source: `fn main() { let n: Int = "text" }`, verdict: "rejected", codes: []string{"TYPE_MISMATCH"}, phases: []string{"type"}},
		{name: "imported parse violation", source: "use lib\nfn main() {}", module: "fn bad( {", verdict: "rejected", codes: []string{"PARSE_ERROR"}, phases: []string{"parse"}},
		{name: "known policy violation", source: "redline fn reset() {}\nagent fn report() { reset() }", verdict: "rejected", codes: []string{"POLICY_VIOLATION"}, phases: []string{"policy"}},
		{name: "incomplete", source: "agent fn report(cb: fn() -> Int) -> Int { return cb() }", verdict: "indeterminate", codes: []string{"POLICY_COVERAGE_INCOMPLETE"}, phases: []string{"policy"}},
		{name: "known violation with incomplete coverage", source: "fn read() needs DB.read -> Int { return 1 }\nagent fn report(cb: fn() -> Int) -> Int { return cb() + read() }", verdict: "rejected", codes: []string{"DECLARED_EFFECT_MISSING", "POLICY_COVERAGE_INCOMPLETE"}, phases: []string{"effect", "policy"}},
		{name: "input failure", codes: []string{"SOURCE_READ_ERROR"}, phases: []string{"input"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "main.splash")
			if tc.source != "" {
				path = writeSplashFile(t, dir, "main.splash", "module main\n"+tc.source+"\n")
			}
			if tc.module != "" {
				writeSplashFile(t, dir, "lib.splash", "module lib\n"+tc.module+"\n")
			}
			diagnostics, _ := readCheckJSON(t, binary, path, tc.verdict)
			for i, code := range tc.codes {
				found := false
				for _, diag := range diagnostics {
					if diag.Code == code && diag.Phase == tc.phases[i] && diag.Severity == "error" && diag.Message != "" {
						found = true
					}
				}
				if !found {
					t.Errorf("missing %s diagnostic with phase %s: %+v", code, tc.phases[i], diagnostics)
				}
			}
			for _, diag := range diagnostics {
				want := map[string]string{"POLICY_VIOLATION": "redline.agent_reachable", "POLICY_COVERAGE_INCOMPLETE": "coverage.required_source"}[diag.Code]
				if diag.PolicyRuleID != want {
					t.Errorf("code %s has policy rule ID %q, want %q", diag.Code, diag.PolicyRuleID, want)
				}
			}
			if tc.name == "type violation" && (len(diagnostics) != 1 ||
				diagnostics[0].ExpectedType != "Int" || diagnostics[0].ActualType != "String") {
				t.Errorf("expected assignment types Int and String: %+v", diagnostics)
			}
			if tc.verdict == "verified" && len(diagnostics) != 0 {
				t.Errorf("unexpected diagnostics: %+v", diagnostics)
			}
		})
	}
}

func TestCheckJSONEffectCoverageIsIndependentOfVerdict(t *testing.T) {
	binary := buildSplashCLI(t)
	for _, tc := range []struct {
		name, source, verdict, coverage string
	}{
		{"complete", "fn main() {}", "verified", "complete"},
		{"ordinary callback", "fn evaluate(cb: fn() -> Int) -> Int { return cb() }", "verified", "incomplete"},
		{"required callback", "agent fn evaluate(cb: fn() -> Int) -> Int { return cb() }", "indeterminate", "incomplete"},
		{"trusted adapter", "approve fn confirm() -> Bool { return true }", "verified", "incomplete"},
		{"known violation", "redline fn reset() {}\nagent fn report() { reset() }", "rejected", "complete"},
		{"parse failure", "fn main( {", "rejected", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSplashFile(t, t.TempDir(), "main.splash", tc.source)
			_, output := readCheckJSON(t, binary, path, tc.verdict)
			var report struct{ EffectCoverage json.RawMessage }
			if err := json.Unmarshal(output, &report); err != nil {
				t.Fatal(err)
			}
			want := `null`
			if tc.coverage != "" {
				want = `{"status":"` + tc.coverage + `"}`
			}
			if string(report.EffectCoverage) != want {
				t.Fatalf("effectCoverage = %s, want %s", report.EffectCoverage, want)
			}
		})
	}
}

func TestCheckJSONEffectWitnesses(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	lib := writeSplashFile(t, dir, "lib.splash", `module lib
expose ask
use std/ai
fn ask(n: Int) needs DB.read -> Int { return ai.prompt<Int>("question") }
`)
	path := writeSplashFile(t, dir, "main.splash", `module main
use lib as lib_alias
fn main() {
    lib_alias.ask(
        1
    )
}
`)
	diagnostics, first := readCheckJSON(t, binary, path, "rejected")
	for range 3 {
		if _, next := readCheckJSON(t, binary, path, "rejected"); !bytes.Equal(first, next) {
			t.Fatalf("diagnostics changed between invocations:\n%s\n%s", first, next)
		}
	}
	// The opening parenthesis is the legacy diagnostic anchor. The end is
	// the exclusive point just after the closing parenthesis on line 6.
	end := checkTestPoint{File: path, Line: 6, Column: 6}
	site := checkTestRange{Start: checkTestPoint{File: path, Line: 4, Column: 18}, End: &end}
	var contract, inference *checkTestDiagnostic
	for i := range diagnostics {
		diag := &diagnostics[i]
		if diag.Source == nil || diag.Source.Start.File != path {
			continue
		}
		switch diag.Code {
		case "DECLARED_EFFECT_MISSING":
			contract = diag
		case "INFERRED_EFFECT_EXCEEDS_AUTHORITY":
			inference = diag
		}
	}
	if contract == nil || !reflect.DeepEqual(contract.Source, &site) || !reflect.DeepEqual(contract.Effects, []string{"DB.read"}) ||
		len(contract.Witness) != 1 || contract.Witness[0].Kind != "call" || contract.Witness[0].Target != "ask" ||
		contract.Witness[0].Declaration == nil || contract.Witness[0].Declaration.File != lib ||
		contract.Witness[0].Declaration.Line != 4 {
		t.Fatalf("want qualified contract witness and multiline range, got %+v", contract)
	}
	if inference == nil || !reflect.DeepEqual(inference.Source, &site) || !reflect.DeepEqual(inference.Effects, []string{"AI"}) ||
		len(inference.Witness) != 2 || inference.Witness[0].Target != "ask" ||
		inference.Witness[1].Kind != "operation" || inference.Witness[1].Target != "std/ai.prompt" ||
		inference.Witness[1].Source.Start.File != lib || inference.Witness[1].Source.End == nil {
		t.Fatalf("want resolved path to the modeled operation, got %+v", inference)
	}
	text, err := exec.Command(binary, "check", path).CombinedOutput()
	if err == nil || !bytes.Contains(text, []byte("missing declared effect DB.read")) {
		t.Fatalf("legacy text check = %q, %v", text, err)
	}
}

func readCheckJSON(t *testing.T, binary, path, verdict string) ([]checkTestDiagnostic, []byte) {
	t.Helper()
	command := exec.Command(binary, "check", path, "--json")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if verdict == "verified" {
		if err != nil {
			t.Fatalf("check failed: %v, stderr: %s", err, &stderr)
		}
	} else {
		var failure *exec.ExitError
		if !errors.As(err, &failure) || failure.ExitCode() != 1 {
			t.Fatalf("check exit error = %v, stderr: %s", err, &stderr)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("JSON check wrote to stderr: %s", &stderr)
	}
	var report struct {
		SchemaVersion int
		EntryPath     string
		Verdict       json.RawMessage
		Diagnostics   []checkTestDiagnostic
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid report %q: %v", &stdout, err)
	}
	expectedVerdict := "null"
	if verdict != "" {
		expectedVerdict = `"` + verdict + `"`
	}
	if report.SchemaVersion != 1 || report.EntryPath != path || string(report.Verdict) != expectedVerdict || report.Diagnostics == nil {
		t.Fatalf("unexpected verification report: %+v", report)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "" || diagnostic.Phase == "" {
			t.Fatalf("diagnostic missing classification: %+v", diagnostic)
		}
	}
	return report.Diagnostics, stdout.Bytes()
}

func TestCheckCLIOptionsPreserveDashPath(t *testing.T) {
	binary := buildSplashCLI(t)
	dir := t.TempDir()
	writeSplashFile(t, dir, "-draft.splash", "module draft\nfn main() {}\n")

	command := exec.Command(binary, "check", "-draft.splash")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil || string(output) != "-draft.splash: ok\n" {
		t.Fatalf("legacy check with a leading dash = %q, %v", output, err)
	}

	command = exec.Command(binary, "check", "--json", "--", "-draft.splash")
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil || !json.Valid(output) || !bytes.Contains(output, []byte(`"verdict":"verified"`)) {
		t.Fatalf("check with flag before an escaped path = %q, %v", output, err)
	}
}

func TestCheckJSONCallProvenance(t *testing.T) {
	binary := buildSplashCLI(t)
	t.Run("recursive effect witness", func(t *testing.T) {
		path := writeSplashFile(t, t.TempDir(), "recursion.splash", `module recursion
use std/ai
fn ask(n: Int) -> Int {
    if n <= 0 { return ai.prompt<Int>("question") }
    return repeat(n - 1)
}
fn repeat(n: Int) -> Int { return ask(n) }
fn start() -> Int { return repeat(1) }
`)
		diagnostics, _ := readCheckJSON(t, binary, path, "rejected")
		for _, diag := range diagnostics {
			if diag.Code != "INFERRED_EFFECT_EXCEEDS_AUTHORITY" || diag.Source.Start.Line != 8 {
				continue
			}
			if len(diag.Witness) != 3 {
				t.Fatalf("want witness through two resolved calls and the operation, got %+v", diag.Witness)
			}
			for i, want := range []struct {
				kind, target string
				site, decl   int
			}{{"call", "repeat", 8, 7}, {"call", "ask", 7, 3}, {"operation", "std/ai.prompt", 4, 0}} {
				step := diag.Witness[i]
				if step.Kind != want.kind || step.Target != want.target || step.Source.Start.Line != want.site ||
					step.Source.End == nil || step.Source.End.Line != want.site {
					t.Fatalf("wrong source for witness step %d: %+v", i, step)
				}
				if want.decl == 0 && step.Declaration != nil ||
					want.decl != 0 && (step.Declaration == nil || step.Declaration.Line != want.decl) {
					t.Fatalf("wrong target declaration for witness step %d: %+v", i, step)
				}
			}
			return
		}
		t.Fatalf("missing diagnostic for start(): %+v", diagnostics)
	})
	t.Run("approval invocation", func(t *testing.T) {
		path := writeSplashFile(t, t.TempDir(), "approval.splash", `module approval
approve fn save() -> Int { return 1 }
fn main() { println(1 + save()) }
`)
		diagnostics, _ := readCheckJSON(t, binary, path, "rejected")
		if len(diagnostics) != 1 || diagnostics[0].Code != "POLICY_VIOLATION" || diagnostics[0].PolicyRuleID != "approval.call_context" {
			t.Fatalf("expected approval policy diagnostic, got %+v", diagnostics)
		}
		site := diagnostics[0].Source
		if site == nil || site.Start.Line != 3 || site.Start.Column != 29 ||
			site.End == nil || site.End.Line != 3 || site.End.Column != 31 {
			t.Fatalf("expected source range for the nested call, got %+v", site)
		}
	})
}

func TestCheckCLIRejectsEmptyOperand(t *testing.T) {
	binary := buildSplashCLI(t)
	path := writeSplashFile(t, t.TempDir(), "main.splash", "module main\nfn main() {}\n")
	command := exec.Command(binary, "check", "", "--json", path)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var failure *exec.ExitError
	if !errors.As(err, &failure) || failure.ExitCode() != 1 || stdout.Len() != 0 ||
		!bytes.Contains(stderr.Bytes(), []byte("empty file path")) {
		t.Fatalf("empty operand = %v, stdout %q, stderr %q", err, &stdout, &stderr)
	}
}
