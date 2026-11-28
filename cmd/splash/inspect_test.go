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

type inspectTestReport struct {
	SchemaVersion int
	EntryPath     string
	Query         string
	Contract      *struct {
		Symbol      string
		Declaration checkTestPoint
		Signature   struct {
			Parameters []struct {
				Name, Type string
			}
			ReturnType string
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
		for _, diag := range report.Diagnostics {
			if diag.Code != tc.code || diag.Phase != tc.phase || diag.Severity != "error" {
				t.Errorf("want %s/%s error, got %+v", tc.code, tc.phase, diag)
			}
		}
	}
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
