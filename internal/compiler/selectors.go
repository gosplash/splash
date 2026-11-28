package compiler

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
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
	err         error
	entryModule string
	entry       map[string]selectedFunction
	imported    map[selectorKey]selectedFunction
}

func (p *analysis) indexSelectors() *selectorIndex {
	p.selectorOnce.Do(func() {
		entry := p.modules[len(p.modules)-1]
		index := &selectorIndex{
			entry:    make(map[string]selectedFunction, len(entry.EffectFacts)),
			imported: make(map[selectorKey]selectedFunction, max(0, len(p.effects)-len(entry.EffectFacts))),
		}
		p.selectors = index
		if entry.File.Module != nil {
			index.entryModule = entry.File.Module.Name
		}
		summaries := make(map[*ast.FunctionDecl]*effects.Summary, len(p.effects))
		for _, summary := range p.effects {
			if summary == nil || summary.Function == nil {
				index.err = fmt.Errorf("invalid effect summary for function selectors")
				return
			}
			if _, exists := summaries[summary.Function]; exists {
				index.err = fmt.Errorf("duplicate effect summary for %q", summary.Function.Name)
				return
			}
			summaries[summary.Function] = summary
		}
		for _, owner := range p.modules {
			module := owner.File.Module
			for _, fact := range owner.EffectFacts {
				name := fact.Function.Name
				summary := summaries[fact.Function]
				if summary == nil {
					index.err = fmt.Errorf("missing effect summary for %q", functionSymbol(module, fact.Function))
					return
				}
				delete(summaries, fact.Function)
				selected := selectedFunction{owner, summary}
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
		if len(summaries) != 0 {
			index.err = fmt.Errorf("effect summary has no analyzed declaration")
		}
	})
	return p.selectors
}

func (p *AnalyzedProgram) resolveFunction(query string) (selectedFunction, error) {
	if p == nil || p.snapshot == nil {
		return selectedFunction{}, ErrInvalidAnalysis
	}
	index := p.snapshot.indexSelectors()
	if index.err != nil {
		return selectedFunction{}, index.err
	}
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
