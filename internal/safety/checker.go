// Package safety runs enforcement passes over the call graph: @redline, @approve, @containment.
package safety

import (
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

	diags = append(diags, c.checkRedline(file, g, agentReachable)...)
	diags = append(diags, c.checkApprove(file, g, agentReachable)...)

	return diags
}

// checkRedline emits an error for every @redline function that is agent-reachable.
func (c *Checker) checkRedline(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
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
		if agentReachable[fn.Name] {
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"@redline function %q is reachable from an agent context",
				fn.Name,
			))
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
