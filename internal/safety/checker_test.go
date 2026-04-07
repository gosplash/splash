// internal/safety/checker_test.go
package safety_test

import (
	"testing"

	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/safety"
)

func check(src string) []diagnostic.Diagnostic {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	g := callgraph.Build(file)
	c := safety.New()
	return c.Check(file, g)
}

func hasError(diags []diagnostic.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == diagnostic.Error {
			return true
		}
	}
	return false
}

func TestRedline_AgentCannotReach(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return dangerous() }
redline fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent-reachable function is redline")
	}
}

func TestRedline_NonAgentCanCall(t *testing.T) {
	src := `
module foo
fn safe_caller() -> String { return dangerous() }
redline fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: non-agent can call redline: %v", diags)
	}
}

func TestRedline_TransitiveViolation(t *testing.T) {
	// agent -> helper -> dangerous: should still error
	src := `
module foo
fn run_agent() needs Agent -> String { return helper() }
fn helper() -> String { return dangerous() }
redline fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent transitively reaches redline function")
	}
}

func TestRedline_ErrorContainsCallPath(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return helper() }
fn helper() -> String { return dangerous() }
redline fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic")
	}
	msg := diags[0].Message
	for _, want := range []string{"run_agent", "helper", "dangerous", "→"} {
		if !contains(msg, want) {
			t.Errorf("expected call path in error message, missing %q\nfull message: %s", want, msg)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRedline_NoAgent_NoError(t *testing.T) {
	src := `
module foo
redline fn dangerous() -> String { return "ok" }
fn other() -> String { return "hi" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: no agent, no violation: %v", diags)
	}
}

func TestApprove_DBWriteAndNetRequiresApproval(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
fn exfil() needs DB.write, Net -> String { return "leaked" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent-reachable function with DB.write+Net requires approve fn")
	}
}

func TestApprove_WithApproveAnnotation_NoError(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
approve fn exfil() needs DB.write, Net -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: approve fn present: %v", diags)
	}
}

func TestApprove_WithAgentAllowed_NoError(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
@agent_allowed
fn exfil() needs DB.write, Net -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: @agent_allowed present: %v", diags)
	}
}

func TestApprove_DBWriteOnlyNoRequirement(t *testing.T) {
	// DB.write alone (no Net) does not trigger the rule
	src := `
module foo
fn run_agent() needs Agent -> String { return writer() }
fn writer() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: DB.write alone should not require approve fn: %v", diags)
	}
}

func TestContainment_None_BlocksAllAgentAccess(t *testing.T) {
	src := `
@containment(agent: "none")
module payments
fn run_agent() needs Agent -> String { return process() }
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: @containment(agent: none) blocks all agent-reachable functions")
	}
}

func TestContainment_None_NoAgent_NoError(t *testing.T) {
	src := `
@containment(agent: "none")
module payments
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: no agent, containment none is satisfied: %v", diags)
	}
}

func TestContainment_ReadOnly_BlocksWrite(t *testing.T) {
	src := `
@containment(agent: "read_only")
module payments
fn run_agent() needs Agent -> String { return process() }
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: read_only containment blocks DB.write in agent context")
	}
}

func TestContainment_ReadOnly_AllowsRead(t *testing.T) {
	src := `
@containment(agent: "read_only")
module payments
fn run_agent() needs Agent -> String { return query() }
fn query() needs DB.read -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: read_only allows DB.read: %v", diags)
	}
}

func TestContainment_ApprovedOnly_RequiresAnnotation(t *testing.T) {
	src := `
@containment(agent: "approved_only")
module payments
fn run_agent() needs Agent -> String { return process() }
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: approved_only requires approve fn or @agent_allowed")
	}
}

func TestContainment_ApprovedOnly_WithApprove_NoError(t *testing.T) {
	src := `
@containment(agent: "approved_only")
module payments
fn run_agent() needs Agent -> String { return process() }
approve fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: approve fn satisfies approved_only: %v", diags)
	}
}

func TestTool_SensitiveReturnType(t *testing.T) {
	src := `
module users
type User {
    id: Int
    @sensitive
    email: String
}
tool fn get_user(id: Int) -> User {
    return User { id: id, email: "a@b.com" }
}
fn run_agent() needs Agent -> User {
    return get_user(1)
}
`
	diags := check(src)
	if len(diags) == 0 {
		t.Fatal("expected error: tool fn returning @sensitive type, but got no diagnostics")
	}
	found := false
	for _, d := range diags {
		if contains(d.Message, "sensitive") && contains(d.Message, "get_user") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected @sensitive diagnostic for get_user, got: %v", diags)
	}
}

func TestTool_PublicReturnType_NoError(t *testing.T) {
	src := `
module users
type Point {
    x: Int
    y: Int
}
tool fn get_point() -> Point {
    return Point { x: 1, y: 2 }
}
fn run_agent() needs Agent -> Point {
    return get_point()
}
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("expected no error for tool fn returning public type, got: %v", diags)
	}
}

func TestTool_RestrictedReturnType(t *testing.T) {
	src := `
module users
type Profile {
    id: Int
    @restricted
    ssn: String
}
tool fn get_profile(id: Int) -> Profile {
    return Profile { id: id, ssn: "123-45-6789" }
}
fn run_agent() needs Agent -> Profile {
    return get_profile(1)
}
`
	diags := check(src)
	if len(diags) == 0 {
		t.Fatal("expected error: tool fn returning @restricted type, but got no diagnostics")
	}
	found := false
	for _, d := range diags {
		if contains(d.Message, "get_profile") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected diagnostic for get_profile, got: %v", diags)
	}
}

func TestTool_OptionalSensitiveReturnType(t *testing.T) {
	src := `
module users
type User {
    id: Int
    @sensitive
    email: String
}
tool fn find_user(id: Int) -> User? {
    return User { id: id, email: "a@b.com" }
}
fn run_agent() needs Agent -> User? {
    return find_user(1)
}
`
	diags := check(src)
	if len(diags) == 0 {
		t.Fatal("expected error: tool fn returning User? which has @sensitive fields")
	}
	found := false
	for _, d := range diags {
		if contains(d.Message, "find_user") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected diagnostic for find_user, got: %v", diags)
	}
}

func TestSandbox_DenyEffect_Blocked(t *testing.T) {
	src := `
module foo
@sandbox(deny: [Net])
fn run_agent() needs Agent -> String { return fetch() }
fn fetch() needs Net -> String { return "data" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: @sandbox deny:Net blocks agent using Net")
	}
}

func TestSandbox_DenyEffect_Transitive(t *testing.T) {
	src := `
module foo
@sandbox(deny: [Net])
fn run_agent() needs Agent -> String { return helper() }
fn helper() -> String { return fetch() }
fn fetch() needs Net -> String { return "data" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: @sandbox deny blocks transitively reachable Net effect")
	}
}

func TestSandbox_AllowList_PermitsListed(t *testing.T) {
	src := `
module foo
@sandbox(allow: [DB.read])
fn run_agent() needs Agent, DB.read -> String { return query() }
fn query() needs DB.read -> String { return "data" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: @sandbox allow:DB.read permits DB.read: %v", diags)
	}
}

func TestSandbox_AllowList_BlocksUnlisted(t *testing.T) {
	src := `
module foo
@sandbox(allow: [DB.read])
fn run_agent() needs Agent, DB.read -> String { return both() }
fn both() needs DB.read, Net -> String { return "data" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: @sandbox allow:DB.read blocks Net usage")
	}
}

func TestSandbox_NoAnnotation_NoError(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent, Net, DB.read -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: no @sandbox means no constraints: %v", diags)
	}
}

func TestBudget_ValidArgs_NoError(t *testing.T) {
	src := `
module foo
@budget(max_cost: 1.0, max_calls: 10)
fn run_agent() needs Agent -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error for valid @budget args: %v", diags)
	}
}

func TestBudget_MaxCalls_MustBeInt(t *testing.T) {
	src := `
module foo
@budget(max_calls: 1.5)
fn run_agent() needs Agent -> String { return "ok" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: @budget max_calls must be an integer literal")
	}
}
