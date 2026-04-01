package codegen_test

import (
	"strings"
	"testing"

	goparser "go/parser"
	gotoken "go/token"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
)

// parse is a shared helper used across all codegen tests.
func parse(t *testing.T, src string) *ast.File {
	t.Helper()
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, diags := p.ParseFile()
	for _, d := range diags {
		t.Logf("parse diagnostic: %s", d)
	}
	return file
}

// emitSrc parses src and emits Go source.
func emitSrc(t *testing.T, src string) string {
	t.Helper()
	f := parse(t, src)
	e := codegen.New()
	return e.EmitFile(f)
}

// mustGoSyntax fails the test if src is not valid Go syntax.
func mustGoSyntax(t *testing.T, src string) {
	t.Helper()
	fset := gotoken.NewFileSet()
	_, err := goparser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		t.Errorf("generated Go has syntax errors: %v\nsource:\n%s", err, src)
	}
}

func TestEmitTypeName(t *testing.T) {
	e := codegen.New()
	for _, c := range []struct {
		in   ast.TypeExpr
		want string
	}{
		{&ast.NamedTypeExpr{Name: "String"}, "string"},
		{&ast.NamedTypeExpr{Name: "Int"}, "int"},
		{&ast.NamedTypeExpr{Name: "Float"}, "float64"},
		{&ast.NamedTypeExpr{Name: "Bool"}, "bool"},
		{
			&ast.OptionalTypeExpr{Inner: &ast.NamedTypeExpr{Name: "String"}},
			"*string",
		},
		{
			&ast.NamedTypeExpr{
				Name:     "List",
				TypeArgs: []ast.TypeExpr{&ast.NamedTypeExpr{Name: "Int"}},
			},
			"[]int",
		},
		{
			&ast.FnTypeExpr{
				Params:     []ast.TypeExpr{&ast.NamedTypeExpr{Name: "String"}},
				ReturnType: &ast.NamedTypeExpr{Name: "Bool"},
			},
			"func(string) bool",
		},
	} {
		got := e.EmitTypeName(c.in)
		if got != c.want {
			t.Errorf("EmitTypeName(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEmitTypeDecl(t *testing.T) {
	src := `
module user
type User {
    name: String
    age: Int
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "type User struct") {
		t.Errorf("expected 'type User struct', got:\n%s", out)
	}
	if !strings.Contains(out, "name string") {
		t.Errorf("expected field 'name string', got:\n%s", out)
	}
}

func TestEmitEnumDecl(t *testing.T) {
	src := `
module result
enum Status {
    Pending
    Done
    Failed
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "type Status interface") {
		t.Errorf("expected 'type Status interface', got:\n%s", out)
	}
	if !strings.Contains(out, "StatusPending") {
		t.Errorf("expected variant type StatusPending, got:\n%s", out)
	}
}

func TestEmitFunctionDecl(t *testing.T) {
	src := `
module greet
fn greet(name: String) -> String {
    return name
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "func greet(name string) string") {
		t.Errorf("expected function signature, got:\n%s", out)
	}
}

func TestEmitLetAndReturn(t *testing.T) {
	src := `
module calc
fn add(x: Int, y: Int) -> Int {
    let result = x
    return result
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "result := x") {
		t.Errorf("expected 'result := x', got:\n%s", out)
	}
	if !strings.Contains(out, "return result") {
		t.Errorf("expected 'return result', got:\n%s", out)
	}
}

func TestEmitIfStmt(t *testing.T) {
	src := `
module check
fn isPositive(n: Int) -> Bool {
    if n > 0 {
        return true
    }
    return false
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "if (n > 0)") {
		t.Errorf("expected 'if (n > 0)', got:\n%s", out)
	}
}

func TestEmitForStmt(t *testing.T) {
	src := `
module loop
fn printAll(items: List<String>) {
    for item in items {
    }
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "for _, item := range items") {
		t.Errorf("expected 'for _, item := range items', got:\n%s", out)
	}
}
