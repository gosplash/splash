package compiler

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/token"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

var (
	ErrFunctionNotFound  = errors.New("function not found")
	ErrAmbiguousFunction = errors.New("ambiguous function")
)

// FunctionContract is a detached projection of the owned declaration and its
// effect summary from one analyzed source snapshot.
type FunctionContract struct {
	Symbol      string
	Declaration token.Position
	Signature   Signature
	Declared    effects.EffectSet
	Inferred    effects.EffectSet
	Gap         *effects.CoverageGap
}

type Signature struct {
	Parameters     []Parameter     `json:"parameters"`
	ReturnType     string          `json:"returnType"`
	TypeParameters []TypeParameter `json:"typeParameters,omitempty"`
	Async          bool            `json:"async,omitempty"`
}

type Parameter struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	HasDefault bool   `json:"hasDefault,omitempty"`
	Variadic   bool   `json:"variadic,omitempty"`
}

type TypeParameter struct {
	Name        string   `json:"name"`
	Constraints []string `json:"constraints,omitempty"`
}

// InspectFunction accepts a declared module-qualified name, or a bare
// function name in the entry file. Import aliases are local to their file.
func (p *AnalyzedProgram) InspectFunction(query string) (FunctionContract, error) {
	owner, declaration, err := p.resolveFunction(query)
	if err != nil {
		return FunctionContract{}, err
	}
	symbol := functionSymbol(owner.File.Module, declaration)
	for _, summary := range p.snapshot.effects {
		if summary.Function != declaration {
			continue
		}
		fnType := owner.GenericSignatures[declaration]
		if fnType == nil {
			fnType, _ = owner.Types[declaration].(*types.FunctionType)
		}
		if fnType == nil {
			return FunctionContract{}, fmt.Errorf("missing checked signature for %q", symbol)
		}
		contract := FunctionContract{
			Symbol: symbol, Declaration: declaration.Pos(), Signature: functionSignature(fnType, declaration),
			Declared: summary.Declared, Inferred: summary.Inferred,
		}
		if summary.Gap != nil {
			gap := *summary.Gap
			contract.Gap = &gap
		}
		return contract, nil
	}
	return FunctionContract{}, fmt.Errorf("missing effect summary for %q", symbol)
}

func (p *AnalyzedProgram) resolveFunction(query string) (*typechecker.TypedFile, *ast.FunctionDecl, error) {
	if p == nil || p.snapshot == nil {
		return nil, nil, ErrInvalidAnalysis
	}
	snapshot := p.snapshot
	qualifier, name, qualified := strings.Cut(query, ".")
	modules := snapshot.modules
	if !qualified {
		name = qualifier
		modules = modules[len(modules)-1:]
	}

	var owner *typechecker.TypedFile
	var declaration *ast.FunctionDecl
	for _, module := range modules {
		moduleName := ""
		if module.File.Module != nil {
			moduleName = module.File.Module.Name
		}
		if qualified && (moduleName == "" || moduleName != qualifier) {
			continue
		}
		for _, fact := range module.EffectFacts {
			if fact.Function.Name != name {
				continue
			}
			if declaration != nil {
				return nil, nil, fmt.Errorf("%w: %q", ErrAmbiguousFunction, query)
			}
			owner, declaration = module, fact.Function
		}
	}
	if declaration == nil {
		return nil, nil, fmt.Errorf("%w: %q", ErrFunctionNotFound, query)
	}
	return owner, declaration, nil
}

func functionSymbol(module *ast.ModuleDecl, declaration *ast.FunctionDecl) string {
	if module == nil {
		return declaration.Name
	}
	return module.Name + "." + declaration.Name
}

func functionSignature(fnType *types.FunctionType, declaration *ast.FunctionDecl) Signature {
	signature := Signature{
		Parameters: make([]Parameter, 0, len(declaration.Params)),
		ReturnType: fnType.Return.TypeName(), Async: declaration.IsAsync,
	}
	for i, param := range declaration.Params {
		signature.Parameters = append(signature.Parameters, Parameter{
			Name: param.Name, Type: fnType.Params[i].TypeName(),
			HasDefault: param.Default != nil, Variadic: param.Variadic,
		})
	}
	for _, param := range declaration.TypeParams {
		signature.TypeParameters = append(signature.TypeParameters, TypeParameter{
			Name: param.Name, Constraints: slices.Clone(param.Constraints),
		})
	}
	return signature
}
