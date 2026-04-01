// Package safety runs enforcement passes over the call graph: @redline, @approve, @containment.
package safety

import (
	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
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
