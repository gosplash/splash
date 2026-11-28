package compiler

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gosplash.dev/splash/internal/effects"
)

func TestModuleSummaryUsesDeclarationIdentityAcrossEffectOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.splash")
	for name, source := range map[string]string{
		"main.splash":    "module app\nuse library\nfn main() {}",
		"library.splash": "module library\nfn read() needs DB.read {}",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	program, diagnostics, err := Analyze(path)
	if err != nil {
		t.Fatalf("analyze: %v, %v", err, diagnostics)
	}
	slices.Reverse(program.snapshot.effects)
	summary, err := program.SummarizeModule("library", 20)
	if err != nil || len(summary.Declarations) != 1 {
		t.Fatalf("summary changed after reordering inference results: %+v, %v", summary, err)
	}
	contract := summary.Declarations[0].Function
	if contract == nil || contract.Symbol != "library.read" || contract.Declared != effects.DBRead {
		t.Fatalf("returned the wrong function authority: %+v", contract)
	}
}
