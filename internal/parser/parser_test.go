package parser_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
)

func parse(t *testing.T, src string) *ast.File {
	t.Helper()
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, diags := p.ParseFile()
	for _, d := range diags {
		t.Logf("diagnostic: %s", d)
	}
	return file
}

func TestParseModule(t *testing.T) {
	file := parse(t, "module greeter")
	if file.Module == nil {
		t.Fatal("expected module decl")
	}
	if file.Module.Name != "greeter" {
		t.Errorf("got %q, want %q", file.Module.Name, "greeter")
	}
}

func TestParseFunctionDecl(t *testing.T) {
	src := `
module foo
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	if len(file.Declarations) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(file.Declarations))
	}
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatalf("expected FunctionDecl, got %T", file.Declarations[0])
	}
	if fn.Name != "greet" {
		t.Errorf("got %q, want %q", fn.Name, "greet")
	}
}

func TestParseFunctionWithEffects(t *testing.T) {
	src := `
module foo
fn load(id: String) needs DB, Net -> User {
  return db.find(id)
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if len(fn.Effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(fn.Effects))
	}
	if fn.Effects[0].Name != "DB" {
		t.Errorf("got %q, want DB", fn.Effects[0].Name)
	}
	if fn.Effects[1].Name != "Net" {
		t.Errorf("got %q, want Net", fn.Effects[1].Name)
	}
}

func TestParseTypeDecl(t *testing.T) {
	src := `
module foo
type User {
  id: UserId
  @sensitive
  email: Email
}`
	file := parse(t, src)
	td, ok := file.Declarations[0].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("expected TypeDecl, got %T", file.Declarations[0])
	}
	if td.Name != "User" {
		t.Errorf("got %q, want User", td.Name)
	}
	if len(td.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(td.Fields))
	}
	emailField := td.Fields[1]
	if len(emailField.Annotations) != 1 {
		t.Fatalf("expected 1 annotation on email, got %d", len(emailField.Annotations))
	}
	if emailField.Annotations[0].Kind != ast.AnnotSensitive {
		t.Errorf("expected @sensitive annotation")
	}
}

func TestParseAnnotationOnFunction(t *testing.T) {
	src := `
module foo
@redline(reason: "dangerous")
fn drop() needs DB.admin { }`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if len(fn.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(fn.Annotations))
	}
	if fn.Annotations[0].Kind != ast.AnnotRedline {
		t.Errorf("expected @redline, got %d", fn.Annotations[0].Kind)
	}
}

func TestParseEnumDecl(t *testing.T) {
	src := `
module foo
enum Sentiment { hopeful convicting celebratory }`
	file := parse(t, src)
	ed, ok := file.Declarations[0].(*ast.EnumDecl)
	if !ok {
		t.Fatalf("expected EnumDecl, got %T", file.Declarations[0])
	}
	if len(ed.Variants) != 3 {
		t.Fatalf("expected 3 variants, got %d", len(ed.Variants))
	}
}

func TestParseExposeList(t *testing.T) {
	src := `module foo
expose greet, Greeting`
	file := parse(t, src)
	if len(file.Exposes) != 2 {
		t.Fatalf("expected 2 exposes, got %d", len(file.Exposes))
	}
}

func TestFunctionDecl_DocComment(t *testing.T) {
	src := `
module foo
/// Greets the named person.
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Doc != "Greets the named person." {
		t.Errorf("expected doc %q, got %q", "Greets the named person.", fn.Doc)
	}
}

func TestFunctionDecl_MultiLineDocComment(t *testing.T) {
	src := `
module foo
/// First line.
/// Second line.
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	want := "First line.\nSecond line."
	if fn.Doc != want {
		t.Errorf("expected doc %q, got %q", want, fn.Doc)
	}
}

func TestFunctionDecl_NoDocComment(t *testing.T) {
	src := `
module foo
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Doc != "" {
		t.Errorf("expected empty doc, got %q", fn.Doc)
	}
}

func TestParam_DocComment(t *testing.T) {
	src := `
module foo
@tool
fn search(
  /// The search query
  query: String,
  /// Max results to return
  limit: Int,
) -> String {
  return query
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Params[0].Doc != "The search query" {
		t.Errorf("expected param[0].Doc %q, got %q", "The search query", fn.Params[0].Doc)
	}
	if fn.Params[1].Doc != "Max results to return" {
		t.Errorf("expected param[1].Doc %q, got %q", "Max results to return", fn.Params[1].Doc)
	}
}
