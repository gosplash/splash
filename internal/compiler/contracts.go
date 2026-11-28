package compiler

import (
	"errors"
	"fmt"
	"slices"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/token"
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
	Parameters         []Parameter     `json:"parameters"`
	ReturnType         string          `json:"returnType"`
	ReturnTypeIdentity TypeIdentity    `json:"returnTypeIdentity,omitzero"`
	TypeParameters     []TypeParameter `json:"typeParameters,omitempty"`
	Async              bool            `json:"async,omitempty"`
}

type Parameter struct {
	Name         string       `json:"name"`
	Type         string       `json:"type"`
	TypeIdentity TypeIdentity `json:"typeIdentity,omitzero"`
	HasDefault   bool         `json:"hasDefault,omitempty"`
	Variadic     bool         `json:"variadic,omitempty"`
}

type TypeParameter struct {
	Name        string   `json:"name"`
	Constraints []string `json:"constraints,omitempty"`
}

// InspectFunction accepts a declared module-qualified name, or a bare
// function name in the entry file. Import aliases are local to their file.
func (p *AnalyzedProgram) InspectFunction(query string) (FunctionContract, error) {
	selected, err := p.resolveFunction(query)
	if err != nil {
		return FunctionContract{}, err
	}
	return p.snapshot.projectFunctionContract(selected)
}

func (a *analysis) projectFunctionContract(selected selectedFunction) (FunctionContract, error) {
	owner, summary := selected.owner, selected.summary
	declaration := summary.Function
	symbol := functionSymbol(owner.File.Module, declaration)
	fnType := owner.GenericSignatures[declaration]
	if fnType == nil {
		fnType, _ = owner.Types[declaration].(*types.FunctionType)
	}
	if fnType == nil {
		return FunctionContract{}, fmt.Errorf("missing checked signature for %q", symbol)
	}
	contract := FunctionContract{
		Symbol: symbol, Declaration: declaration.Pos(), Signature: a.functionSignature(fnType, declaration),
		Declared: summary.Declared, Inferred: summary.Inferred,
	}
	if summary.Gap != nil {
		gap := *summary.Gap
		contract.Gap = &gap
	}
	return contract, nil
}

func functionSymbol(module *ast.ModuleDecl, declaration *ast.FunctionDecl) string {
	if module == nil {
		return declaration.Name
	}
	return module.Name + "." + declaration.Name
}

func (a *analysis) functionSignature(fnType *types.FunctionType, declaration *ast.FunctionDecl) Signature {
	signature := Signature{
		Parameters: make([]Parameter, 0, len(declaration.Params)),
		ReturnType: fnType.Return.TypeName(), ReturnTypeIdentity: a.signatureTypeIdentity(declaration.ReturnType, fnType.Return),
		Async: declaration.IsAsync,
	}
	for i, param := range declaration.Params {
		signature.Parameters = append(signature.Parameters, Parameter{
			Name: param.Name, Type: fnType.Params[i].TypeName(), TypeIdentity: a.signatureTypeIdentity(param.Type, fnType.Params[i]),
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
