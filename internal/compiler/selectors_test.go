package compiler

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gosplash.dev/splash/internal/effects"
)

func TestSharedSelectorsAreLazy(t *testing.T) {
	dir := t.TempDir()
	for file, source := range map[string]string{
		"main.splash":    "module shared\nuse first as local\nuse second as other\nuse unnamed as hidden\nfn run() {}\nfn main() {}\n",
		"first.splash":   "module shared\nfn run() {}\nfn unique() {}\n",
		"second.splash":  "module shared\nfn run() {}\n",
		"unnamed.splash": "fn hidden() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, diagnostics, err := Analyze(filepath.Join(dir, "main.splash"))
	if err != nil {
		t.Fatalf("analyze: %v, %v", err, diagnostics)
	}
	if _, _, err := Verify(p); err != nil {
		t.Fatal(err)
	}
	if p.snapshot.selectors != nil || p.snapshot.dependencies != nil {
		t.Fatal("ordinary verification initialized a query index")
	}

	for _, tc := range []struct {
		query, symbol string
		err           error
	}{
		{"run", "shared.run", nil},
		{"shared.main", "shared.main", nil},
		{"shared.unique", "shared.unique", nil},
		{"shared.run", "", ErrAmbiguousFunction},
		{"local.run", "", ErrFunctionNotFound},
		{"unique", "", ErrFunctionNotFound},
		{"hidden", "", ErrFunctionNotFound},
		{".hidden", "", ErrFunctionNotFound},
		{"shared.", "", ErrFunctionNotFound},
	} {
		c, err := p.InspectFunction(tc.query)
		if !errors.Is(err, tc.err) || c.Symbol != tc.symbol {
			t.Errorf("inspect %q: %q, %v", tc.query, c.Symbol, err)
		}
	}
	selectors := p.snapshot.selectors
	if selectors == nil || p.snapshot.dependencies != nil {
		t.Fatal("inspection should initialize only the shared selector index")
	}
	for _, query := range []string{"shared.run", "local.run"} {
		_, inspectErr := p.InspectFunction(query)
		_, depErr := p.Dependencies(query, Callers, 1)
		if inspectErr == nil || depErr == nil || inspectErr.Error() != depErr.Error() {
			t.Errorf("selector %q gave different errors: inspect %v, callers %v", query, inspectErr, depErr)
		}
	}
	if p.snapshot.dependencies != nil {
		t.Fatal("failed dependency queries initialized adjacency")
	}
	d, err := p.Dependencies("shared.unique", Callees, 1)
	if err != nil || d.Root.Symbol != "shared.unique" || p.snapshot.selectors != selectors {
		t.Fatalf("dependency query failed to reuse the selector index: %+v, %v", d, err)
	}
}

func TestSelectorsAssociateEffectsByDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.splash")
	library := "module library\nfn scoped() needs Net {}\n"
	app := "module app\nuse library\nfn main() {}\n"
	for path, source := range map[string]string{path: app, filepath.Join(dir, "library.splash"): library} {
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, diagnostics, err := Analyze(path)
	if err != nil {
		t.Fatalf("analyze: %v, %v", err, diagnostics)
	}
	slices.Reverse(p.snapshot.effects)
	for _, tc := range []struct {
		query, symbol string
		authority     effects.EffectSet
	}{
		{"main", "app.main", effects.None},
		{"library.scoped", "library.scoped", effects.Net},
	} {
		contract, err := p.InspectFunction(tc.query)
		if err != nil || contract.Symbol != tc.symbol || contract.Declared != tc.authority {
			t.Errorf("inspect %q: got %+v, %v", tc.query, contract, err)
		}
	}
}
