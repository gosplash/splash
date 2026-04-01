package typechecker_test

import (
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
