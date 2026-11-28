package compiler

import (
	"errors"
	"fmt"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
)

const (
	DefaultContextLimit = 20
	MaxContextLimit     = 100
)

var (
	ErrModuleNotFound  = errors.New("module not found")
	ErrAmbiguousModule = errors.New("ambiguous module")
)

// ModuleSummary is a detached, source-ordered view of a module's exports.
type ModuleSummary struct {
	Module              string
	SourcePath          string
	Declarations        []ModuleDeclaration
	TotalDeclarations   int
	OmittedDeclarations int
}

type ModuleDeclaration struct {
	Kind     string
	Name     string
	Line     int
	Column   int
	Function *ModuleFunctionSummary
}

// ModuleFunctionSummary describes a function in the module projection.
// A representative gap uses capture-relative source paths.
type ModuleFunctionSummary struct {
	Signature Signature
	Declared  effects.EffectSet
	Inferred  effects.EffectSet
	Gap       *effects.CoverageGap
}

// SummarizeModule selects the entry file when name is empty. A named query
// matches a declared module name in the analyzed source closure.
func (p *AnalyzedProgram) SummarizeModule(name string, limit int) (ModuleSummary, error) {
	if p == nil || p.snapshot == nil {
		return ModuleSummary{}, ErrInvalidAnalysis
	}
	if limit < 1 || limit > MaxContextLimit {
		return ModuleSummary{}, fmt.Errorf("module summary limit must be between 1 and %d", MaxContextLimit)
	}

	a := p.snapshot
	moduleIndex := len(a.modules) - 1
	if name != "" {
		moduleIndex = -1
		for i, owner := range a.modules {
			if decl := owner.File.Module; decl != nil && decl.Name == name {
				if moduleIndex >= 0 {
					return ModuleSummary{}, fmt.Errorf("%w: %q", ErrAmbiguousModule, name)
				}
				moduleIndex = i
			}
		}
		if moduleIndex < 0 {
			return ModuleSummary{}, fmt.Errorf("%w: %q", ErrModuleNotFound, name)
		}
	}

	owner := a.modules[moduleIndex]
	file := owner.File
	path := a.entryPath
	if file.Module != nil {
		name, path = file.Module.Name, file.Module.Pos().File
	}
	result := ModuleSummary{
		Module: name, SourcePath: a.sourcePath(path),
		Declarations: make([]ModuleDeclaration, 0, min(limit, len(file.Declarations))),
	}
	exposed := make(map[string]bool, len(file.Exposes))
	for _, name := range file.Exposes {
		exposed[name] = true
	}

	var functions map[*ast.FunctionDecl]int
	for _, decl := range file.Declarations {
		var item ModuleDeclaration
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			item.Kind, item.Name = "function", d.Name
		case *ast.TypeDecl:
			item.Kind, item.Name = "record", d.Name
		case *ast.EnumDecl:
			item.Kind, item.Name = "enum", d.Name
		case *ast.ConstraintDecl:
			item.Kind, item.Name = "constraint", d.Name
		default:
			continue
		}
		if len(exposed) != 0 && !exposed[item.Name] {
			continue
		}
		result.TotalDeclarations++
		if len(result.Declarations) == limit {
			continue
		}
		pos := decl.Pos()
		item.Line, item.Column = pos.Line, pos.Column
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			if functions == nil {
				functions = make(map[*ast.FunctionDecl]int)
			}
			functions[fn] = len(result.Declarations)
		}
		result.Declarations = append(result.Declarations, item)
	}
	result.OmittedDeclarations = result.TotalDeclarations - len(result.Declarations)
	if len(functions) == 0 {
		return result, nil
	}

	// Inference order is independent of declaration ownership. Join only the
	// selected functions by checked declaration and stop when all are found.
	for _, summary := range a.effects {
		i, wanted := functions[summary.Function]
		if !wanted {
			continue
		}
		contract, err := a.projectFunctionContract(selectedFunction{owner: owner, summary: summary})
		if err != nil {
			return ModuleSummary{}, err
		}
		if gap := contract.Gap; gap != nil {
			if gap.Position.File != "" {
				gap.Position.File = a.sourcePath(gap.Position.File)
			}
			if gap.End.File != "" {
				gap.End.File = a.sourcePath(gap.End.File)
			}
		}
		result.Declarations[i].Function = &ModuleFunctionSummary{
			Signature: contract.Signature, Declared: contract.Declared, Inferred: contract.Inferred, Gap: contract.Gap,
		}
		delete(functions, summary.Function)
		if len(functions) == 0 {
			return result, nil
		}
	}
	return ModuleSummary{}, errors.New("missing effect summary for exported function")
}
