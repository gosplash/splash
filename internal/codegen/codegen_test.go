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

// Silence "strings imported and not used" — used by later tasks' tests in this file.
var _ = strings.Contains
