// Package safety runs enforcement passes over the call graph: redline fn,
// approve fn, and @containment.
package safety

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/token"
	"gosplash.dev/splash/internal/types"
)

// Checker runs safety enforcement passes over typed modules and their call graph.
type Checker struct{}

// New creates a new Checker.
func New() *Checker { return &Checker{} }

// Module carries owned syntax and types resolved by shared analysis.
type Module struct {
	File  *ast.File
	Types map[ast.Node]types.Type
}

var (
	ErrRejected      = errors.New("safety errors (rejected)")
	ErrIndeterminate = errors.New("safety verification indeterminate")
)

// Result contains policy findings and coverage of the analyzed effects.
type Result struct {
	Violations      []diagnostic.Diagnostic
	Incomplete      []diagnostic.Diagnostic
	EffectsComplete bool
}

// Check runs the enforcement passes and reports findings even if coverage is incomplete.
func (c *Checker) Check(modules []Module, g *callgraph.Graph, inferred []*effects.Summary) Result {
	var diags []diagnostic.Diagnostic

	roots := g.AgentRoots()
	agentReachable := g.Reachable(roots)
	agentPaths := g.ShortestPaths(roots)
	var agentCallees map[string]bool
	if slices.ContainsFunc(modules, func(module Module) bool {
		return moduleContainment(module.File) == containmentApprovedOnly
	}) {
		agentCallees = make(map[string]bool)
		for caller := range agentReachable {
			if node := g.Node(caller); node != nil {
				for _, callee := range node.Callees() {
					agentCallees[callee] = true
				}
			}
		}
	}

	gated := g.ApprovalFunctions()
	diags = append(diags, c.checkApprovalCallContexts(g, gated)...)
	for _, module := range modules {
		file := module.File
		diags = append(diags, c.checkRedline(file, g, agentPaths)...)
		diags = append(diags, c.checkApprove(file, g, agentReachable)...)
		diags = append(diags, c.checkContainment(file, g, agentReachable, agentCallees)...)
		diags = append(diags, c.checkSandbox(file, g)...)
		diags = append(diags, c.checkBudget(file)...)
	}
	toolViolations, toolGaps := c.checkToolDataClassification(modules)
	diags = append(diags, toolViolations...)
	effectsComplete := true
	for _, summary := range inferred {
		diags = append(diags, summary.Diagnostics()...)
		effectsComplete = effectsComplete && summary.Gap == nil
	}

	return Result{
		Violations:      diags,
		Incomplete:      append(requiredCoverage(g, inferred, len(gated) != 0), toolGaps...),
		EffectsComplete: effectsComplete,
	}
}

func policyErrorf(ruleID string, pos token.Position, format string, args ...any) diagnostic.Diagnostic {
	diag := diagnostic.Errorf(pos, format, args...).WithCode(diagnostic.PolicyViolation)
	diag.PolicyRuleID = ruleID
	return diag
}

// requiredCoverage reports direct coverage gaps in the resolved closure of policy roots.
// Approval error propagation applies to all source functions when any uses approval.
// The compiler-injected Go adapter is a trusted host boundary for this source check.
func requiredCoverage(g *callgraph.Graph, summaries []*effects.Summary, hasApproval bool) []diagnostic.Diagnostic {
	hasGaps := slices.ContainsFunc(summaries, func(summary *effects.Summary) bool {
		return len(summary.DirectGaps) != 0
	})
	if !hasGaps {
		return nil
	}

	var reachable map[string]bool
	if !hasApproval {
		roots := g.AgentRoots()
		for _, summary := range summaries {
			key := g.FunctionKey(summary.Function)
			if node := g.Node(key); summary.Declared != effects.None || (node != nil && node.HasAnnotation(ast.AnnotSandbox)) {
				roots = append(roots, key)
			}
		}
		reachable = g.Reachable(roots)
	}

	var diags []diagnostic.Diagnostic
	seen := make(map[effects.CoverageGap]bool)
	for _, summary := range summaries {
		if !hasApproval && !reachable[g.FunctionKey(summary.Function)] {
			continue
		}
		for _, gap := range summary.DirectGaps {
			if !gap.ApprovalBoundary && !seen[gap] {
				diag := policyErrorf("coverage.required_source", gap.Position, "incomplete policy coverage: %s", gap.Reason).
					WithCode(diagnostic.PolicyCoverageIncomplete)
				diag.End = gap.End
				diags = append(diags, diag)
				seen[gap] = true
			}
		}
	}
	return diags
}

// checkApprovalCallContexts rejects uses of an approval error channel that the
// current backend can only propagate for a whole statement, let or return value.
func (c *Checker) checkApprovalCallContexts(g *callgraph.Graph, gated map[string]bool) []diagnostic.Diagnostic {
	if len(gated) == 0 {
		return nil
	}
	withErrorChannel := g.Callers(gated)
	var diags []diagnostic.Diagnostic
	for _, node := range g.Nodes() {
		for _, call := range node.NestedCalls {
			if withErrorChannel[call.Target] {
				diag := policyErrorf("approval.call_context", call.Position,
					"approval call to %q must be a standalone statement, let value, or return value", call.Target)
				diag.End = call.End
				diags = append(diags, diag)
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

// checkToolDataClassification follows data-bearing return types using resolved
// declaration identities and the owning modules' type evidence.
func (c *Checker) checkToolDataClassification(modules []Module) (violations, incomplete []diagnostic.Diagnostic) {
	var owners map[*ast.TypeDecl]map[ast.Node]types.Type
	for _, module := range modules {
		for _, declaration := range module.File.Declarations {
			fn, ok := declaration.(*ast.FunctionDecl)
			if !ok || fn.ReturnType == nil || !slices.ContainsFunc(fn.Annotations, func(ann ast.Annotation) bool {
				return ann.Kind == ast.AnnotTool
			}) {
				continue
			}
			if owners == nil {
				owners = recordTypeOwners(modules)
			}
			var returnType types.Type
			if signature, ok := module.Types[fn].(*types.FunctionType); ok && signature != nil {
				returnType = signature.Return
			}
			found, gap := checkToolReturnType(fn, returnType, owners)
			violations = append(violations, found...)
			if gap != (token.Position{}) {
				incomplete = append(incomplete, policyErrorf("tool.return_coverage", gap,
					"incomplete policy coverage: tool fn %q return classification lacks supported resolved type evidence", fn.Name,
				).WithCode(diagnostic.PolicyCoverageIncomplete))
			}
		}
	}
	return violations, incomplete
}

func recordTypeOwners(modules []Module) map[*ast.TypeDecl]map[ast.Node]types.Type {
	owners := make(map[*ast.TypeDecl]map[ast.Node]types.Type)
	for _, module := range modules {
		for _, declaration := range module.File.Declarations {
			if record, ok := declaration.(*ast.TypeDecl); ok {
				owners[record] = module.Types
			}
		}
	}
	return owners
}

// checkToolReturnType reports known violations even when part of the return
// type is unsupported. Each resolved record is visited once, including cycles.
func checkToolReturnType(fn *ast.FunctionDecl, returnType types.Type, owners map[*ast.TypeDecl]map[ast.Node]types.Type) (violations []diagnostic.Diagnostic, gap token.Position) {
	seen := make(map[*ast.TypeDecl]bool)
	markGap := func(pos token.Position) {
		if gap == (token.Position{}) {
			gap = pos
		}
	}
	var visit func(types.Type, token.Position)
	visit = func(typ types.Type, pos token.Position) {
		switch typ := typ.(type) {
		case *types.OptionalType:
			visit(typ.Inner, pos)
		case *types.ListType:
			visit(typ.Element, pos)
		case *types.ResultType:
			visit(typ.Ok, pos)
			visit(typ.Err, pos)
		case *types.NamedType:
			record := typ.Declaration
			if record == nil {
				markGap(pos)
				return
			}
			if seen[record] {
				return
			}
			seen[record] = true
			if fields := sensitiveFields(record); len(fields) > 0 {
				violations = append(violations, policyErrorf("tool.classified_return", fn.Position,
					"tool fn %q returns type %s which contains sensitive or restricted fields (%s), declared at %s",
					fn.Name, record.Name, strings.Join(fields, ", "), record.Pos(),
				))
			}
			fieldTypes := owners[record]
			if fieldTypes == nil || len(record.TypeParams) > 0 {
				markGap(pos)
			}
			for _, field := range record.Fields {
				fieldType := fieldTypes[field.Type]
				if !types.RetainsTypeArguments(field.Type, fieldType) {
					markGap(field.Position)
				}
				visit(fieldType, field.Position)
			}
		case *types.EnumType:
			if typ.Declaration == nil || slices.ContainsFunc(typ.Declaration.Variants, func(variant ast.EnumVariant) bool {
				return variant.Payload != nil
			}) {
				markGap(pos)
			}
		case *types.PrimitiveType:
			if typ == types.Unknown {
				markGap(pos)
			}
		default:
			markGap(pos)
		}
	}
	if !types.RetainsTypeArguments(fn.ReturnType, returnType) {
		markGap(fn.ReturnType.Pos())
	}
	visit(returnType, fn.ReturnType.Pos())
	return violations, gap
}

// checkRedline emits an error for every redline function that is agent-reachable.
// The message shows the agent root and the portion of the path nearest the violation.
func (c *Checker) checkRedline(file *ast.File, g *callgraph.Graph, agentPaths callgraph.PathTree) []diagnostic.Diagnostic {
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
		path, length := agentPaths.PathExcerpt(node.Name, diagnostic.WitnessLimit)
		if length == 0 {
			continue
		}
		truncated := length > len(path)
		if truncated {
			gap := fmt.Sprintf("… (omitted %d of %d functions) …", length-len(path), length)
			path = slices.Insert(path, 1, gap)
		}
		diag := policyErrorf("redline.agent_reachable", fn.Position,
			"redline fn %q is reachable from an agent context\n  call path: %s",
			fn.Name, strings.Join(path, " → "))
		diag.WitnessLength = length
		diag.WitnessTruncated = truncated
		diags = append(diags, diag)
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
func (c *Checker) checkContainment(file *ast.File, g *callgraph.Graph, agentReachable, agentCallees map[string]bool) []diagnostic.Diagnostic {
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
		key := g.FunctionKey(fn)
		if !agentReachable[key] {
			continue
		}
		node := g.Node(key)
		if node == nil {
			continue
		}

		switch policy {
		case containmentNone:
			diags = append(diags, policyErrorf("containment.none",
				fn.Position,
				"function %q is agent-reachable but module has @containment(agent: \"none\")",
				fn.Name,
			))
		case containmentReadOnly:
			if disallowed := node.Effects &^ (effects.DBRead | effects.Agent); disallowed != effects.None {
				diags = append(diags, policyErrorf("containment.read_only",
					fn.Position,
					"function %q has %s but module has @containment(agent: \"read_only\")",
					fn.Name, disallowed,
				))
			}
		case containmentApprovedOnly:
			// Agent entry-point admission is independent of permission to call it.
			if node.Effects.Has(effects.Agent) && !agentCallees[key] {
				continue
			}
			if !node.HasAnnotation(ast.AnnotApprove) && !node.HasAnnotation(ast.AnnotAgentAllowed) {
				diags = append(diags, policyErrorf("containment.approved_only",
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
		return effects.None, []diagnostic.Diagnostic{policyErrorf("sandbox.invalid_effect_list", expr.Pos(), "@sandbox requires a list of effect labels")}
	}
	var set effects.EffectSet
	var diags []diagnostic.Diagnostic
	for _, elem := range list.Elements {
		name := exprToEffectName(elem)
		if name == "" {
			diags = append(diags, policyErrorf("sandbox.invalid_effect_list", elem.Pos(), "@sandbox requires an effect label"))
		} else if flag, known := effects.Lookup(name); known {
			set |= flag
		} else {
			diags = append(diags, policyErrorf("sandbox.invalid_effect_list", elem.Pos(), "unknown effect %q in @sandbox", name))
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
					diags = append(diags, policyErrorf("sandbox.duplicate", fn.Annotations[i].Pos, "duplicate @sandbox annotation"))
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

		firstFinding := len(diags)
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
					diags = append(diags, policyErrorf("sandbox.denied_effect",
						fn.Position,
						"@sandbox on %q: reachable function %q uses denied effect %s",
						fn.Name, name, violated.String(),
					))
				}
			}
			if hasAllow {
				if disallowed := nodeEffects &^ allowSet; disallowed != effects.None {
					diags = append(diags, policyErrorf("sandbox.effect_not_allowed",
						fn.Position,
						"@sandbox on %q: reachable function %q uses effect %s not in allow list",
						fn.Name, name, disallowed.String(),
					))
				}
			}
		}
		slices.SortFunc(diags[firstFinding:], func(a, b diagnostic.Diagnostic) int {
			return cmp.Compare(a.Message, b.Message)
		})
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
					diags = append(diags, policyErrorf("budget.max_cost_literal",
						fn.Position,
						"@budget max_cost on %q must be a numeric literal (e.g. 1.0)",
						fn.Name,
					))
				}
			}
			if maxCalls, ok := ann.Args["max_calls"]; ok {
				if _, ok := maxCalls.(*ast.IntLiteral); !ok {
					diags = append(diags, policyErrorf("budget.max_calls_literal",
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
				diags = append(diags, policyErrorf("approval.required",
					fn.Position,
					"function %q combines DB.write and Net effects in an agent context — add approve fn or @agent_allowed",
					fn.Name,
				))
			}
		}
	}

	return diags
}
