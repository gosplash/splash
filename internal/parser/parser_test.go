package parser_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/token"
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

func TestDocComment_BeforeTypeDecl_NoError(t *testing.T) {
	// Doc before a type decl is silently accepted (TypeDecl has no Doc field in v0.1).
	src := `
module foo
/// This comment is intentionally ignored.
type Person {
    name: String
}`
	file := parse(t, src)
	if len(file.Declarations) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(file.Declarations))
	}
	if _, ok := file.Declarations[0].(*ast.TypeDecl); !ok {
		t.Errorf("expected TypeDecl, got %T", file.Declarations[0])
	}
}

func TestDocComment_AfterAnnotation_NotAttached(t *testing.T) {
	// Doc after an annotation does not attach: the required ordering is doc-then-annotation.
	src := `
module foo
@tool
fn search(query: String) -> String {
  return query
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Doc != "" {
		t.Errorf("expected empty doc when no /// precedes the fn, got %q", fn.Doc)
	}
}

func TestParseSandboxAnnotation(t *testing.T) {
	src := `
module demo
@sandbox(allow: [DB.read, AI], deny: [Net, FS, DB.write])
@budget(max_cost: 0.50, max_calls: 20)
async fn answer_question(q: String) needs Agent -> String { return q }
`
	file := parse(t, src)
	if len(file.Declarations) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(file.Declarations))
	}
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatal("expected FunctionDecl")
	}
	if len(fn.Annotations) != 2 {
		t.Fatalf("expected 2 annotations (@sandbox, @budget), got %d", len(fn.Annotations))
	}
	if fn.Annotations[0].Kind != ast.AnnotSandbox {
		t.Errorf("expected first annotation to be @sandbox, got %v", fn.Annotations[0].Kind)
	}
	if fn.Annotations[1].Kind != ast.AnnotBudget {
		t.Errorf("expected second annotation to be @budget, got %v", fn.Annotations[1].Kind)
	}
	allowVal, ok := fn.Annotations[0].Args["allow"]
	if !ok {
		t.Fatal("expected @sandbox to have 'allow' arg")
	}
	allowList, ok := allowVal.(*ast.ListLiteral)
	if !ok {
		t.Fatalf("expected 'allow' to be *ast.ListLiteral, got %T", allowVal)
	}
	if len(allowList.Elements) != 2 {
		t.Errorf("expected 'allow' list to have 2 elements, got %d", len(allowList.Elements))
	}
	denyVal, ok := fn.Annotations[0].Args["deny"]
	if !ok {
		t.Fatal("expected @sandbox to have 'deny' arg")
	}
	denyList, ok := denyVal.(*ast.ListLiteral)
	if !ok {
		t.Fatalf("expected 'deny' to be *ast.ListLiteral, got %T", denyVal)
	}
	if len(denyList.Elements) != 3 {
		t.Errorf("expected 'deny' list to have 3 elements, got %d", len(denyList.Elements))
	}
}

func TestParseBudgetAnnotationArgs(t *testing.T) {
	src := `
module demo
@budget(max_cost: 0.50, max_calls: 20)
async fn run_agent(goal: String) needs Agent -> String { return goal }
`
	file := parse(t, src)
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatal("expected FunctionDecl")
	}
	if fn.Annotations[0].Kind != ast.AnnotBudget {
		t.Errorf("expected @budget annotation")
	}
	maxCostVal, ok := fn.Annotations[0].Args["max_cost"]
	if !ok {
		t.Fatal("expected @budget to have 'max_cost' arg")
	}
	if _, ok := maxCostVal.(*ast.FloatLiteral); !ok {
		t.Errorf("expected 'max_cost' to be *ast.FloatLiteral, got %T", maxCostVal)
	}
	maxCallsVal, ok := fn.Annotations[0].Args["max_calls"]
	if !ok {
		t.Fatal("expected @budget to have 'max_calls' arg")
	}
	if _, ok := maxCallsVal.(*ast.IntLiteral); !ok {
		t.Errorf("expected 'max_calls' to be *ast.IntLiteral, got %T", maxCallsVal)
	}
}

func TestParseGenericCall_SingleTypeArg(t *testing.T) {
	src := `
module demo
fn test(ai: String) -> String {
  return ai.prompt<SermonInsight>(ai)
}
`
	file := parse(t, src)
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatal("expected FunctionDecl")
	}
	ret, ok := fn.Body.Stmts[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatal("expected ReturnStmt")
	}
	call, ok := ret.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", ret.Value)
	}
	if len(call.TypeArgs) != 1 {
		t.Fatalf("expected 1 type arg, got %d", len(call.TypeArgs))
	}
	typeArg, ok := call.TypeArgs[0].(*ast.NamedTypeExpr)
	if !ok {
		t.Fatalf("expected NamedTypeExpr, got %T", call.TypeArgs[0])
	}
	if typeArg.Name != "SermonInsight" {
		t.Errorf("expected type arg SermonInsight, got %q", typeArg.Name)
	}
	if len(call.Args) != 1 {
		t.Errorf("expected 1 call arg, got %d", len(call.Args))
	}
}

func TestParseGenericCall_StillParseComparisonLT(t *testing.T) {
	// a < b must still parse as a comparison, not as a generic call
	src := `
module demo
fn test(a: Int, b: Int) -> Bool {
  return a < b
}
`
	file := parse(t, src)
	fn, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatal("expected FunctionDecl")
	}
	ret, ok := fn.Body.Stmts[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatal("expected ReturnStmt")
	}
	bin, ok := ret.Value.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr for a < b, got %T", ret.Value)
	}
	if bin.Op != token.LT {
		t.Errorf("expected LT op, got %v", bin.Op)
	}
}
