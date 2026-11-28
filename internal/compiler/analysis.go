// Package compiler analyzes Splash source and authorizes source artifacts.
package compiler

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/typechecker"
)

// AnalyzedProgram is a handle to captured source and resolved semantic facts.
type AnalyzedProgram struct {
	snapshot *analysis
}

// Analysis facts are immutable once returned by Analyze. The dependency
// index is memoized on demand without changing the captured facts.
type analysis struct {
	sources map[string]string
	modules []*typechecker.TypedFile
	merged  *ast.File
	graph   *callgraph.Graph
	effects []*effects.Summary

	dependencyOnce sync.Once
	dependencies   *dependencyIndex

	functionReferences map[ast.Expr]*ast.FunctionDecl
	printlnCalls       map[*ast.CallExpr]bool
	localNames         map[string]bool
}

// Analyze captures the entry and its imports and resolves their semantic facts.
func Analyze(path string) (*AnalyzedProgram, []diagnostic.Diagnostic, error) {
	program := &analysis{sources: make(map[string]string)}
	read := func(path string) ([]byte, error) {
		if source, ok := program.sources[path]; ok {
			return []byte(source), nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		program.sources[path] = string(data)
		return data, nil
	}

	src, err := read(path)
	if err != nil {
		return nil, nil, err
	}
	toks := lexer.New(path, string(src)).Tokenize()
	p := parser.New(path, toks)
	f, diags := p.ParseFile()
	if len(diags) > 0 {
		return nil, diags, fmt.Errorf("parse errors in %s", path)
	}

	tc := typechecker.New()
	tc.SetFileLoader(filepath.Dir(path), read)
	entry, typeErrs := tc.Check(f)
	if len(typeErrs) > 0 {
		return nil, typeErrs, fmt.Errorf("type errors")
	}

	program.modules = append(tc.LoadedTypedFiles(), entry)
	program.merged = mergeFiles(program.modules)
	files := make([]*ast.File, 0, len(program.modules))
	program.functionReferences = make(map[ast.Expr]*ast.FunctionDecl)
	program.printlnCalls = make(map[*ast.CallExpr]bool)
	program.localNames = make(map[string]bool)
	unresolved := make(map[*ast.CallExpr]bool)
	var effectFacts []effects.FunctionFacts
	for _, module := range program.modules {
		files = append(files, module.File)
		maps.Copy(program.functionReferences, module.FunctionReferences)
		maps.Copy(program.printlnCalls, module.PrintlnCalls)
		maps.Copy(program.localNames, module.LocalNames)
		maps.Copy(unresolved, module.UnresolvedCalls)
		effectFacts = append(effectFacts, module.EffectFacts...)
	}
	program.graph = callgraph.BuildResolved(files, program.functionReferences, unresolved)
	program.effects = effects.Infer(effectFacts)
	return &AnalyzedProgram{snapshot: program}, nil, nil
}

// mergeFiles builds a combined ast.File where all imported files' declarations
// come first (in load order), followed by the main file's declarations.
// The main file's module declaration and package name are preserved.
// This ensures imported types are defined before the main file references them
// in the generated Go output.
func mergeFiles(modules []*typechecker.TypedFile) *ast.File {
	main := modules[len(modules)-1].File
	if len(modules) == 1 {
		return main
	}
	merged := &ast.File{
		Module:   main.Module,
		Exposes:  main.Exposes,
		Position: main.Position,
	}
	for _, module := range modules {
		f := module.File
		merged.Uses = append(merged.Uses, f.Uses...)
		merged.Declarations = append(merged.Declarations, f.Declarations...)
	}
	return merged
}
