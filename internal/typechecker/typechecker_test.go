package typechecker_test

import (
	"fmt"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

func check(src string) []diagnostic.Diagnostic {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	tc := typechecker.New()
	_, diags := tc.Check(file)
	return diags
}

func hasError(diags []diagnostic.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == diagnostic.Error {
			return true
		}
	}
	return false
}

func TestForwardReference(t *testing.T) {
	// Greet references Greeting which is declared after it — must work
	src := `
module greeter
fn greet(name: String) -> Greeting {
  return Greeting { message: "hi" }
}
type Greeting {
  message: String
}`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestUnknownType(t *testing.T) {
	src := `
module foo
fn bad() -> NonExistent { }`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error for unknown type NonExistent")
	}
}

func TestOptionalType(t *testing.T) {
	src := `
module foo
fn greet(name: String?) -> String {
  return name ?? "stranger"
}`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestConstraintSatisfaction_Loggable_Sensitive(t *testing.T) {
	// @sensitive field prevents Loggable satisfaction — no declaration needed
	src := `
module foo
type User {
    id: String
    @sensitive
    email: String
}
fn log_user<T: Loggable>(val: T) -> String {
    return "ok"
}
fn bad(u: User) -> String {
    return log_user(u)
}`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: User has @sensitive field and cannot satisfy Loggable")
	}
}

func TestConstraintSatisfaction_PublicType_Loggable(t *testing.T) {
	// Public types satisfy Loggable — no declaration needed
	src := `
module foo
type Point {
    x: Int
    y: Int
}
fn log_point<T: Loggable>(val: T) -> String {
    return "ok"
}
fn ok_usage(p: Point) -> String {
    return log_point(p)
}`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestBinaryExprArithmeticType(t *testing.T) {
	// arithmetic on Int should resolve to Int, not Bool
	src := `
module foo
fn add(a: Int, b: Int) -> Int {
  return a + b
}`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors for valid Int arithmetic: %v", diags)
	}
}

func TestBinaryExprComparisonReturnsBool(t *testing.T) {
	// comparison should return Bool — assigning to Int should error
	src := `
module foo
fn cmp(a: Int, b: Int) -> Int {
  return a == b
}`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: comparison returns Bool, not Int")
	}
}

func TestReturnTypeMismatch(t *testing.T) {
	src := `
module foo
fn greet() -> String {
  return 42
}`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: returning Int from String function")
	}
}

func TestReturnTypeMatch(t *testing.T) {
	src := `
module foo
fn greet() -> String {
  return "hello"
}`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestMemberAccess_BasicField(t *testing.T) {
	src := `
module demo
type Person {
  name: String
  age: Int
}
fn get_name(p: Person) -> String {
  return p.name
}
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for basic field access")
	}
}

func TestMemberAccess_OptionalField(t *testing.T) {
	src := `
module demo
type Person {
  name: String
  nickname: String?
}
fn get_nick(p: Person) -> String? {
  return p.nickname
}
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for optional field access")
	}
}

func TestMemberAccess_UnknownField(t *testing.T) {
	src := `
module demo
type Person {
  name: String
}
fn bad(p: Person) -> String {
  return p.nonexistent
}
`
	if !hasError(check(src)) {
		t.Error("expected type error for unknown field access")
	}
}

func TestMemberAccess_OptionalChaining(t *testing.T) {
	// p?.name where p: Person? — ?. returns String? (nil-propagating)
	src := `
module demo
type Person {
  name: String
}
fn get_name(p: Person?) -> String? { return p?.name }
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for optional chaining")
	}
}

func TestMemberAccess_ChainedAccess(t *testing.T) {
	// Access two different fields in the same function
	src := `
module demo
type Point {
  x: Int
  y: Int
}
fn sum(p: Point) -> Int { return p.x + p.y }
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for chained field access")
	}
}

func TestMemberAccess_HelloSplashPattern(t *testing.T) {
	// The hello.splash pattern: struct literal + optional field + null coalesce
	src := `
module hello
type Person {
  name: String
  nickname: String?
}
fn greet(p: Person) -> String {
  let display_name = p.nickname ?? p.name
  return "Hello, " + display_name
}
fn main() {
  let p = Person { name: "world", nickname: none }
  println(greet(p))
}
`
	if hasError(check(src)) {
		t.Error("unexpected type errors in hello.splash pattern")
	}
}

func TestMemberAccess_WrongReturnType(t *testing.T) {
	// p.age is Int — returning it as String should be a type error
	src := `
module demo
type Person {
  name: String
  age: Int
}
fn bad(p: Person) -> String { return p.age }
`
	if !hasError(check(src)) {
		t.Error("expected type error: returning Int field as String")
	}
}

func TestAIPrompt_BasicReturn(t *testing.T) {
	// ai.prompt<SermonInsight>(text) should return Result<SermonInsight, AIError>
	// and be valid as the return value of a function returning that type.
	src := `
module demo
use std/ai

type SermonInsight {
  title: String
}

async fn analyze(text: String) needs AI -> SermonInsight {
  return ai.prompt<SermonInsight>(text)
}
`
	if hasError(check(src)) {
		t.Error("unexpected type errors for ai.prompt<T> call")
	}
}

func TestAIPrompt_WrongReturnType(t *testing.T) {
	// ai.prompt<Foo> returns Result<Foo, AIError>, not Result<Bar, AIError>
	src := `
module demo
use std/ai

type Foo { x: Int }
type Bar { y: String }

async fn bad(text: String) needs AI -> Bar {
  return ai.prompt<Foo>(text)
}
`
	if !hasError(check(src)) {
		t.Error("expected type error: ai.prompt<Foo> returned as Result<Bar, AIError>")
	}
}

func TestAIPrompt_NoStdAi_AiIsUndefined(t *testing.T) {
	// Without use std/ai, 'ai' is not in scope
	src := `
module demo

type Foo { x: Int }

fn bad(text: String) -> Foo {
  return ai.prompt<Foo>(text)
}
`
	if !hasError(check(src)) {
		t.Error("expected error: ai is undefined without use std/ai")
	}
}

func TestPrintln_SensitiveArgument_Error(t *testing.T) {
	src := `
module users
type User {
    id: Int
    @sensitive
    email: String
}
fn debug(u: User) {
    println(u)
}
`
	diags := check(src)
	if len(diags) == 0 {
		t.Fatal("expected error: println with @sensitive type User")
	}
	found := false
	for _, d := range diags {
		if hasError([]diagnostic.Diagnostic{d}) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error diagnostic, got: %v", diags)
	}
}

func TestPrintln_PublicArgument_NoError(t *testing.T) {
	src := `
module foo
fn greet(name: String) {
    println(name)
}
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("expected no error for println with public String, got: %v", diags)
	}
}

func TestPrintln_RestrictedArgument_Error(t *testing.T) {
	src := `
module users
type Profile {
    id: Int
    @restricted
    ssn: String
}
fn debug(p: Profile) {
    println(p)
}
`
	diags := check(src)
	if len(diags) == 0 {
		t.Fatal("expected error: println with @restricted type Profile")
	}
	if !hasError(diags) {
		t.Errorf("expected an error diagnostic, got: %v", diags)
	}
}

// checkWithImports type-checks src with an in-memory filesystem for imports.
// files maps module paths (e.g. "billing") to their Splash source content.
func checkWithImports(mainSrc string, files map[string]string) []diagnostic.Diagnostic {
	_, _, diags := checkTypedWithImports(mainSrc, files)
	return diags
}

func checkTypedWithImports(mainSrc string, files map[string]string) (*typechecker.TypedFile, []*typechecker.TypedFile, []diagnostic.Diagnostic) {
	toks := lexer.New("test.splash", mainSrc).Tokenize()
	p := parser.New("test.splash", toks)
	file, parseDiags := p.ParseFile()
	tc := typechecker.New()
	tc.SetFileLoader(".", func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("module not found: %s", path)
		}
		return []byte(content), nil
	})
	typed, diags := tc.Check(file)
	return typed, tc.LoadedTypedFiles(), append(parseDiags, diags...)
}

func TestUseDecl_ImportedTypeAvailable(t *testing.T) {
	// A type defined in an imported module should be usable in the importing file.
	mainSrc := `
module app
use billing
fn run() -> Charge {
    return Charge { customer_id: 1, amount_cents: 100 }
}
`
	billingContent := `
module billing
type Charge {
    customer_id: Int
    amount_cents: Int
}
`
	diags := checkWithImports(mainSrc, map[string]string{
		"billing.splash": billingContent,
	})
	if hasError(diags) {
		t.Errorf("expected no errors after importing billing module, got: %v", diags)
	}
}

func TestUseDecl_ImportedFunctionAvailable(t *testing.T) {
	// A function defined in an imported module should be callable in the importing file.
	mainSrc := `
module app
use billing
fn run() -> Int {
    return get_amount()
}
`
	billingContent := `
module billing
fn get_amount() -> Int {
    return 100
}
`
	diags := checkWithImports(mainSrc, map[string]string{
		"billing.splash": billingContent,
	})
	if hasError(diags) {
		t.Errorf("expected no errors after importing billing module, got: %v", diags)
	}
}

func TestUseDecl_ImportedFunctionAvailableByNamespace(t *testing.T) {
	mainSrc := `
module app
use billing
fn run() -> Int {
    return billing.get_amount()
}
`
	billingContent := `
module billing
fn get_amount() -> Int {
    return 100
}
`
	diags := checkWithImports(mainSrc, map[string]string{
		"billing.splash": billingContent,
	})
	if hasError(diags) {
		t.Errorf("expected no errors after namespaced import access, got: %v", diags)
	}
}

func TestUseDecl_ImportedTypeAvailableByNamespace(t *testing.T) {
	mainSrc := `
module app
use billing
fn run() -> billing.Charge {
    return billing.Charge { customer_id: 1, amount_cents: 100 }
}
`
	billingContent := `
module billing
type Charge {
    customer_id: Int
    amount_cents: Int
}
`
	diags := checkWithImports(mainSrc, map[string]string{
		"billing.splash": billingContent,
	})
	if hasError(diags) {
		t.Errorf("expected no errors after namespaced type access, got: %v", diags)
	}
}

func TestUseDecl_RetainsOwningRecordTypes(t *testing.T) {
	main := `
module app
use primary as first
use secondary as second
use primary as again
fn sample(other: second.Entry) -> first.Entry {
    let entry: first.Entry = again.make()
    return entry
}
`
	typed, imports, diags := checkTypedWithImports(main, map[string]string{
		"primary.splash": `
module primary
fn make() -> Entry { return Entry { count: 1 } }
type Entry { count: Int }
`,
		"secondary.splash": `
module secondary
type Entry { count: Int }
`,
	})
	if hasError(diags) {
		t.Fatalf("unexpected errors: %v", diags)
	}
	if len(imports) != 2 {
		t.Fatalf("loaded %d modules, want 2", len(imports))
	}
	first := imports[0].File.Declarations[1].(*ast.TypeDecl)
	second := imports[1].File.Declarations[0].(*ast.TypeDecl)
	makeEntry := imports[0].File.Declarations[0].(*ast.FunctionDecl)
	fn := typed.File.Declarations[0].(*ast.FunctionDecl)
	let := fn.Body.Stmts[0].(*ast.LetStmt)
	if field := imports[0].Types[first.Fields[0].Type]; field != types.Int {
		t.Errorf("first.Entry.count type = %v, want Int", field)
	}
	for _, fact := range []struct {
		name string
		got  types.Type
		want *ast.TypeDecl
	}{
		{"parameter", typed.Types[fn.Params[0].Type], second},
		{"return", typed.Types[fn.ReturnType], first},
		{"let", typed.Types[let.Type], first},
		{"call result", typed.Types[let.Value], first},
		{"local literal", imports[0].Types[makeEntry.Body.Stmts[0].(*ast.ReturnStmt).Value], first},
	} {
		if named, ok := fact.got.(*types.NamedType); !ok || named.Declaration != fact.want {
			t.Errorf("%s type = %v, want record declared at %v", fact.name, fact.got, fact.want.Pos())
		}
	}
}

func TestUseDecl_ProjectsRecordFieldsFromDefiningModule(t *testing.T) {
	main := `
module app
use primary as first
use secondary as second
fn sample() -> Int { return first.make().detail.count }
`
	typed, _, diags := checkTypedWithImports(main, map[string]string{
		"primary.splash": `
module primary
expose make
type Detail { count: Int }
type Entry { detail: Detail }
fn make() -> Entry { return Entry { detail: Detail { count: 1 } } }
`,
		"secondary.splash": `
module secondary
type Entry { detail: String }
type Detail { count: String }
`,
	})
	if hasError(diags) {
		t.Fatalf("unexpected errors: %v", diags)
	}
	fn := typed.File.Declarations[0].(*ast.FunctionDecl)
	projection := fn.Body.Stmts[0].(*ast.ReturnStmt).Value
	if got := typed.Types[projection]; got != types.Int {
		t.Errorf("projected type = %v, want Int", got)
	}
}

func TestUseDecl_RejectsSameShapedRecordSubstitution(t *testing.T) {
	files := map[string]string{
		"primary.splash": `
module primary
fn accept(value: Entry) -> Int { return value.count }
type Entry { count: Int }
`,
		"secondary.splash": `
module secondary
type Entry { count: Int }
`,
	}
	const imports = "\nmodule app\nuse primary as first\nuse secondary as second\n"
	for _, test := range []struct{ name, body, want string }{
		{"return", `fn sample() -> first.Entry { return second.Entry { count: 1 } }`, "cannot return"},
		{"let", `fn sample() { let item: first.Entry = second.Entry { count: 1 } }`, "cannot assign"},
		{"argument", `fn sample() -> Int { return first.accept(second.Entry { count: 1 }) }`, "argument"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diags := checkWithImports(imports+test.body+"\n", files)
			for _, diag := range diags {
				if diag.Severity == diagnostic.Error && strings.Contains(diag.Message, test.want) {
					return
				}
			}
			t.Errorf("expected %q error, got %v", test.want, diags)
		})
	}
}

func TestAgentKeywordFunctionTypechecks(t *testing.T) {
	src := `
module app
agent fn run_agent() -> String {
    return "ok"
}
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("expected agent keyword function to type-check, got: %v", diags)
	}
}

func TestUseDecl_MissingModule_Error(t *testing.T) {
	// Importing a module that doesn't exist should produce an error.
	mainSrc := `
module app
use missing_module
fn run() -> Int { return 0 }
`
	diags := checkWithImports(mainSrc, map[string]string{})
	if !hasError(diags) {
		t.Errorf("expected error for missing module, got no errors")
	}
}

func TestUseDecl_CircularImport_Error(t *testing.T) {
	// Circular imports should produce an error.
	// app uses billing; billing uses app — cycle.
	mainSrc := `
module app
use billing
fn run() -> Int { return 0 }
`
	billingContent := `
module billing
use app
fn charge() -> Int { return 0 }
`
	diags := checkWithImports(mainSrc, map[string]string{
		"billing.splash": billingContent,
		"app.splash":     mainSrc,
	})
	if !hasError(diags) {
		t.Errorf("expected error for circular import, got no errors")
	}
}

// Loggable is a built-in constraint — programs must not need to declare it.

func TestLoggable_BuiltIn_NoDeclarationNeeded(t *testing.T) {
	// Loggable should be usable without declaring constraint Loggable { ... }
	src := `
module foo
type Point {
    x: Int
    y: Int
}
fn log_it<T: Loggable>(val: T) -> String {
    return "ok"
}
fn use_it(p: Point) -> String {
    return log_it(p)
}
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("Loggable should be built-in — no declaration required, got: %v", diags)
	}
}

func TestLoggable_BuiltIn_SensitiveTypeRejected(t *testing.T) {
	// @sensitive type must be rejected even without a constraint declaration
	src := `
module foo
type User {
    id: Int
    @sensitive
    email: String
}
fn log_it<T: Loggable>(val: T) -> String {
    return "ok"
}
fn bad(u: User) -> String {
    return log_it(u)
}
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: User has @sensitive field and cannot satisfy built-in Loggable")
	}
}

func TestLoggable_UnknownConstraint_Error(t *testing.T) {
	// An undeclared, non-built-in constraint name should produce an error
	src := `
module foo
type Point {
    x: Int
    y: Int
}
fn do_it<T: NonExistentConstraint>(val: T) -> String {
    return "ok"
}
fn use_it(p: Point) -> String {
    return do_it(p)
}
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: NonExistentConstraint is not declared and not built-in")
	}
}

func TestDuplicateRecordDeclaration(t *testing.T) {
	_, _, diags := checkTypedWithImports(`module app
type Entry { count: Int }
type Entry { count: Int }
`, nil)
	for _, diag := range diags {
		if diag.Severity == diagnostic.Error && strings.Contains(diag.Message, `duplicate type "Entry"`) {
			if diag.Pos.File != "test.splash" || diag.Pos.Line != 3 || diag.Pos.Column < 1 {
				t.Errorf("duplicate declaration reported at %v, want test.splash:3", diag.Pos)
			}
			return
		}
	}
	t.Errorf("expected a duplicate type declaration error, got %v", diags)
}

func TestUseDecl_RejectsAmbiguousRecordNames(t *testing.T) {
	files := map[string]string{
		"primary.splash":   "module primary\ntype Entry { count: Int }\n",
		"secondary.splash": "module secondary\ntype Entry { count: Int }\n",
	}
	for _, test := range []struct{ name, aliases, body, want string }{
		{"plain annotation", "use primary as first\nuse secondary as second\n", `fn read(value: Entry) {}`, `"Entry"`},
		{"qualified annotation", "use primary as shared\nuse secondary as shared\n", `fn read(value: shared.Entry) {}`, `"shared.Entry"`},
		{"qualified literal", "use primary as shared\nuse secondary as shared\n", `fn read() { let value = shared.Entry { count: 1 } }`, `"shared.Entry"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			diags := checkWithImports("module app\n"+test.aliases+test.body+"\n", files)
			for _, diag := range diags {
				if diag.Severity == diagnostic.Error && strings.Contains(diag.Message, "ambiguous imported name "+test.want) {
					if diag.Pos.File != "test.splash" || diag.Pos.Line != 4 || diag.Pos.Column < 1 {
						t.Errorf("ambiguous name reported at %v, want test.splash:4", diag.Pos)
					}
					return
				}
			}
			t.Errorf("expected an ambiguity error for %s, got %v", test.want, diags)
		})
	}
}

func TestGenericParameterShadowsRecordTypeInCalls(t *testing.T) {
	diags := check(`module app
type T { n: Int }
fn accept<T>(value: T) {}
fn sample() { accept("text") }
`)
	if hasError(diags) {
		t.Errorf("unexpected errors for generic argument: %v", diags)
	}
}
