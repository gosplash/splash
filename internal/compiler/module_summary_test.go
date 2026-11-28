package compiler_test

import (
	"errors"
	"reflect"
	"testing"

	"gosplash.dev/splash/internal/compiler"
	"gosplash.dev/splash/internal/effects"
)

func TestModuleSummaryProjectsOnlyOwnedExportsInSourceOrder(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "helper.splash", `module helper
fn calculate(cb: fn() -> Int) -> Int { return cb() }
fn report() -> String { return "helper" }
`)
	writeSource(t, dir, "lib.splash", `module inventory
expose Report, report, Status, Loggable, report
use helper
use std/ai
fn hidden() {}
constraint Loggable { fn describe(self: Loggable) -> String }
type Report { count: Int }
enum Status { Ready }
fn report(cb: fn() -> Int, document: Report) needs DB.read, AI -> Int {
    let value = ai.prompt<Int>("classify")
    return value + calculate(cb)
}
`)
	entry := writeSource(t, dir, "app.splash", "module app\nuse lib as local\nfn main() {}\n")
	program := analyze(t, entry)
	writeSource(t, dir, "lib.splash", "module replaced\n")

	short, err := program.SummarizeModule("inventory", 2)
	if err != nil || short.Module != "inventory" || short.SourcePath != "lib.splash" ||
		short.TotalDeclarations != 4 || short.OmittedDeclarations != 2 || len(short.Declarations) != 2 ||
		short.Declarations[0].Kind != "constraint" || short.Declarations[0].Name != "Loggable" ||
		short.Declarations[1].Kind != "record" || short.Declarations[1].Name != "Report" ||
		short.Declarations[1].Line != 7 || short.Declarations[1].Column != 1 {
		t.Fatalf("unexpected bounded exports: %+v, %v", short, err)
	}

	full, err := program.SummarizeModule("inventory", 4)
	if err != nil || full.OmittedDeclarations != 0 || len(full.Declarations) != 4 ||
		full.Declarations[2].Kind != "enum" || full.Declarations[2].Name != "Status" ||
		full.Declarations[3].Kind != "function" || full.Declarations[3].Name != "report" {
		t.Fatalf("unexpected exports: %+v, %v", full, err)
	}
	contract := full.Declarations[3].Function
	if contract == nil || contract.Signature.ReturnType != "Int" || len(contract.Signature.Parameters) != 2 ||
		contract.Signature.Parameters[1].TypeIdentity.Name != "Report" ||
		contract.Declared != effects.DBRead|effects.AI || contract.Inferred != effects.AI ||
		contract.Gap == nil || contract.Gap.Reason != "unresolved call target" || contract.Gap.Position.File != "helper.splash" {
		t.Fatalf("unexpected function contract: %+v", contract)
	}
	contract.Signature.Parameters[1].Name = "changed"
	contract.Gap.Reason = "changed"
	again, err := program.SummarizeModule("inventory", 4)
	if err != nil || again.Declarations[3].Function.Signature.Parameters[1].Name != "document" ||
		again.Declarations[3].Function.Gap.Reason != "unresolved call target" {
		t.Fatalf("summary changed the analysis: %+v, %v", again, err)
	}
	if _, err := program.SummarizeModule("local", 20); !errors.Is(err, compiler.ErrModuleNotFound) {
		t.Fatalf("alias lookup: %v", err)
	}
}

func TestModuleSummaryDefaultSelectsEntryEvenWhenNameIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "other.splash", "module same\nfn other() {}\n")
	entry := writeSource(t, dir, "app.splash", "module same\nuse other as local\nfn main() {}\nfn helper() {}\n")
	program := analyze(t, entry)
	if _, err := program.SummarizeModule("same", 20); !errors.Is(err, compiler.ErrAmbiguousModule) {
		t.Fatalf("named lookup must reject both owners: %v", err)
	}
	summary, err := program.SummarizeModule("", 20)
	if err != nil || summary.Module != "same" || summary.SourcePath != "app.splash" ||
		summary.TotalDeclarations != 2 || len(summary.Declarations) != 2 ||
		summary.Declarations[0].Name != "main" || summary.Declarations[1].Name != "helper" {
		t.Fatalf("default did not select the entry's declarations: %+v, %v", summary, err)
	}
}

func TestModuleSummaryUnnamedEntryAndEmptyExports(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		source string
		names  []string
	}{
		{"fn main() {}\n", []string{"main"}},
		{"expose missing\nfn main() {}\n", []string{}},
		{"", []string{}},
	} {
		entry := writeSource(t, dir, "app.splash", tc.source)
		summary, err := analyze(t, entry).SummarizeModule("", 20)
		if err != nil || summary.Module != "" || summary.SourcePath != "app.splash" ||
			summary.TotalDeclarations != len(tc.names) || summary.OmittedDeclarations != 0 || summary.Declarations == nil {
			t.Fatalf("unexpected unnamed entry: %+v, %v", summary, err)
		}
		names := make([]string, 0, len(summary.Declarations))
		for _, decl := range summary.Declarations {
			names = append(names, decl.Name)
		}
		if !reflect.DeepEqual(names, tc.names) {
			t.Fatalf("exported %v, want %v", names, tc.names)
		}
	}
}
