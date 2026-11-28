package compiler

import (
	"maps"
	"slices"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
)

// Function contains detached graph and effect facts for inspection.
type Function struct {
	Name            string
	Flags           []string
	Callees         []string
	UnresolvedCalls int
	Declared        effects.EffectSet
	Inferred        effects.EffectSet
	Unused          effects.EffectSet
	Gap             *effects.CoverageGap
}

// Functions returns deterministic graph and effect facts even if verification fails.
func (p *AnalyzedProgram) Functions() []Function {
	if p == nil || p.snapshot == nil {
		return nil
	}
	snapshot := p.snapshot
	summaries := make(map[string]*effects.Summary, len(snapshot.effects))
	for _, summary := range snapshot.effects {
		summaries[snapshot.graph.FunctionKey(summary.Function)] = summary
	}
	nodes := snapshot.graph.Nodes()
	functions := make([]Function, 0, len(nodes))
	for _, node := range nodes {
		summary := summaries[node.Name]
		function := Function{
			Name: node.Name, Callees: node.Callees(), UnresolvedCalls: len(node.UnresolvedCalls),
			Declared: summary.Declared, Inferred: summary.Inferred, Unused: summary.UnusedGrants(),
		}
		if gap := summary.Gap; gap != nil {
			copy := *gap
			function.Gap = &copy
		}
		if node.Effects.Has(effects.Agent) {
			function.Flags = append(function.Flags, "agent")
		}
		if node.HasAnnotation(ast.AnnotTool) {
			function.Flags = append(function.Flags, "tool")
		}
		if node.HasAnnotation(ast.AnnotApprove) {
			function.Flags = append(function.Flags, "approve")
		}
		if node.HasAnnotation(ast.AnnotRedline) {
			function.Flags = append(function.Flags, "redline")
		}
		functions = append(functions, function)
	}
	return functions
}

// AgentRoots returns the resolved policy roots in name order.
func (p *AnalyzedProgram) AgentRoots() []string {
	if p == nil || p.snapshot == nil {
		return nil
	}
	return p.snapshot.graph.AgentRoots()
}

// Approvals returns the approval functions and their callers in name order.
func (p *AnalyzedProgram) Approvals() (functions, callers []string) {
	if p == nil || p.snapshot == nil {
		return nil, nil
	}
	g := p.snapshot.graph
	gated := g.ApprovalFunctions()
	return slices.Sorted(maps.Keys(gated)), slices.Sorted(maps.Keys(g.Callers(gated)))
}
