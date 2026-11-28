package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/ast"
)

func TestAnalyzeProgramRetainsTypedModulesAndSource(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	entry := write("main.splash", `
module main
use lib as local
fn main() { println(local.answer()) }
`)
	const original = "module lib\nfn answer() -> Int { return 42 }\n"
	lib := write("lib.splash", original)

	program, _, err := Analyze(entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.snapshot.modules) != 2 {
		t.Fatalf("expected one imported module and one entry module, got %d", len(program.snapshot.modules))
	}
	imported, root := program.snapshot.modules[0], program.snapshot.modules[1]
	if imported.File.Module.Name != "lib" || root.File.Module.Name != "main" {
		t.Fatal("expected imports before the entry module, with original module ownership")
	}
	fn := imported.File.Declarations[0].(*ast.FunctionDecl)
	value := fn.Body.Stmts[0].(*ast.ReturnStmt).Value
	if typ := imported.Types[value]; typ == nil || typ.TypeName() != "Int" {
		t.Fatalf("expected the imported return expression to retain type Int, got %v", typ)
	}
	write("lib.splash", "module lib\nfn answer() -> Int { return 99 }\n")
	if program.snapshot.sources[lib] != original || !strings.Contains(program.snapshot.sources[entry], "use lib as local") {
		t.Fatal("expected captured source from both modules, including the original imported bytes")
	}

	verified, _, err := Verify(program)
	if err != nil {
		t.Fatal(err)
	}
	goSrc, err := verified.EmitGo()
	if err != nil {
		t.Fatalf("emit captured program: %v", err)
	}
	generated := filepath.Join(dir, "generated.go")
	if err := os.WriteFile(generated, []byte(goSrc), 0644); err != nil {
		t.Fatalf("write generated Go: %v", err)
	}
	output, err := exec.Command("go", "run", generated).CombinedOutput()
	if err != nil {
		t.Fatalf("run generated Go: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "42" {
		t.Fatalf("expected captured program to print 42, got %q", got)
	}
}
