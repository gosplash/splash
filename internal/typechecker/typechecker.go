package typechecker

import (
	"path/filepath"
	"slices"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/token"
	"gosplash.dev/splash/internal/types"
)

// TypedFile is the output of the type checker.
type TypedFile struct {
	File *ast.File
	// Types retains expression, annotation, and declaration types from checking this file.
	Types              map[ast.Node]types.Type
	FunctionReferences map[ast.Expr]*ast.FunctionDecl
	LocalReferences    map[*ast.Ident]LocalBinding
	PrintlnCalls       map[*ast.CallExpr]bool
	UnresolvedCalls    map[*ast.CallExpr]bool
	LocalNames         map[string]bool
}

// moduleState is shared across the entire import chain (main file + all transitive imports).
// It prevents loading the same module twice and detects circular dependencies.
type moduleState struct {
	fileLoader   func(path string) ([]byte, error) // nil = no filesystem loading
	baseDir      string                            // directory of the main file
	loadedFiles  []*TypedFile                      // files in load order (imports before importers)
	cache        map[string]*TypedFile             // path → checked file (dedup)
	loading      map[string]bool                   // currently-loading paths (cycle detection)
	recordOwners map[*ast.TypeDecl]*TypedFile
}

type TypeChecker struct {
	globals         *Env
	recordOwners    map[*ast.TypeDecl]*TypedFile
	constraintDecls map[string]*ast.ConstraintDecl
	moduleFunctions map[*types.ModuleType]map[string]*ast.FunctionDecl
	diags           []diagnostic.Diagnostic
	modules         *moduleState // nil if no loader configured
}

func newGlobals() *Env {
	globals := NewEnv(nil)
	// Built-in functions available in all Splash programs.
	globals.symbols["println"] = binding{println: true, typ: &types.FunctionType{
		Params: []types.Type{types.Unknown},
		Return: types.Void,
	}}
	return globals
}

// builtinConstraints are constraint names the compiler recognizes without
// a user declaration. Loggable is classification-gated: any type whose
// classification exceeds LoggableMaxClassification cannot satisfy it.
var builtinConstraints = map[string]*ast.ConstraintDecl{
	"Loggable": {Name: "Loggable"},
}

func New() *TypeChecker {
	tc := &TypeChecker{
		globals:         newGlobals(),
		constraintDecls: make(map[string]*ast.ConstraintDecl),
	}
	for name, decl := range builtinConstraints {
		tc.constraintDecls[name] = decl
	}
	return tc
}

// SetFileLoader configures user-defined module loading.
// baseDir is the directory to resolve relative import paths from.
// loader reads a file by path; in tests, an in-memory map works.
func (tc *TypeChecker) SetFileLoader(baseDir string, loader func(path string) ([]byte, error)) {
	tc.modules = &moduleState{
		fileLoader:   loader,
		baseDir:      baseDir,
		cache:        make(map[string]*TypedFile),
		loading:      make(map[string]bool),
		recordOwners: make(map[*ast.TypeDecl]*TypedFile),
	}
}

// LoadedTypedFiles returns the successfully checked dependencies in load
// order (imports before importers). It excludes the entry file.
func (tc *TypeChecker) LoadedTypedFiles() []*TypedFile {
	if tc.modules == nil {
		return nil
	}
	return slices.Clone(tc.modules.loadedFiles)
}

func (tc *TypeChecker) Check(file *ast.File) (*TypedFile, []diagnostic.Diagnostic) {
	tc.diags = nil
	tc.globals = newGlobals()
	if tc.modules != nil {
		tc.recordOwners = tc.modules.recordOwners
	} else {
		tc.recordOwners = make(map[*ast.TypeDecl]*TypedFile)
	}
	tc.constraintDecls = make(map[string]*ast.ConstraintDecl)
	for name, decl := range builtinConstraints {
		tc.constraintDecls[name] = decl
	}
	tc.moduleFunctions = make(map[*types.ModuleType]map[string]*ast.FunctionDecl)
	typed := &TypedFile{
		File:               file,
		Types:              make(map[ast.Node]types.Type),
		FunctionReferences: make(map[ast.Expr]*ast.FunctionDecl),
		LocalReferences:    make(map[*ast.Ident]LocalBinding),
		PrintlnCalls:       make(map[*ast.CallExpr]bool),
		UnresolvedCalls:    make(map[*ast.CallExpr]bool),
		LocalNames:         make(map[string]bool),
	}
	tc.pass1(file, typed)
	tc.pass2(file, typed)
	return typed, tc.diags
}

// pass1: register all top-level declarations (handles forward references).
func (tc *TypeChecker) pass1(file *ast.File, typed *TypedFile) {
	for _, u := range file.Uses {
		tc.processUseDecl(u)
	}
	recordNames := make(map[string]bool)
	for _, decl := range file.Declarations {
		switch d := decl.(type) {
		case *ast.TypeDecl:
			if recordNames[d.Name] {
				tc.errorf(d.Position, "duplicate type %q in module", d.Name)
				continue
			}
			recordNames[d.Name] = true
			nt := tc.buildNamedType(d)
			tc.globals.Set(d.Name, nt)
			typed.Types[d] = nt
			tc.recordOwners[d] = typed
		case *ast.ConstraintDecl:
			tc.constraintDecls[d.Name] = d
		case *ast.EnumDecl:
			tc.globals.Set(d.Name, types.Named(d.Name))
		}
	}
	// Resolve signatures after all local type names are available.
	functionNames := make(map[string]bool)
	for _, decl := range file.Declarations {
		if d, ok := decl.(*ast.FunctionDecl); ok {
			if functionNames[d.Name] {
				tc.errorf(d.Position, "duplicate function %q in module", d.Name)
			}
			functionNames[d.Name] = true
			ft := tc.buildFunctionType(d)
			tc.globals.symbols[d.Name] = binding{typ: ft, function: d}
			typed.Types[d] = ft
		}
	}
}

// processUseDecl injects stdlib module bindings into globals.
func (tc *TypeChecker) processUseDecl(u *ast.UseDecl) {
	switch u.Path {
	case "std/ai":
		// Inject 'ai' as an AIAdapter instance and 'AIError' as a named type.
		tc.globals.Set("ai", types.Named("AIAdapter"))
		tc.globals.Set("AIError", types.Named("AIError"))
	default:
		tc.loadUserModule(u)
	}
}

func (tc *TypeChecker) loadUserModule(u *ast.UseDecl) {
	if tc.modules == nil || tc.modules.fileLoader == nil {
		tc.errorf(u.Position, "cannot import module %q: no file loader configured", u.Path)
		return
	}

	// Resolve: "billing" → "<baseDir>/billing.splash", "app/users" → "<baseDir>/app/users.splash"
	relPath := u.Path + ".splash"
	filePath := filepath.Join(tc.modules.baseDir, relPath)

	// Cycle detection
	if tc.modules.loading[filePath] {
		tc.errorf(u.Position, "circular import: %q is already being loaded", u.Path)
		return
	}

	// Cache hit
	if cached, ok := tc.modules.cache[filePath]; ok {
		tc.injectModule(cached, u)
		return
	}

	// Load and parse
	src, err := tc.modules.fileLoader(filePath)
	if err != nil {
		tc.errorf(u.Position, "cannot load module %q: %v", u.Path, err)
		return
	}

	toks := lexer.New(filePath, string(src)).Tokenize()
	p := parser.New(filePath, toks)
	imported, diags := p.ParseFile()
	for _, d := range diags {
		tc.diags = append(tc.diags, d)
	}
	if imported == nil {
		return
	}

	// Mark loading, type-check, unmark
	tc.modules.loading[filePath] = true
	sub := New()
	sub.modules = tc.modules // share state for transitive imports
	typed, subDiags := sub.Check(imported)
	tc.diags = append(tc.diags, subDiags...)
	delete(tc.modules.loading, filePath)

	// If the imported module has type errors, don't inject its symbols.
	// The importer should not receive partial type information from a broken dependency.
	for _, d := range subDiags {
		if d.Severity == diagnostic.Error {
			return
		}
	}

	// Cache and record
	tc.modules.cache[filePath] = typed
	tc.modules.loadedFiles = append(tc.modules.loadedFiles, typed)

	// Inject symbols
	tc.injectModule(typed, u)
}

// injectModule copies exported types, functions, and constraints from an
// imported file into this checker's namespace.
// If the file has an expose list, only listed names are injected.
// Otherwise all declarations are injected.
func (tc *TypeChecker) injectModule(typed *TypedFile, u *ast.UseDecl) {
	f := typed.File
	exposed := make(map[string]bool)
	for _, name := range f.Exposes {
		exposed[name] = true
	}
	exportAll := len(f.Exposes) == 0
	namespaceExports := make(map[string]types.Type)
	namespaceFunctions := make(map[string]*ast.FunctionDecl)

	for _, decl := range f.Declarations {
		switch d := decl.(type) {
		case *ast.TypeDecl:
			if exportAll || exposed[d.Name] {
				nt := typed.Types[d]
				tc.setImportedBinding(d.Name, binding{typ: nt, imported: d})
				namespaceExports[d.Name] = nt
			}
		case *ast.FunctionDecl:
			if exportAll || exposed[d.Name] {
				ft := typed.Types[d]
				tc.setImportedBinding(d.Name, binding{typ: ft, function: d, imported: d})
				namespaceExports[d.Name] = ft
				namespaceFunctions[d.Name] = d
			}
		case *ast.ConstraintDecl:
			if exportAll || exposed[d.Name] {
				tc.constraintDecls[d.Name] = d
			}
		}
	}

	if ns := namespaceName(u, f); ns != "" {
		moduleType := &types.ModuleType{Name: ns, Exports: namespaceExports}
		tc.moduleFunctions[moduleType] = namespaceFunctions
		tc.setImportedBinding(ns, binding{typ: moduleType, imported: f})
	}
}

// setImportedBinding detects collisions across function, type, and namespace
// imports. Importing the same declaration through another alias is harmless.
func (tc *TypeChecker) setImportedBinding(name string, next binding) {
	previous := tc.globals.symbols[name]
	next.ambiguous = previous.ambiguous || previous.imported != nil && previous.imported != next.imported
	tc.globals.symbols[name] = next
}

func namespaceName(u *ast.UseDecl, f *ast.File) string {
	if u.Alias != "" {
		return u.Alias
	}
	if f != nil && f.Module != nil && f.Module.Name != "" {
		return f.Module.Name
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (tc *TypeChecker) buildNamedType(d *ast.TypeDecl) *types.NamedType {
	nt := &types.NamedType{Name: d.Name, Declaration: d}
	for _, field := range d.Fields {
		nt.FieldClassifications = append(nt.FieldClassifications, classificationOf(field.Annotations))
	}
	return nt
}

func (tc *TypeChecker) buildFunctionType(d *ast.FunctionDecl) *types.FunctionType {
	var typeParams map[string]types.Type
	if len(d.TypeParams) > 0 {
		// Call signatures are not instantiated yet. Unresolved type parameters
		// still shadow any module type with the same name.
		typeParams = make(map[string]types.Type, len(d.TypeParams))
		for _, param := range d.TypeParams {
			typeParams[param.Name] = types.Unknown
		}
	}
	ft := &types.FunctionType{}
	for _, p := range d.Params {
		ft.Params = append(ft.Params, tc.resolveTypeExpr(p.Type, typeParams, false))
	}
	if d.ReturnType != nil {
		ft.Return = tc.resolveTypeExpr(d.ReturnType, typeParams, false)
	} else {
		ft.Return = types.Void
	}
	return ft
}

// resolveMemberType resolves the type of a field access on a named type.
// objType is the type of the object being accessed; member is the field name;
// optional indicates the ?. operator was used (result is wrapped in OptionalType).
func (tc *TypeChecker) resolveMemberType(objType types.Type, member string, optional bool, pos token.Position) types.Type {
	if mod, ok := objType.(*types.ModuleType); ok {
		if exported, found := mod.Exports[member]; found {
			return exported
		}
		tc.errorf(pos, "module %s has no exported member %q", mod.Name, member)
		return types.Unknown
	}
	// Unwrap optional: Person? → Person for field lookup
	inner := objType
	if opt, ok := objType.(*types.OptionalType); ok {
		inner = opt.Inner
	}

	nt, ok := inner.(*types.NamedType)
	if !ok {
		return types.Unknown
	}

	decl := nt.Declaration
	if decl == nil {
		return types.Unknown
	}

	for _, field := range decl.Fields {
		if field.Name == member {
			fieldType := tc.recordOwners[decl].Types[field.Type]
			if optional {
				if _, isOpt := fieldType.(*types.OptionalType); !isOpt {
					return &types.OptionalType{Inner: fieldType}
				}
			}
			return fieldType
		}
	}

	tc.errorf(pos, "type %s has no field %q", nt.Name, member)
	return types.Unknown
}

// pass2 resolves field types before checking bodies that may project them.
func (tc *TypeChecker) pass2(file *ast.File, typed *TypedFile) {
	for _, decl := range file.Declarations {
		if d, ok := decl.(*ast.TypeDecl); ok {
			tc.checkTypeDecl(d, typed)
		}
	}
	for _, decl := range file.Declarations {
		if d, ok := decl.(*ast.FunctionDecl); ok {
			tc.checkFunction(d, typed)
		}
	}
}

func (tc *TypeChecker) checkTypeDecl(d *ast.TypeDecl, typed *TypedFile) {
	for _, field := range d.Fields {
		resolved := tc.resolveTypeExpr(field.Type, nil, true)
		typed.Types[field.Type] = resolved
		if resolved == types.Unknown {
			tc.errorf(field.Position, "unknown type %q", field.Type.String())
		}
	}
}

func (tc *TypeChecker) checkFunction(d *ast.FunctionDecl, typed *TypedFile) {
	env := NewEnv(tc.globals)
	typeParamEnv := make(map[string]types.Type)
	for _, tp := range d.TypeParams {
		for _, constraintName := range tp.Constraints {
			if _, ok := tc.constraintDecls[constraintName]; !ok {
				tc.errorf(d.Position, "unknown constraint %q in type parameter %s", constraintName, tp.Name)
			}
		}
		tpt := &types.TypeParamType{Name: tp.Name, Constraints: tp.Constraints}
		typeParamEnv[tp.Name] = tpt
		env.Set(tp.Name, tpt)
	}
	for i := range d.Params {
		p := &d.Params[i]
		paramType := tc.resolveTypeExpr(p.Type, typeParamEnv, true)
		if p.Type != nil {
			typed.Types[p.Type] = paramType
		}
		env.symbols[p.Name] = binding{
			typ:   paramType,
			local: LocalBinding{Param: p},
		}
		typed.LocalNames[p.Name] = true
	}
	var returnType types.Type = types.Void
	if d.ReturnType != nil {
		ret := tc.resolveTypeExpr(d.ReturnType, typeParamEnv, true)
		typed.Types[d.ReturnType] = ret
		if ret == types.Unknown {
			tc.errorf(d.ReturnType.Pos(), "unknown return type %q", d.ReturnType.String())
		}
		returnType = ret
	}
	if d.Body != nil {
		tc.checkBlock(d.Body, env, typed, typeParamEnv, returnType)
	}
}

func (tc *TypeChecker) checkBlock(block *ast.BlockStmt, env *Env, typed *TypedFile, typeParamEnv map[string]types.Type, returnType types.Type) {
	for _, stmt := range block.Stmts {
		tc.checkStmt(stmt, env, typed, typeParamEnv, returnType)
	}
}

func (tc *TypeChecker) checkStmt(stmt ast.Stmt, env *Env, typed *TypedFile, typeParamEnv map[string]types.Type, returnType types.Type) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		typed.LocalNames[s.Name] = true
		valType := tc.checkExpr(s.Value, env, typed, typeParamEnv)
		if s.Type != nil {
			declType := tc.resolveTypeExpr(s.Type, typeParamEnv, true)
			typed.Types[s.Type] = declType
			if declType == types.Unknown {
				tc.errorf(s.Type.Pos(), "unknown type %q", s.Type.String())
			} else if !valType.IsAssignableTo(declType) {
				tc.errorf(s.Pos(), "cannot assign %s to %s", valType.TypeName(), declType.TypeName())
			}
			env.symbols[s.Name] = binding{typ: declType, local: LocalBinding{Let: s}}
		} else {
			env.symbols[s.Name] = binding{typ: valType, local: LocalBinding{Let: s}}
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			valType := tc.checkExpr(s.Value, env, typed, typeParamEnv)
			if returnType != types.Void && returnType != types.Unknown && valType != types.Unknown && !valType.IsAssignableTo(returnType) {
				tc.errorf(s.Pos(), "cannot return %s from function returning %s", valType.TypeName(), returnType.TypeName())
			}
		}
	case *ast.ExprStmt:
		tc.checkExpr(s.Expr, env, typed, typeParamEnv)
	case *ast.BlockStmt:
		inner := NewEnv(env)
		tc.checkBlock(s, inner, typed, typeParamEnv, returnType)
	case *ast.IfStmt:
		tc.checkExpr(s.Cond, env, typed, typeParamEnv)
		tc.checkBlock(s.Then, NewEnv(env), typed, typeParamEnv, returnType)
		if s.Else != nil {
			tc.checkStmt(s.Else, env, typed, typeParamEnv, returnType)
		}
	case *ast.GuardStmt:
		tc.checkExpr(s.Cond, env, typed, typeParamEnv)
		tc.checkBlock(s.Else, NewEnv(env), typed, typeParamEnv, returnType)
	case *ast.ForStmt:
		typed.LocalNames[s.Binding] = true
		iterType := tc.checkExpr(s.Iter, env, typed, typeParamEnv)
		inner := NewEnv(env)
		if lt, ok := iterType.(*types.ListType); ok {
			inner.Set(s.Binding, lt.Element)
		} else {
			inner.Set(s.Binding, types.Unknown)
		}
		tc.checkBlock(s.Body, inner, typed, typeParamEnv, returnType)
	case *ast.AssignStmt:
		tc.checkExpr(s.Target, env, typed, typeParamEnv)
		tc.checkExpr(s.Value, env, typed, typeParamEnv)
	}
}

func (tc *TypeChecker) checkExpr(expr ast.Expr, env *Env, typed *TypedFile, typeParamEnv map[string]types.Type) types.Type {
	var result types.Type
	switch e := expr.(type) {
	case *ast.IntLiteral:
		result = types.Int
	case *ast.FloatLiteral:
		result = types.Float
	case *ast.StringLiteral:
		result = types.String
	case *ast.BoolLiteral:
		result = types.Bool
	case *ast.NoneLiteral:
		result = &types.OptionalType{Inner: types.Unknown}
	case *ast.Ident:
		if b, ok := env.lookup(e.Name); ok {
			if b.ambiguous {
				tc.errorf(e.Pos(), "ambiguous imported name %q; use a qualified name or distinct import alias", e.Name)
				result = types.Unknown
			} else {
				result = b.typ
				if b.function != nil {
					typed.FunctionReferences[e] = b.function
				}
				if b.local != (LocalBinding{}) {
					typed.LocalReferences[e] = b.local
				}
			}
		} else {
			tc.errorf(e.Pos(), "undefined: %s", e.Name)
			result = types.Unknown
		}
	case *ast.CallExpr:
		result = tc.checkCallExpr(e, env, typed, typeParamEnv)
	case *ast.MemberExpr:
		objType := tc.checkExpr(e.Object, env, typed, typeParamEnv)
		result = tc.resolveMemberType(objType, e.Member, e.Optional, e.Pos())
		if mod, ok := objType.(*types.ModuleType); ok && !e.Optional {
			if fn := tc.moduleFunctions[mod][e.Member]; fn != nil {
				typed.FunctionReferences[e] = fn
			}
		}
	case *ast.BinaryExpr:
		left := tc.checkExpr(e.Left, env, typed, typeParamEnv)
		tc.checkExpr(e.Right, env, typed, typeParamEnv)
		switch e.Op {
		case token.EQ, token.NEQ, token.LT, token.LTE, token.GT, token.GTE,
			token.AND_AND, token.OR_OR:
			result = types.Bool
		default:
			result = left // arithmetic: inherit left operand type
		}
	case *ast.UnaryExpr:
		operand := tc.checkExpr(e.Operand, env, typed, typeParamEnv)
		result = types.Bool
		if e.Op == token.MINUS {
			switch operand {
			case types.Int, types.Float, types.Unknown:
				result = operand
			default:
				tc.errorf(e.Pos(), "unary - requires Int or Float, got %s", operand.TypeName())
				result = types.Unknown
			}
		}
	case *ast.NullCoalesceExpr:
		left := tc.checkExpr(e.Left, env, typed, typeParamEnv)
		tc.checkExpr(e.Right, env, typed, typeParamEnv)
		if opt, ok := left.(*types.OptionalType); ok {
			result = opt.Inner
		} else {
			result = left
		}
	case *ast.ListLiteral:
		if len(e.Elements) > 0 {
			elemType := tc.checkExpr(e.Elements[0], env, typed, typeParamEnv)
			for _, el := range e.Elements[1:] {
				tc.checkExpr(el, env, typed, typeParamEnv)
			}
			result = &types.ListType{Element: elemType}
		} else {
			result = &types.ListType{Element: types.Unknown}
		}
	case *ast.IndexExpr:
		objType := tc.checkExpr(e.Object, env, typed, typeParamEnv)
		tc.checkExpr(e.Index, env, typed, typeParamEnv)
		if lt, ok := objType.(*types.ListType); ok {
			result = lt.Element
		} else {
			result = types.Unknown
		}
	case *ast.StructLiteral:
		result = types.Unknown
		if nt, ok := tc.resolveTypeName(e.TypeName, e.Pos(), true).(*types.NamedType); ok && nt.Declaration != nil {
			result = nt
		}
		for _, f := range e.Fields {
			tc.checkExpr(f.Value, env, typed, typeParamEnv)
		}
	default:
		result = types.Unknown
	}
	if result != nil {
		typed.Types[expr] = result
	}
	return result
}

// checkCallExpr handles function calls, including constraint satisfaction for generic calls.
func (tc *TypeChecker) checkCallExpr(e *ast.CallExpr, env *Env, typed *TypedFile, typeParamEnv map[string]types.Type) types.Type {
	// Special case: ai.prompt<T>(...) on AIAdapter → Result<T, AIError>
	// Look up 'ai' directly in env to avoid double-checking (which would double-report errors).
	if mem, ok := e.Callee.(*ast.MemberExpr); ok && mem.Member == "prompt" {
		if ident, ok2 := mem.Object.(*ast.Ident); ok2 {
			if t, ok3 := env.Get(ident.Name); ok3 {
				if nt, ok4 := t.(*types.NamedType); ok4 && nt.Name == "AIAdapter" {
					typed.Types[mem.Object] = t
					typed.Types[e.Callee] = types.Unknown
					for _, arg := range e.Args {
						tc.checkExpr(arg, env, typed, typeParamEnv)
					}
					if len(e.TypeArgs) == 1 {
						resolved := tc.resolveTypeExpr(e.TypeArgs[0], typeParamEnv, true)
						return resolved
					}
					return types.Unknown
				}
			}
		}
	}

	calleeType := tc.checkExpr(e.Callee, env, typed, typeParamEnv)
	argTypes := make([]types.Type, len(e.Args))
	for i, arg := range e.Args {
		argTypes[i] = tc.checkExpr(arg, env, typed, typeParamEnv)
	}

	var println bool
	if ident, ok := e.Callee.(*ast.Ident); ok {
		b, _ := env.lookup(ident.Name)
		println = b.println
	}
	fnDecl := typed.FunctionReferences[e.Callee]
	if fnDecl == nil && !println {
		typed.UnresolvedCalls[e] = true
	}

	ft, ok := calleeType.(*types.FunctionType)
	if !ok {
		return types.Unknown
	}
	// Generic call signatures are not instantiated by this checker.
	if fnDecl == nil || len(fnDecl.TypeParams) == 0 {
		for i, param := range ft.Params[:min(len(ft.Params), len(argTypes))] {
			if record, ok := param.(*types.NamedType); ok && record.Declaration != nil &&
				argTypes[i] != types.Unknown && !argTypes[i].IsAssignableTo(param) {
				tc.errorf(e.Args[i].Pos(), "cannot pass %s as argument %d of type %s", argTypes[i].TypeName(), i+1, param.TypeName())
			}
		}
	}

	// Classification checks apply to the builtin rather than a shadowing declaration.
	if println {
		typed.PrintlnCalls[e] = true
		for i, argType := range argTypes {
			if argType.Classification() > types.LoggableMaxClassification {
				tc.errorf(e.Args[i].Pos(), "cannot pass %s type %s to println — use a public accessor or a type that satisfies Loggable",
					argType.Classification(), argType.TypeName())
			}
		}
	}

	if fnDecl != nil && len(fnDecl.TypeParams) > 0 {
		// Build a map from type param name -> inferred concrete type from args.
		// Walk function params to find which arg position each type param is used in.
		typeParamInferred := make(map[string]types.Type)
		for i, param := range fnDecl.Params {
			if i >= len(argTypes) {
				break
			}
			// If param type is a named type expr matching a type param, record the inference.
			if named, ok2 := param.Type.(*ast.NamedTypeExpr); ok2 {
				for _, tp := range fnDecl.TypeParams {
					if named.Name == tp.Name {
						typeParamInferred[tp.Name] = argTypes[i]
					}
				}
			}
		}
		// Now check constraints.
		for _, tp := range fnDecl.TypeParams {
			inferredType, hasInferred := typeParamInferred[tp.Name]
			if !hasInferred {
				continue
			}
			for _, constraintName := range tp.Constraints {
				if constraintName == "Loggable" {
					if inferredType.Classification() > types.LoggableMaxClassification {
						tc.errorf(e.Pos(), "type %s has classification %s and cannot satisfy constraint Loggable",
							inferredType.TypeName(), inferredType.Classification())
					}
				}
			}
		}
	}

	if ft.Return != nil {
		return ft.Return
	}
	return types.Void
}

func (tc *TypeChecker) resolveTypeExpr(expr ast.TypeExpr, typeParams map[string]types.Type, reportAmbiguity bool) types.Type {
	if expr == nil {
		return types.Void
	}
	switch e := expr.(type) {
	case *ast.NamedTypeExpr:
		if typeParams != nil {
			if tp, ok := typeParams[e.Name]; ok {
				return tp
			}
		}
		switch e.Name {
		case "String":
			return types.String
		case "Int":
			return types.Int
		case "Float":
			return types.Float
		case "Bool":
			return types.Bool
		case "Void":
			return types.Void
		}
		if e.Name == "List" && len(e.TypeArgs) == 1 {
			return &types.ListType{Element: tc.resolveTypeExpr(e.TypeArgs[0], typeParams, reportAmbiguity)}
		}
		if e.Name == "Result" && len(e.TypeArgs) == 2 {
			return &types.ResultType{
				Ok:  tc.resolveTypeExpr(e.TypeArgs[0], typeParams, reportAmbiguity),
				Err: tc.resolveTypeExpr(e.TypeArgs[1], typeParams, reportAmbiguity),
			}
		}
		if t := tc.resolveTypeName(e.Name, e.Pos(), reportAmbiguity); t != nil {
			return t
		}
		return types.Unknown
	case *ast.OptionalTypeExpr:
		inner := tc.resolveTypeExpr(e.Inner, typeParams, reportAmbiguity)
		return &types.OptionalType{Inner: inner}
	case *ast.FnTypeExpr:
		ft := &types.FunctionType{}
		for _, p := range e.Params {
			ft.Params = append(ft.Params, tc.resolveTypeExpr(p, typeParams, reportAmbiguity))
		}
		if e.ReturnType != nil {
			ft.Return = tc.resolveTypeExpr(e.ReturnType, typeParams, reportAmbiguity)
		} else {
			ft.Return = types.Void
		}
		return ft
	}
	return types.Unknown
}

func (tc *TypeChecker) resolveTypeName(name string, pos token.Position, reportAmbiguity bool) types.Type {
	prefix, member, qualified := strings.Cut(name, ".")
	b, ok := tc.globals.lookup(prefix)
	if !ok {
		return nil
	}
	if b.ambiguous {
		if reportAmbiguity {
			tc.errorf(pos, "ambiguous imported name %q; use a qualified name or distinct import alias", name)
		}
		return nil
	}
	if !qualified {
		return b.typ
	}
	if mod, ok := b.typ.(*types.ModuleType); ok {
		return mod.Exports[member]
	}
	return nil
}

func (tc *TypeChecker) errorf(pos token.Position, format string, args ...any) {
	tc.diags = append(tc.diags, diagnostic.Errorf(pos, format, args...))
}

func classificationOf(annotations []ast.Annotation) types.Classification {
	max := types.ClassPublic
	for _, a := range annotations {
		var c types.Classification
		switch a.Kind {
		case ast.AnnotRestricted:
			c = types.ClassRestricted
		case ast.AnnotSensitive:
			c = types.ClassSensitive
		case ast.AnnotInternal:
			c = types.ClassInternal
		default:
			continue
		}
		if c > max {
			max = c
		}
	}
	return max
}
