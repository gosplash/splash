// Package safety runs enforcement passes over the call graph: @redline, @approve, @containment.
package safety

import (
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/effects"
)

// Checker runs safety enforcement passes over a parsed file and its call graph.
type Checker struct{}

// New creates a new Checker.
func New() *Checker { return &Checker{} }

// Check runs all enforcement passes and returns any violations as diagnostics.
func (c *Checker) Check(file *ast.File, g *callgraph.Graph) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	roots := g.AgentRoots()
	agentReachable := g.Reachable(roots)
	agentParents := g.Parents(roots)

	diags = append(diags, c.checkRedline(file, g, agentParents)...)
	diags = append(diags, c.checkApprove(file, g, agentReachable)...)
	diags = append(diags, c.checkContainment(file, g, agentReachable)...)

	return diags
}

// checkRedline emits an error for every @redline function that is agent-reachable.
// The error message includes the full call path from the agent entry point to the violation.
func (c *Checker) checkRedline(file *ast.File, g *callgraph.Graph, agentParents map[string]string) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		node := g.Node(fn.Name)
		if node == nil {
			continue
		}
		if !node.HasAnnotation(ast.AnnotRedline) {
			continue
		}
		if _, reachable := agentParents[fn.Name]; reachable {
			path := callgraph.PathTo(agentParents, fn.Name)
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"@redline function %q is reachable from an agent context\n  call path: %s",
				fn.Name,
				strings.Join(path, " → "),
			))
		}
	}

	return diags
}

type containmentPolicy int

const (
	containmentNone         containmentPolicy = iota // no agent access
	containmentReadOnly                              // DB.read only
	containmentApprovedOnly                          // @approve or @agent_allowed required
	containmentUnrestricted                          // no containment annotation
)

// moduleContainment reads the @containment annotation from the module declaration.
func moduleContainment(file *ast.File) containmentPolicy {
	if file.Module == nil {
		return containmentUnrestricted
	}
	for _, ann := range file.Module.Annotations {
		if ann.Kind != ast.AnnotContainment {
			continue
		}
		agentExpr, ok := ann.Args["agent"]
		if !ok {
			continue
		}
		lit, ok := agentExpr.(*ast.StringLiteral)
		if !ok {
			continue
		}
		switch lit.Value {
		case "none":
			return containmentNone
		case "read_only":
			return containmentReadOnly
		case "approved_only":
			return containmentApprovedOnly
		}
	}
	return containmentUnrestricted
}

// checkContainment enforces the module-level @containment policy against the agent-reachable set.
func (c *Checker) checkContainment(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
	policy := moduleContainment(file)
	if policy == containmentUnrestricted {
		return nil
	}

	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !agentReachable[fn.Name] {
			continue
		}
		node := g.Node(fn.Name)
		if node == nil {
			continue
		}

		switch policy {
		case containmentNone:
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"function %q is agent-reachable but module has @containment(agent: \"none\")",
				fn.Name,
			))
		case containmentReadOnly:
			if node.Effects.Has(effects.DBWrite) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q has DB.write but module has @containment(agent: \"read_only\")",
					fn.Name,
				))
			}
		case containmentApprovedOnly:
			// Agent-root functions (those with the Agent effect) are the entry points;
			// only non-root agent-reachable functions need explicit annotation.
			if node.Effects.Has(effects.Agent) {
				continue
			}
			if !node.HasAnnotation(ast.AnnotApprove) && !node.HasAnnotation(ast.AnnotAgentAllowed) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q is agent-reachable but module has @containment(agent: \"approved_only\") — add @approve or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}

// checkApprove emits an error for every agent-reachable function that combines
// DBWrite and Net effects without @approve or @agent_allowed.
func (c *Checker) checkApprove(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !agentReachable[fn.Name] {
			continue
		}
		node := g.Node(fn.Name)
		if node == nil {
			continue
		}
		// Policy: DB.write + Net in agent context requires explicit approval.
		if node.Effects.Has(effects.DBWrite) && node.Effects.Has(effects.Net) {
			if !node.HasAnnotation(ast.AnnotApprove) && !node.HasAnnotation(ast.AnnotAgentAllowed) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q combines DB.write and Net effects in an agent context — add @approve or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}
