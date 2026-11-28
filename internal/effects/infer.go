package effects

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/token"
)

// FunctionFacts contains implementation evidence from one checked declaration.
// Calls, operations, and local coverage gaps are in source traversal order.
type FunctionFacts struct {
	Function   *ast.FunctionDecl
	Calls      []ResolvedCall
	Operations []Operation
	Gaps       []CoverageGap
}

type ResolvedCall struct {
	Target   *ast.FunctionDecl
	Position token.Position
	End      token.Position
}

type Operation struct {
	Effects  EffectSet
	Name     string
	Position token.Position
	End      token.Position
}

type CoverageGap struct {
	Reason   string
	Position token.Position
	End      token.Position
	// ApprovalBoundary identifies the compiler-injected Go host boundary.
	// Source verification accounts for approval call edges independently.
	ApprovalBoundary bool
}

// Summary distinguishes the known lower bound of the conservative effect
// summary from the coverage needed to use it as an upper bound.
type Summary struct {
	Function           *ast.FunctionDecl
	Declared           EffectSet
	Inferred           EffectSet
	Gap                *CoverageGap // first representative transitive gap for inspection
	DirectGaps         []CoverageGap
	contractViolations []contractViolation
	requiredByCalls    EffectSet
	witness            map[EffectSet]effectStep
}

type contractViolation struct {
	call    ResolvedCall
	missing EffectSet
}

type effectStep struct {
	next   *Summary
	origin Operation
	length int
	at     token.Position
	end    token.Position
}

// Infer propagates known effects and coverage gaps in separate worklists. A
// function/effect pair enters the worklist once, including within recursive
// groups. Input and call order break ties between equally short witnesses.
func Infer(facts []FunctionFacts) []*Summary {
	summaries := make([]*Summary, 0, len(facts))
	byFunction := make(map[*ast.FunctionDecl]*Summary, len(facts))
	for _, fact := range facts {
		summary := &Summary{Function: fact.Function, Declared: Parse(fact.Function.Effects), DirectGaps: fact.Gaps}
		if len(summary.DirectGaps) != 0 {
			summary.Gap = &summary.DirectGaps[0]
		}
		summaries = append(summaries, summary)
		byFunction[fact.Function] = summary
	}

	type callerEdge struct {
		caller *Summary
		at     token.Position
		end    token.Position
	}
	callers := make(map[*Summary][]callerEdge)
	type knownEffect struct {
		summary *Summary
		effect  EffectSet
	}
	var pending []knownEffect
	var incomplete []*Summary
	for i, fact := range facts {
		summary := summaries[i]
		for _, call := range fact.Calls {
			if target := byFunction[call.Target]; target != nil {
				summary.requiredByCalls |= target.Declared
				callers[target] = append(callers[target], callerEdge{summary, call.Position, call.End})
				if missing := target.Declared &^ (summary.Declared | Agent); missing != None {
					summary.contractViolations = append(summary.contractViolations, contractViolation{call, missing})
				}
			} else {
				summary.DirectGaps = append(summary.DirectGaps, CoverageGap{Reason: "resolved target has no effect summary", Position: call.Position, End: call.End})
				if summary.Gap == nil || summary.Gap.ApprovalBoundary {
					summary.Gap = &summary.DirectGaps[len(summary.DirectGaps)-1]
				}
			}
		}
		if summary.Gap != nil {
			incomplete = append(incomplete, summary)
		}
		for _, operation := range fact.Operations {
			for _, effect := range atomicEffects {
				if operation.Effects.Has(effect.flag) && !summary.Inferred.Has(effect.flag) {
					summary.add(effect.flag, effectStep{origin: operation, length: 1, at: operation.Position, end: operation.End})
					pending = append(pending, knownEffect{summary, effect.flag})
				}
			}
		}
	}
	for head := 0; head < len(pending); head++ {
		current := pending[head]
		downstream := current.summary.witness[current.effect]
		for _, edge := range callers[current.summary] {
			if !edge.caller.Inferred.Has(current.effect) {
				edge.caller.add(current.effect, effectStep{
					next: current.summary, origin: downstream.origin, length: downstream.length + 1,
					at: edge.at, end: edge.end,
				})
				pending = append(pending, knownEffect{edge.caller, current.effect})
			}
		}
	}
	for head := 0; head < len(incomplete); head++ {
		current := incomplete[head]
		for _, edge := range callers[current] {
			// A source gap must remain visible even when a caller also
			// inherits an unmodeled approval host boundary.
			previous := edge.caller.Gap
			if previous == nil || (previous.ApprovalBoundary && !current.Gap.ApprovalBoundary) {
				edge.caller.Gap = current.Gap
				incomplete = append(incomplete, edge.caller)
			}
		}
	}
	return summaries
}

// UnusedGrants returns candidates for review only when coverage is complete.
// Resolved callees can still require a grant their implementation leaves unused.
func (s *Summary) UnusedGrants() EffectSet {
	if s.Gap != nil {
		return None
	}
	return s.Declared &^ (s.Inferred | s.requiredByCalls | Agent)
}

func (s *Summary) add(effect EffectSet, step effectStep) {
	if s.witness == nil {
		s.witness = make(map[EffectSet]effectStep)
	}
	s.Inferred |= effect
	s.witness[effect] = step
}

// Diagnostics rejects established violations even when other effects remain
// unknown. Each witness shows the start of a shortest resolved path and its
// modeled origin, with an explicit count of any omitted steps.
func (s *Summary) Diagnostics() []diagnostic.Diagnostic {
	missing := s.Inferred &^ s.Declared
	var diags []diagnostic.Diagnostic
	for _, violation := range s.contractViolations {
		call := violation.call
		diag := diagnostic.Errorf(call.Position,
			"missing declared effect %s for call to %q (declared at %s)",
			violation.missing, call.Target.Name, call.Target.Pos()).WithCode(diagnostic.DeclaredEffectMissing)
		diag.End = call.End
		diag.Effects = violation.missing.Labels()
		diag.WitnessLength = 1
		diag.Witness = []diagnostic.WitnessStep{{
			Target: call.Target.Name, Site: call.Position, End: call.End, Declaration: call.Target.Pos(),
		}}
		diags = append(diags, diag)
	}
	for _, effect := range atomicEffects {
		if !missing.Has(effect.flag) {
			continue
		}
		step := s.witness[effect.flag]
		origin := step.origin
		length := step.length
		shown := min(length, diagnostic.WitnessLimit)
		chain := make([]string, 1, shown+2)
		chain[0] = s.Function.Name
		witness := make([]diagnostic.WitnessStep, 0, shown)
		start, end := step.at, step.end
		for range shown - 1 {
			target := step.next
			chain = append(chain, target.Function.Name)
			witness = append(witness, diagnostic.WitnessStep{
				Target: target.Function.Name, Site: step.at, End: step.end, Declaration: target.Function.Pos(),
			})
			step = target.witness[effect.flag]
		}
		if length > shown {
			chain = append(chain, fmt.Sprintf("… (omitted %d of %d steps) …", length-shown, length))
		}
		chain = append(chain, origin.Name)
		witness = append(witness, diagnostic.WitnessStep{Target: origin.Name, Site: origin.Position, End: origin.End})
		diag := diagnostic.Errorf(start,
			"inferred effect %s exceeds %q's declared authority: %s (origin at %s)",
			effect.flag, s.Function.Name, strings.Join(chain, " -> "), origin.Position).
			WithCode(diagnostic.InferredEffectExceedsAuthority)
		diag.End = end
		diag.Effects = []string{effect.name}
		diag.Witness = witness
		diag.WitnessLength = length
		diag.WitnessTruncated = length > shown
		diags = append(diags, diag)
	}
	return diags
}
