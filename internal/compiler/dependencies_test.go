package compiler_test

import (
	"sync"
	"testing"

	"gosplash.dev/splash/internal/compiler"
)

func TestDependenciesUseCapturedFactsConcurrently(t *testing.T) {
	dir := t.TempDir()
	path := writeSource(t, dir, "main.splash", "module app\nfn root() { old() }\nfn old() {}\n")
	program := analyze(t, path)
	writeSource(t, dir, "main.splash", "module app\nfn root() { new_target() }\nfn new_target() {}\n")

	var tasks sync.WaitGroup
	for range 8 {
		tasks.Go(func() {
			result, err := program.Dependencies("app.root", compiler.Callees, 1)
			if err != nil || len(result.Relationships) != 1 || result.Relationships[0].Function.Symbol != "app.old" {
				t.Errorf("dependency facts changed after the source edit: %+v, %v", result, err)
				return
			}
			// The result is detached even when the index is reused.
			result.Relationships[0].Function.Symbol = "changed"
		})
	}
	tasks.Go(func() {
		_, _, err := compiler.Verify(program)
		if err != nil {
			t.Errorf("dependency memoization affected verification: %v", err)
		}
	})
	tasks.Wait()
}
