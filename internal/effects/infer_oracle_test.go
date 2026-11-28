package effects

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/token"
)

func TestInferMatchesForwardOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(47003))
	const graphs = 400
	for sample := 0; sample < graphs; sample++ {
		n := 2 + rng.Intn(24)
		facts := make([]FunctionFacts, n)
		index := make(map[*ast.FunctionDecl]int, n)
		for i := range facts {
			facts[i].Function = &ast.FunctionDecl{Name: fmt.Sprintf("f%d", i)}
			index[facts[i].Function] = i
		}
		missing := &ast.FunctionDecl{Name: "unmodeled"}
		for i := range facts {
			f := &facts[i]
			if rng.Intn(4) == 0 {
				f.Gaps = []CoverageGap{{Reason: "synthetic local gap"}}
			}
			for k, flag := range []EffectSet{AI, DBRead, Net} {
				if rng.Intn(6) == 0 {
					pos := token.Position{File: "graph", Line: i + 1, Column: k + 1}
					f.Operations = append(f.Operations, Operation{Effects: flag, Name: "modeled", Position: pos})
				}
			}
			for j := range facts {
				if rng.Intn(7) == 0 {
					f.Calls = append(f.Calls, ResolvedCall{Target: facts[j].Function,
						Position: token.Position{File: "graph", Line: i + 1, Column: j + 1}})
				}
			}
			if rng.Intn(8) == 0 {
				f.Calls = append(f.Calls, ResolvedCall{Target: missing})
			}
		}
		got := Infer(facts)
		for i, subject := range got {
			var known EffectSet
			var gap bool
			// Walk from each subject independently, collecting operations
			// and gaps over the forward closure.
			seen := make(map[int]bool)
			pending := []int{i}
			for len(pending) != 0 {
				j := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				if seen[j] {
					continue
				}
				seen[j] = true
				f := facts[j]
				gap = gap || len(f.Gaps) > 0
				for _, op := range f.Operations {
					known |= op.Effects
				}
				for _, call := range f.Calls {
					if target, ok := index[call.Target]; ok {
						pending = append(pending, target)
					} else {
						gap = true
					}
				}
			}
			if subject.Inferred != known || (subject.Gap != nil) != gap {
				t.Fatalf("graph %d subject %d: got %s,%t want %s,%t", sample, i, subject.Inferred, subject.Gap != nil, known, gap)
			}
			for _, flag := range []EffectSet{AI, DBRead, Net} {
				if !known.Has(flag) {
					continue
				}
				current := subject
				for hop := 0; ; hop++ {
					step, ok := current.witness[flag]
					if !ok || hop > n {
						t.Fatalf("graph %d subject %d: invalid witness termination", sample, i)
					}
					f := facts[index[current.Function]]
					if step.next == nil {
						if !slices.Contains(f.Operations, step.origin) || !step.origin.Effects.Has(flag) {
							t.Fatalf("graph %d subject %d: invalid origin", sample, i)
						}
						break
					}
					if !slices.ContainsFunc(f.Calls, func(c ResolvedCall) bool {
						return c.Target == step.next.Function && c.Position == step.at
					}) {
						t.Fatalf("graph %d subject %d: invalid witness edge", sample, i)
					}
					current = step.next
				}
			}
		}
	}
	t.Logf("matched %d generated graphs against the forward oracle", graphs)
}
