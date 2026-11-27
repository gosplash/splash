// Package safety runs enforcement passes over the call graph: redline fn,
// approve fn, and @containment.
package safety

import (
	"slices"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

// Checker runs safety enforcement passes over typed modules and their call graph.
type Checker struct{}

// New creates a new Checker.
func New() *Checker { return &Checker{} }

// Check runs all enforcement passes and returns any violations as diagnostics.
func (c *Checker) Check(modules []*typechecker.TypedFile, g *callgraph.Graph, inferred []*effects.Summary) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	roots := g.AgentRoots()
	agentReachable := g.Reachable(roots)
	agentParents := g.Parents(roots)

	diags = append(diags, c.checkUnresolvedCalls(g, roots)...)
	diags = append(diags, c.checkApprovalCallContexts(g)...)
	for _, module := range modules {
		file := module.File
		diags = append(diags, c.checkRedline(file, g, agentParents)...)
		diags = append(diags, c.checkApprove(file, g, agentReachable)...)
		diags = append(diags, c.checkContainment(file, g, agentReachable)...)
		diags = append(diags, c.checkToolDataClassification(module)...)
		diags = append(diags, c.checkSandbox(file, g)...)
		diags = append(diags, c.checkBudget(file)...)
	}
	for _, summary := range inferred {
		diags = append(diags, summary.Diagnostics()...)
	}

	return diags
}

// checkUnresolvedCalls rejects incomplete reachability in regions that need
// resolved identities for policy checks. Approval propagation also requires
// complete call targets so indirect calls cannot discard the error channel.
func (c *Checker) checkUnresolvedCalls(g *callgraph.Graph, roots []string) []diagnostic.Diagnostic {
	if !g.HasUnresolvedCalls() {
		return nil
	}
	nodes := g.Nodes()
	var hasApproval bool
	for _, node := range nodes {
		if node.HasAnnotation(ast.AnnotSandbox) {
			roots = append(roots, node.Name)
		}
		if node.HasAnnotation(ast.AnnotApprove) {
			hasApproval = true
		}
	}
	reachable := g.Reachable(roots)

	var diags []diagnostic.Diagnostic
	for _, node := range nodes {
		if hasApproval || reachable[node.Name] {
			for _, pos := range node.UnresolvedCalls {
				diags = append(diags, diagnostic.Errorf(pos, "cannot verify indirect call target in %q under agent, sandbox, or approval checks", node.Name))
			}
		}
	}
	return diags
}

// checkApprovalCallContexts rejects uses of an approval error channel that the
// current backend can only propagate for a whole statement, let or return value.
func (c *Checker) checkApprovalCallContexts(g *callgraph.Graph) []diagnostic.Diagnostic {
	gated := g.ApprovalFunctions()
	if len(gated) == 0 {
		return nil
	}
	withErrorChannel := g.Callers(gated)
	var diags []diagnostic.Diagnostic
	for _, node := range g.Nodes() {
		for _, call := range node.NestedCalls {
			if withErrorChannel[call.Target] {
				diags = append(diags, diagnostic.Errorf(call.Position,
					"approval call to %q must be a standalone statement, let value, or return value", call.Target))
			}
		}
	}
	return diags
}

// sensitiveFields returns the names of @sensitive or @restricted fields in a type declaration.
func sensitiveFields(decl *ast.TypeDecl) []string {
	var names []string
	for _, field := range decl.Fields {
		for _, ann := range field.Annotations {
			if ann.Kind == ast.AnnotSensitive || ann.Kind == ast.AnnotRestricted {
				names = append(names, field.Name)
				break
			}
		}
	}
	return names
}

// checkToolDataClassification emits an error for every tool function whose
// return type contains @sensitive or @restricted fields.
// PII in a tool's return value flows directly into the AI agent's context window.
func (c *Checker) checkToolDataClassification(module *typechecker.TypedFile) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic
	for _, declaration := range module.File.Declarations {
		fn, ok := declaration.(*ast.FunctionDecl)
		if !ok || !slices.ContainsFunc(fn.Annotations, func(ann ast.Annotation) bool {
			return ann.Kind == ast.AnnotTool
		}) {
			continue
		}
		returnType := module.Types[fn.ReturnType]
		if optional, ok := returnType.(*types.OptionalType); ok {
			returnType = optional.Inner
		}
		record, ok := returnType.(*types.NamedType)
		if !ok || record.Declaration == nil {
			continue
		}
		if fields := sensitiveFields(record.Declaration); len(fields) > 0 {
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"tool fn %q returns type %s which contains sensitive or restricted fields (%s) — PII would flow into the AI agent's context",
				fn.Name, record.TypeName(), strings.Join(fields, ", "),
			))
		}
	}
	return diags
}

// checkRedline emits an error for every redline function that is agent-reachable.
// The error message includes the full call path from the agent entry point to the violation.
func (c *Checker) checkRedline(file *ast.File, g *callgraph.Graph, agentParents map[string]string) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		node := g.Node(g.FunctionKey(fn))
		if node == nil {
			continue
		}
		if !node.HasAnnotation(ast.AnnotRedline) {
			continue
		}
		if _, reachable := agentParents[g.FunctionKey(fn)]; reachable {
			path := callgraph.PathTo(agentParents, g.FunctionKey(fn))
			diags = append(diags, diagnostic.Errorf(
				fn.Position,
				"redline fn %q is reachable from an agent context\n  call path: %s",
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
	containmentApprovedOnly                          // approve fn or @agent_allowed required
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
		if !agentReachable[g.FunctionKey(fn)] {
			continue
		}
		node := g.Node(g.FunctionKey(fn))
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
			if disallowed := node.Effects &^ (effects.DBRead | effects.Agent); disallowed != effects.None {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q has %s but module has @containment(agent: \"read_only\")",
					fn.Name, disallowed,
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
					"function %q is agent-reachable but module has @containment(agent: \"approved_only\") — add approve fn or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}

// exprToEffectName converts an AST expression used in a @sandbox allow/deny list
// to an effect name string (e.g. "DB.read", "Net", "AI").
func exprToEffectName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Ident); ok && !e.Optional {
			return id.Name + "." + e.Member
		}
	}
	return ""
}

// parseEffectListArg converts a @sandbox allow/deny list argument to an EffectSet.
func parseEffectListArg(expr ast.Expr) (effects.EffectSet, []diagnostic.Diagnostic) {
	list, ok := expr.(*ast.ListLiteral)
	if !ok {
		return effects.None, []diagnostic.Diagnostic{diagnostic.Errorf(expr.Pos(), "@sandbox requires a list of effect labels")}
	}
	var set effects.EffectSet
	var diags []diagnostic.Diagnostic
	for _, elem := range list.Elements {
		name := exprToEffectName(elem)
		if name == "" {
			diags = append(diags, diagnostic.Errorf(elem.Pos(), "@sandbox requires an effect label"))
		} else if flag, known := effects.Lookup(name); known {
			set |= flag
		} else {
			diags = append(diags, diagnostic.Errorf(elem.Pos(), "unknown effect %q in @sandbox", name))
		}
	}
	return set, diags
}

// checkSandbox enforces @sandbox allow/deny constraints on annotated functions.
// For each @sandbox function, it checks that every transitively reachable function's
// effects satisfy the allow/deny lists. Agent is excluded from constraint checking
// since it is a structural meta-effect, not a runtime capability.
func (c *Checker) checkSandbox(file *ast.File, g *callgraph.Graph) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		var sandboxAnn *ast.Annotation
		for i := range fn.Annotations {
			if fn.Annotations[i].Kind == ast.AnnotSandbox {
				if sandboxAnn != nil {
					diags = append(diags, diagnostic.Errorf(fn.Annotations[i].Pos, "duplicate @sandbox annotation"))
					continue
				}
				sandboxAnn = &fn.Annotations[i]
			}
		}
		if sandboxAnn == nil {
			continue
		}

		var allowSet effects.EffectSet
		allowExpr, hasAllow := sandboxAnn.Args["allow"]
		if hasAllow {
			set, errors := parseEffectListArg(allowExpr)
			allowSet = set
			diags = append(diags, errors...)
		}
		var denySet effects.EffectSet
		if denyExpr, ok := sandboxAnn.Args["deny"]; ok {
			set, errors := parseEffectListArg(denyExpr)
			denySet = set
			diags = append(diags, errors...)
		}

		reachable := g.Reachable([]string{g.FunctionKey(fn)})
		for name := range reachable {
			node := g.Node(name)
			if node == nil {
				continue
			}
			// Exclude the Agent meta-effect; it is structural, not a capability.
			nodeEffects := node.Effects &^ effects.Agent
			if nodeEffects == effects.None {
				continue
			}

			if denySet != effects.None {
				if violated := nodeEffects & denySet; violated != effects.None {
					diags = append(diags, diagnostic.Errorf(
						fn.Position,
						"@sandbox on %q: reachable function %q uses denied effect %s",
						fn.Name, name, violated.String(),
					))
				}
			}
			if hasAllow {
				if disallowed := nodeEffects &^ allowSet; disallowed != effects.None {
					diags = append(diags, diagnostic.Errorf(
						fn.Position,
						"@sandbox on %q: reachable function %q uses effect %s not in allow list",
						fn.Name, name, disallowed.String(),
					))
				}
			}
		}
	}

	return diags
}

// checkBudget validates @budget annotation arguments.
// max_cost must be a Float or Int literal; max_calls must be an Int literal.
func (c *Checker) checkBudget(file *ast.File) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		for _, ann := range fn.Annotations {
			if ann.Kind != ast.AnnotBudget {
				continue
			}
			if maxCost, ok := ann.Args["max_cost"]; ok {
				switch maxCost.(type) {
				case *ast.FloatLiteral, *ast.IntLiteral:
					// valid
				default:
					diags = append(diags, diagnostic.Errorf(
						fn.Position,
						"@budget max_cost on %q must be a numeric literal (e.g. 1.0)",
						fn.Name,
					))
				}
			}
			if maxCalls, ok := ann.Args["max_calls"]; ok {
				if _, ok := maxCalls.(*ast.IntLiteral); !ok {
					diags = append(diags, diagnostic.Errorf(
						fn.Position,
						"@budget max_calls on %q must be an integer literal (e.g. 20)",
						fn.Name,
					))
				}
			}
		}
	}

	return diags
}

// checkApprove emits an error for every agent-reachable function that combines
// DBWrite and Net effects without approve fn or @agent_allowed.
func (c *Checker) checkApprove(file *ast.File, g *callgraph.Graph, agentReachable map[string]bool) []diagnostic.Diagnostic {
	var diags []diagnostic.Diagnostic

	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !agentReachable[g.FunctionKey(fn)] {
			continue
		}
		node := g.Node(g.FunctionKey(fn))
		if node == nil {
			continue
		}
		// Policy: DB.write + Net in agent context requires explicit approval.
		if node.Effects.Has(effects.DBWrite) && node.Effects.Has(effects.Net) {
			if !node.HasAnnotation(ast.AnnotApprove) && !node.HasAnnotation(ast.AnnotAgentAllowed) {
				diags = append(diags, diagnostic.Errorf(
					fn.Position,
					"function %q combines DB.write and Net effects in an agent context — add approve fn or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}
