package compiler_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/vm"
)

func writeSource(t *testing.T, dir, name, source string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func analyze(t *testing.T, path string) *compiler.AnalyzedProgram {
	t.Helper()
	program, diagnostics, err := compiler.Analyze(path)
	if err != nil {
		t.Fatalf("analyze: %v, diagnostics: %v", err, diagnostics)
	}
	return program
}

func verify(t *testing.T, program *compiler.AnalyzedProgram) *compiler.VerifiedProgram {
	t.Helper()
	verified, report, err := compiler.Verify(program)
	if err != nil {
		t.Fatalf("verify: %v, report: %v", err, report)
	}
	return verified
}

func TestUninitializedHandlesCannotAuthorizeArtifacts(t *testing.T) {
	for _, analyzed := range []*compiler.AnalyzedProgram{nil, {}} {
		if verified, _, err := compiler.Verify(analyzed); verified != nil || !errors.Is(err, compiler.ErrInvalidAnalysis) {
			t.Fatalf("Verify(%v) issued %v with error %v", analyzed, verified, err)
		}
	}

	for _, handle := range []*compiler.VerifiedProgram{nil, {}} {
		if source, err := handle.EmitGo(); source != "" || !errors.Is(err, compiler.ErrUnverifiedProgram) {
			t.Fatalf("EmitGo produced %q, error %v", source, err)
		}
		if source, err := handle.EmitExecutableGo(); source != "" || !errors.Is(err, compiler.ErrUnverifiedProgram) {
			t.Fatalf("EmitExecutableGo produced %q, error %v", source, err)
		}
		if program, err := handle.LowerVM(); len(program.Functions) != 0 || !errors.Is(err, compiler.ErrUnverifiedProgram) {
			t.Fatalf("LowerVM produced %v, error %v", program, err)
		}
		if manifest, err := handle.ExportTools(compiler.FormatOpenAI); manifest != nil || !errors.Is(err, compiler.ErrUnverifiedProgram) {
			t.Fatalf("ExportTools produced %q, error %v", manifest, err)
		}
		var output bytes.Buffer
		if err := handle.RunVM(&output, vm.Limits{}); !errors.Is(err, compiler.ErrUnverifiedProgram) || output.Len() != 0 {
			t.Fatalf("RunVM produced %q, error %v", output.String(), err)
		}
	}
}

func TestGoCannotConvertAnalyzedHandlesToVerified(t *testing.T) {
	analyzed := reflect.TypeFor[compiler.AnalyzedProgram]()
	verified := reflect.TypeFor[compiler.VerifiedProgram]()
	if analyzed.ConvertibleTo(verified) || reflect.PointerTo(analyzed).ConvertibleTo(reflect.PointerTo(verified)) {
		t.Fatal("Go conversion must not mint a verified handle")
	}
}

func TestOnlySuccessfulVerificationMintsHandle(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         error
	}{
		{"rejected", "redline fn reset() {}\nagent fn report() { reset() }\n", safety.ErrRejected},
		{"indeterminate", "agent fn report(callback: fn() -> Int) -> Int { return callback() }\n", safety.ErrIndeterminate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSource(t, t.TempDir(), "service.splash", "module service\n"+tc.source)
			program := analyze(t, path)
			verified, report, err := compiler.Verify(program)
			if verified != nil || !errors.Is(err, tc.want) {
				t.Fatalf("got handle %v, error %v, report %v", verified, err, report)
			}
			if len(program.Functions()) == 0 {
				t.Fatal("analysis should remain inspectable")
			}
		})
	}
}

func TestVerifiedHandleStaysBoundToOriginalAnalysis(t *testing.T) {
	dir := t.TempDir()
	lib := "module value\nexpose answer\nfn answer() -> Int { return %s }\n"
	writeSource(t, dir, "value.splash", strings.ReplaceAll(lib, "%s", "42"))
	path := writeSource(t, dir, "main.splash", "module main\nuse value as local\nfn main() { println(local.answer()) }\n")
	original := analyze(t, path)
	handle := verify(t, original)

	// Replacing an analysis handle and rewriting source cannot change the
	// immutable analysis state held by an already verified handle.
	writeSource(t, dir, "value.splash", strings.ReplaceAll(lib, "%s", "99"))
	*original = *analyze(t, path)
	functions := original.Functions()
	for index := range functions {
		if len(functions[index].Callees) > 0 {
			functions[index].Callees[0] = "invalid"
		}
	}
	for _, function := range original.Functions() {
		for _, callee := range function.Callees {
			if callee == "invalid" {
				t.Fatal("inspection mutated the analysis graph")
			}
		}
	}

	updated := verify(t, original)
	rejected := writeSource(t, dir, "rejected.splash",
		"module rejected\nredline fn reset() {}\nagent fn report() { reset() }\n")
	*original = *analyze(t, rejected)
	if blocked, _, err := compiler.Verify(original); blocked != nil || !errors.Is(err, safety.ErrRejected) {
		t.Fatalf("replacement analysis should be rejected, got handle %v, error %v", blocked, err)
	}

	for _, tc := range []struct {
		name   string
		handle *compiler.VerifiedProgram
		want   string
	}{
		{"original captured source", handle, "42\n"},
		{"new captured source", updated, "99\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := tc.handle.RunVM(&output, vm.Limits{Instructions: 100, Frames: 4}); err != nil || output.String() != tc.want {
				t.Fatalf("run = %q, %v; want %q", output.String(), err, tc.want)
			}
		})
	}
}

func TestInspectionCannotChangeVerificationEvidence(t *testing.T) {
	path := writeSource(t, t.TempDir(), "service.splash", `module service
agent fn report(callback: fn() -> Int) -> Int { return callback() }
`)
	program := analyze(t, path)
	functions := program.Functions()
	if len(functions) != 1 || functions[0].Name != "report" || functions[0].Gap == nil {
		t.Fatalf("expected inspected coverage gap for report, got %v", functions)
	}
	functions[0].Gap.ApprovalBoundary = true
	functions[0].Gap.Reason = "changed by caller"
	handle, result, err := compiler.Verify(program)
	if handle != nil || !errors.Is(err, safety.ErrIndeterminate) || len(result.Incomplete) != 1 ||
		!strings.Contains(result.Incomplete[0].Message, "unresolved call target") {
		t.Fatalf("inspection changed verification, got %v, %v, %v", handle, err, result)
	}
}
