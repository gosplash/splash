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
		Verdict       string
		Diagnostics   []checkTestDiagnostic
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid report %q: %v", &stdout, err)
	}
	if report.SchemaVersion != 1 || report.EntryPath != path || report.Verdict != verdict || report.Diagnostics == nil {
		t.Fatalf("unexpected verification report: %+v", report)
	}
	return report.Diagnostics, stdout.Bytes()
}
