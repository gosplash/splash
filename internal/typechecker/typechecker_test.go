package typechecker_test

import (
	"fmt"
	"testing"

	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/typechecker"
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
	// @sensitive field prevents Loggable satisfaction
	src := `
module foo
constraint Loggable {
  fn to_log_string(self) -> String
}
type User {
  id: String
  @sensitive
  email: String
}
fn log_user<T: Loggable>(val: T) -> String {
  return val.to_log_string()
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
	src := `
module foo
constraint Loggable {
  fn to_log_string(self) -> String
}
type Point {
  x: Int
  y: Int
}
fn log_point<T: Loggable>(val: T) -> String {
  return val.to_log_string()
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

async fn analyze(text: String) needs AI -> Result<SermonInsight, AIError> {
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

async fn bad(text: String) needs AI -> Result<Bar, AIError> {
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
	toks := lexer.New("test.splash", mainSrc).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	tc := typechecker.New()
	tc.SetFileLoader(".", func(path string) ([]byte, error) {
		content, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("module not found: %s", path)
		}
		return []byte(content), nil
	})
	_, diags := tc.Check(file)
	return diags
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
