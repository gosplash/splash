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
@redline
fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent-reachable function is @redline")
	}
}

func TestRedline_NonAgentCanCall(t *testing.T) {
	src := `
module foo
fn safe_caller() -> String { return dangerous() }
@redline
fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: non-agent can call @redline: %v", diags)
	}
}

func TestRedline_TransitiveViolation(t *testing.T) {
	// agent -> helper -> dangerous: should still error
	src := `
module foo
fn run_agent() needs Agent -> String { return helper() }
fn helper() -> String { return dangerous() }
@redline
fn dangerous() -> String { return "boom" }
`
	diags := check(src)
	if !hasError(diags) {
		t.Error("expected error: agent transitively reaches @redline function")
	}
}

func TestRedline_ErrorContainsCallPath(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return helper() }
fn helper() -> String { return dangerous() }
@redline
fn dangerous() -> String { return "boom" }
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
@redline
fn dangerous() -> String { return "ok" }
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
		t.Error("expected error: agent-reachable function with DB.write+Net requires @approve")
	}
}

func TestApprove_WithApproveAnnotation_NoError(t *testing.T) {
	src := `
module foo
fn run_agent() needs Agent -> String { return exfil() }
@approve
fn exfil() needs DB.write, Net -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected errors: @approve present: %v", diags)
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
		t.Errorf("unexpected errors: DB.write alone should not require @approve: %v", diags)
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
		t.Error("expected error: approved_only requires @approve or @agent_allowed")
	}
}

func TestContainment_ApprovedOnly_WithApprove_NoError(t *testing.T) {
	src := `
@containment(agent: "approved_only")
module payments
fn run_agent() needs Agent -> String { return process() }
@approve
fn process() needs DB.write -> String { return "ok" }
`
	diags := check(src)
	if hasError(diags) {
		t.Errorf("unexpected error: @approve satisfies approved_only: %v", diags)
	}
}
