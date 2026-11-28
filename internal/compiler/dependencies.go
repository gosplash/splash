package compiler

import (
	"errors"
	"fmt"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/token"
)

type DependencyDirection string

const (
	Callers DependencyDirection = "callers"
	Callees DependencyDirection = "callees"
)

type DependencyFunction struct {
	Symbol      string
	Declaration token.Position
}

// Relationship records one step of a shortest path toward the selected root
// in the query traversal. Position and End describe the actual call site.
type Relationship struct {
	Function DependencyFunction
	Via      DependencyFunction
	Distance int
	Position token.Position
	End      token.Position
}

type DependencyGap struct {
	Function DependencyFunction
	Reason   string
	Position token.Position
	End      token.Position
}

type DependencyResult struct {
	Root          DependencyFunction
	Relationships []Relationship
	Gap           *DependencyGap
	total         int
}

type CompactDependencyFunction struct {
	Symbol string `json:"symbol"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}

type CompactDependencyMatch struct {
	CompactDependencyFunction
	Distance int `json:"distance"`
}

type CompactDependencyResult struct {
	Root    CompactDependencyFunction
	Matches []CompactDependencyMatch
	Total   int
	Gap     *DependencyGap
}

type dependencyIndex struct {
	nodes    map[*ast.FunctionDecl]*dependencyNode
	firstGap *dependencyNode
}

type dependencyNode struct {
	declaration *ast.FunctionDecl
	module      *ast.ModuleDecl
	out, in     []dependencyEdge
	gap         *effects.CoverageGap
}

type dependencyEdge struct {
	node *dependencyNode
	call *effects.ResolvedCall
}

func (n *dependencyNode) function() DependencyFunction {
	return DependencyFunction{
		Symbol: functionSymbol(n.module, n.declaration), Declaration: n.declaration.Pos(),
	}
}

func (p *analysis) indexDependencies() *dependencyIndex {
	p.dependencyOnce.Do(func() {
		index := &dependencyIndex{nodes: make(map[*ast.FunctionDecl]*dependencyNode, len(p.effects))}
		nodes := make([]dependencyNode, len(p.effects))
		nextNode, callCount := 0, 0
		for _, module := range p.modules {
			for _, fact := range module.EffectFacts {
				node := &nodes[nextNode]
				*node = dependencyNode{declaration: fact.Function, module: module.File.Module}
				index.nodes[fact.Function] = node
				nextNode++
				callCount += len(fact.Calls)
			}
		}
		// Summaries retain direct source gaps from the same call evidence used
		// for effect conformance. Injected approval hosts are outside the
		// relation between analyzed Splash declarations.
		for _, summary := range p.effects {
			node := index.nodes[summary.Function]
			for i := range summary.DirectGaps {
				if gap := &summary.DirectGaps[i]; !gap.ApprovalBoundary {
					node.gap = gap
					if index.firstGap == nil {
						index.firstGap = node
					}
					break
				}
			}
		}
		outgoing := make([]dependencyEdge, callCount)
		for _, module := range p.modules {
			for _, fact := range module.EffectFacts {
				caller := index.nodes[fact.Function]
				caller.out = outgoing[:0:len(fact.Calls)]
				outgoing = outgoing[len(fact.Calls):]
				for i := range fact.Calls {
					call := &fact.Calls[i]
					if callee := index.nodes[call.Target]; callee != nil {
						caller.out = append(caller.out, dependencyEdge{callee, call})
						callee.in = append(callee.in, dependencyEdge{caller, call})
					}
				}
			}
		}
		p.dependencies = index
	})
	return p.dependencies
}

// Dependencies returns modeled source call relationships. Max depth 0 means
// unbounded. A callers query conservatively reports gaps in the entry closure
// because even a function outside the known reverse graph could call the root.
func (p *AnalyzedProgram) Dependencies(query string, direction DependencyDirection, maxDepth int) (DependencyResult, error) {
	return p.dependenciesWithin(query, direction, maxDepth, 0)
}

// CompactDependencies returns the first limit matches in traversal order.
// Counting and coverage still describe the entire requested depth and scope.
func (p *AnalyzedProgram) CompactDependencies(query string, direction DependencyDirection, maxDepth, limit int) (CompactDependencyResult, error) {
	if limit < 1 || limit > MaxContextLimit {
		return CompactDependencyResult{}, fmt.Errorf("dependency limit must be between 1 and %d", MaxContextLimit)
	}
	result, err := p.dependenciesWithin(query, direction, maxDepth, limit)
	if err != nil {
		return CompactDependencyResult{}, err
	}
	location := func(fn DependencyFunction) CompactDependencyFunction {
		return CompactDependencyFunction{
			Symbol: fn.Symbol,
			Path:   p.snapshot.sourcePath(fn.Declaration.File),
			Line:   fn.Declaration.Line,
		}
	}
	compact := CompactDependencyResult{
		Root: location(result.Root), Matches: make([]CompactDependencyMatch, 0, len(result.Relationships)),
		Total: result.total, Gap: result.Gap,
	}
	for _, match := range result.Relationships {
		compact.Matches = append(compact.Matches, CompactDependencyMatch{
			CompactDependencyFunction: location(match.Function), Distance: match.Distance,
		})
	}
	return compact, nil
}

func (p *AnalyzedProgram) dependenciesWithin(query string, direction DependencyDirection, maxDepth, limit int) (DependencyResult, error) {
	if (direction != Callers && direction != Callees) || maxDepth < 0 {
		return DependencyResult{}, errors.New("invalid dependency query")
	}
	selected, err := p.resolveFunction(query)
	if err != nil {
		return DependencyResult{}, err
	}
	index := p.snapshot.indexDependencies()
	root := index.nodes[selected.summary.Function]
	result := DependencyResult{Root: root.function(), Relationships: []Relationship{}}

	var uncovered *dependencyNode
	if direction == Callers {
		uncovered = index.firstGap
	}
	type visit struct {
		node     *dependencyNode
		distance int
	}
	queue := []visit{{root, 0}}
	seen := make(map[*dependencyNode]bool)
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if maxDepth != 0 && current.distance == maxDepth {
			break
		}
		edges := current.node.out
		if direction == Callers {
			edges = current.node.in
		} else if uncovered == nil && current.node.gap != nil {
			uncovered = current.node
		}
		for _, edge := range edges {
			if seen[edge.node] {
				continue
			}
			seen[edge.node] = true
			distance := current.distance + 1
			result.total++
			if limit == 0 || len(result.Relationships) < limit {
				result.Relationships = append(result.Relationships, Relationship{
					Function: edge.node.function(), Via: current.node.function(), Distance: distance,
					Position: edge.call.Position, End: edge.call.End,
				})
			}
			if edge.node != root {
				queue = append(queue, visit{edge.node, distance})
			}
		}
	}
	if uncovered != nil {
		gap := uncovered.gap
		result.Gap = &DependencyGap{
			Function: uncovered.function(), Reason: gap.Reason, Position: gap.Position, End: gap.End,
		}
	}
	return result, nil
}
