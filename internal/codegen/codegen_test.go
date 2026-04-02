package codegen_test

import (
	"strings"
	"testing"

	goparser "go/parser"
	gotoken "go/token"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
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

func emitSrcWithApproval(t *testing.T, src string) string {
	t.Helper()
	f := parse(t, src)

	// Collect @approve function names from AST
	approveFns := make(map[string]bool)
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				approveFns[fn.Name] = true
			}
		}
	}

	g := callgraph.Build(f)
	approveCallers := g.Callers(approveFns)

	e := codegen.New()
	e.SetApprovalCallers(approveCallers)
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

func TestEmitIfElseStmt(t *testing.T) {
	src := `
module check
fn classify(n: Int) -> Bool {
    if n > 0 {
        return true
    } else {
        return false
    }
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if strings.Count(out, "}") < 3 {
		t.Errorf("expected at least 3 closing braces (if, else, func), got:\n%s", out)
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

func TestEmitBinaryExpr(t *testing.T) {
	src := `
module math
fn sum(a: Int, b: Int) -> Int {
    return a + b
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "(a + b)") {
		t.Errorf("expected '(a + b)', got:\n%s", out)
	}
}

func TestEmitCallExpr(t *testing.T) {
	src := `
module greet
fn hello(name: String) -> String {
    return name
}
fn run() {
    hello("world")
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, `hello("world")`) {
		t.Errorf(`expected 'hello("world")', got:\n%s`, out)
	}
}

func TestEmitStructLiteralExpr(t *testing.T) {
	src := `
module user
type User {
    name: String
}
fn makeUser() -> User {
    return User { name: "alice" }
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, `User{name: "alice"}`) {
		t.Errorf("expected struct literal, got:\n%s", out)
	}
}

func TestEmitListLiteralExpr(t *testing.T) {
	src := `
module lists
fn nums() -> List<Int> {
    return [1, 2, 3]
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "[]any{1, 2, 3}") {
		t.Errorf("expected list literal, got:\n%s", out)
	}
}

func TestOptionalParamType(t *testing.T) {
	src := `
module optional
fn greet(name: String?) -> String {
    return name ?? "world"
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "name *string") {
		t.Errorf("expected 'name *string' for optional param, got:\n%s", out)
	}
}

func TestNullCoalesceEmitsHelper(t *testing.T) {
	src := `
module optional
fn greeting(name: String?) -> String {
    return name ?? "stranger"
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, `splashCoalesce(name, "stranger")`) {
		t.Errorf("expected splashCoalesce call, got:\n%s", out)
	}
	if !strings.Contains(out, "func splashCoalesce") {
		t.Errorf("expected splashCoalesce helper in preamble, got:\n%s", out)
	}
}

func TestApproveGate(t *testing.T) {
	src := `
module payments
@approve
fn processPayment(amount: Int) {
    let x = amount
}
fn run() {
    processPayment(100)
}
`
	// Use plain emitSrc (no graph) — cascade does not fire, but @approve function
	// itself still gets the approval gate and error return.
	out := emitSrc(t, src)
	mustGoSyntax(t, out)

	// Void @approve function gets error return
	if !strings.Contains(out, "func processPayment(amount int) error") {
		t.Errorf("expected error return on @approve function, got:\n%s", out)
	}
	// Body injection is now an error-returning guard
	if !strings.Contains(out, `if err := splashApprove("processPayment"); err != nil`) {
		t.Errorf("expected error-returning gate, got:\n%s", out)
	}
	// Implicit return nil after void @approve function body
	if !strings.Contains(out, "return nil") {
		t.Errorf("expected 'return nil' at end of void @approve body, got:\n%s", out)
	}
	// Helper in preamble
	if !strings.Contains(out, "func splashApprove") {
		t.Errorf("expected splashApprove helper in preamble, got:\n%s", out)
	}
	// splashApprove must now return error (not be void)
	if !strings.Contains(out, "func splashApprove(name string) error") {
		t.Errorf("expected splashApprove to return error, got:\n%s", out)
	}
	// No cascade without graph: run() call site must not have error injection
	runIdx := strings.Index(out, "func run()")
	if runIdx < 0 {
		t.Fatal("func run() not found in output — cannot verify call site is clean")
	}
	if strings.Contains(out[runIdx:], "if err :=") {
		t.Errorf("error injection must not appear at call site without graph, got:\n%s", out)
	}
}

func TestApproveCascade(t *testing.T) {
	src := `
module payments
@approve
fn charge(amount: Int) -> Int {
    return amount
}
fn run() -> Int {
    let result = charge(100)
    return result
}
`
	out := emitSrcWithApproval(t, src)
	mustGoSyntax(t, out)

	// @approve function: (Int, error) return
	if !strings.Contains(out, "func charge(amount int) (int, error)") {
		t.Errorf("expected (int, error) return on @approve function, got:\n%s", out)
	}
	// @approve function body: error-returning gate
	if !strings.Contains(out, `if err := splashApprove("charge"); err != nil`) {
		t.Errorf("expected error-returning gate in charge body, got:\n%s", out)
	}
	// @approve function: explicit return becomes return x, nil
	if !strings.Contains(out, "return amount, nil") {
		t.Errorf("expected 'return amount, nil' inside charge, got:\n%s", out)
	}
	// Cascade: run() also gets (int, error) return
	if !strings.Contains(out, "func run() (int, error)") {
		t.Errorf("expected run() to gain (int, error) return via cascade, got:\n%s", out)
	}
	// Cascade: call site in run() handles error
	if !strings.Contains(out, "result, err := charge(100)") {
		t.Errorf("expected multi-return call site for charge(), got:\n%s", out)
	}
	if !strings.Contains(out, "if err != nil") {
		t.Errorf("expected error check after charge() call, got:\n%s", out)
	}
	// Error must be returned upward, not silently dropped
	if !strings.Contains(out, "return 0, err") {
		t.Errorf("expected 'return 0, err' in cascade error propagation, got:\n%s", out)
	}
	// Cascade: return in run() becomes return result, nil
	if !strings.Contains(out, "return result, nil") {
		t.Errorf("expected 'return result, nil' in run(), got:\n%s", out)
	}
}

func TestApproveCascadeTransitive(t *testing.T) {
	// Three-level chain: top → middle → @approve charge
	// Both middle and top must get (T, error) signatures and call-site error handling.
	src := `
module payments
@approve
fn charge(amount: Int) -> Int {
    return amount
}
fn middle(amount: Int) -> Int {
    let result = charge(amount)
    return result
}
fn top(amount: Int) -> Int {
    let result = middle(amount)
    return result
}
`
	out := emitSrcWithApproval(t, src)
	mustGoSyntax(t, out)

	// middle gets (int, error) via cascade
	if !strings.Contains(out, "func middle(amount int) (int, error)") {
		t.Errorf("expected middle() to get (int, error) return, got:\n%s", out)
	}
	// top gets (int, error) via transitive cascade
	if !strings.Contains(out, "func top(amount int) (int, error)") {
		t.Errorf("expected top() to get (int, error) return, got:\n%s", out)
	}
	// middle call site in top: multi-return + error check
	if !strings.Contains(out, "result, err := middle(amount)") {
		t.Errorf("expected multi-return call to middle() in top(), got:\n%s", out)
	}
	// Error propagated upward in top
	if !strings.Contains(out, "return 0, err") {
		t.Errorf("expected 'return 0, err' in top() cascade, got:\n%s", out)
	}
}

func TestApproveMainExit(t *testing.T) {
	src := `
module main
@approve
fn doWork() {
    println("done")
}
fn main() {
    doWork()
}
`
	out := emitSrcWithApproval(t, src)
	mustGoSyntax(t, out)

	// doWork() call must still be present
	if !strings.Contains(out, "doWork()") {
		t.Errorf("expected doWork() call in main(), got:\n%s", out)
	}

	// main() must NOT get an error return (Go forbids it)
	if strings.Contains(out, "func main() error") {
		t.Errorf("main() must not have error return, got:\n%s", out)
	}
	// main() call site: graceful exit, not return err
	if !strings.Contains(out, "os.Exit(1)") {
		t.Errorf("expected os.Exit(1) in main() denial path, got:\n%s", out)
	}
	if !strings.Contains(out, `fmt.Fprintf(os.Stderr`) {
		t.Errorf("expected fmt.Fprintf in main() denial path, got:\n%s", out)
	}
}

func TestApprovalAdapterSwap(t *testing.T) {
	src := `
module payments
@approve
fn processPayment(amount: Int) {
    let x = amount
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)

	if !strings.Contains(out, "type ApprovalAdapter interface") {
		t.Errorf("expected ApprovalAdapter interface, got:\n%s", out)
	}
	if !strings.Contains(out, "func SetApprovalAdapter") {
		t.Errorf("expected SetApprovalAdapter swap function, got:\n%s", out)
	}
	if !strings.Contains(out, "splashStdinApproval") {
		t.Errorf("expected splashStdinApproval default impl, got:\n%s", out)
	}
	if !strings.Contains(out, `"bufio"`) {
		t.Errorf("expected bufio import, got:\n%s", out)
	}
}

func TestEmitFileEndToEnd(t *testing.T) {
	src := `
module main
type Point {
    x: Int
    y: Int
}
fn add(a: Int, b: Int) -> Int {
    return a + b
}
fn main() {
    let result = add(1, 2)
    let pt = Point { x: result, y: 0 }
    let r = pt
}
`
	out := emitSrc(t, src)
	mustGoSyntax(t, out)
	if !strings.Contains(out, "package main") {
		t.Errorf("expected 'package main', got:\n%s", out)
	}
	if !strings.Contains(out, "type Point struct") {
		t.Errorf("expected Point struct, got:\n%s", out)
	}
	if !strings.Contains(out, "func add(a int, b int) int") {
		t.Errorf("expected add signature, got:\n%s", out)
	}
	if !strings.Contains(out, "Point{x: result, y: 0}") {
		t.Errorf("expected Point struct literal, got:\n%s", out)
	}
}
