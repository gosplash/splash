package compiler

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/typechecker"
)

type selectorKey struct {
	module string
	name   string
}

type selectedFunction struct {
	owner   *typechecker.TypedFile
	summary *effects.Summary
}

type selectorIndex struct {
	entryModule string
	entry       map[string]selectedFunction
	imported    map[selectorKey]selectedFunction
}

func (p *analysis) indexSelectors() *selectorIndex {
	p.selectorOnce.Do(func() {
		entry := p.modules[len(p.modules)-1]
		index := &selectorIndex{
			entry:    make(map[string]selectedFunction, len(entry.EffectFacts)),
			imported: make(map[selectorKey]selectedFunction, len(p.effects)-len(entry.EffectFacts)),
		}
		if entry.File.Module != nil {
			index.entryModule = entry.File.Module.Name
		}
		// Infer returns one summary per fact in input order. Analyze passes
		// the facts in this same module and declaration order.
		next := 0
		for _, owner := range p.modules {
			module := owner.File.Module
			for _, fact := range owner.EffectFacts {
				selected := selectedFunction{owner, p.effects[next]}
				next++
				name := fact.Function.Name
				switch {
				case owner == entry:
					index.entry[name] = selected
				case module != nil && module.Name != "":
					key := selectorKey{module.Name, name}
					if _, exists := index.imported[key]; exists {
						selected = selectedFunction{} // A present zero value denotes ambiguity.
					}
					index.imported[key] = selected
				}
			}
		}
		p.selectors = index
	})
	return p.selectors
}

func (p *AnalyzedProgram) resolveFunction(query string) (selectedFunction, error) {
	if p == nil || p.snapshot == nil {
		return selectedFunction{}, ErrInvalidAnalysis
	}
	index := p.snapshot.indexSelectors()
	module, name, qualified := strings.Cut(query, ".")
	if !qualified {
		if selected, exists := index.entry[module]; exists {
			return selected, nil
		}
	} else if module != "" {
		imported, exists := index.imported[selectorKey{module, name}]
		if module == index.entryModule {
			if selected, found := index.entry[name]; found {
				if exists {
					return selectedFunction{}, fmt.Errorf("%w: %q", ErrAmbiguousFunction, query)
				}
				return selected, nil
			}
		}
		if exists {
			if imported.summary == nil {
				return selectedFunction{}, fmt.Errorf("%w: %q", ErrAmbiguousFunction, query)
			}
			return imported, nil
		}
	}
	return selectedFunction{}, fmt.Errorf("%w: %q", ErrFunctionNotFound, query)
}
