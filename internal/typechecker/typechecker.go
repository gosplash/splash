package typechecker

import (
	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/token"
	"gosplash.dev/splash/internal/types"
)

// TypedFile is the output of the type checker.
type TypedFile struct {
	File  *ast.File
	Types map[ast.Node]types.Type
}

type TypeChecker struct {
	globals         *Env
	typeDecls       map[string]*ast.TypeDecl
	constraintDecls map[string]*ast.ConstraintDecl
	fnDecls         map[string]*ast.FunctionDecl
	diags           []diagnostic.Diagnostic
}

func newGlobals() *Env {
	globals := NewEnv(nil)
	// Built-in functions available in all Splash programs.
	globals.Set("println", &types.FunctionType{
		Params: []types.Type{types.Unknown},
		Return: types.Void,
	})
	return globals
}

func New() *TypeChecker {
	return &TypeChecker{
		globals:         newGlobals(),
		typeDecls:       make(map[string]*ast.TypeDecl),
		constraintDecls: make(map[string]*ast.ConstraintDecl),
		fnDecls:         make(map[string]*ast.FunctionDecl),
	}
}

func (tc *TypeChecker) Check(file *ast.File) (*TypedFile, []diagnostic.Diagnostic) {
	tc.diags = nil
	tc.globals = newGlobals()
	tc.typeDecls = make(map[string]*ast.TypeDecl)
	tc.constraintDecls = make(map[string]*ast.ConstraintDecl)
	tc.fnDecls = make(map[string]*ast.FunctionDecl)
	typed := &TypedFile{File: file, Types: make(map[ast.Node]types.Type)}
	tc.pass1(file)
	tc.pass2(file, typed)
	return typed, tc.diags
}

// pass1: register all top-level declarations (handles forward references).
func (tc *TypeChecker) pass1(file *ast.File) {
	for _, u := range file.Uses {
		tc.processUseDecl(u)
	}
	for _, decl := range file.Declarations {
		switch d := decl.(type) {
		case *ast.TypeDecl:
			tc.typeDecls[d.Name] = d
			tc.globals.Set(d.Name, tc.buildNamedType(d))
		case *ast.ConstraintDecl:
			tc.constraintDecls[d.Name] = d
		case *ast.FunctionDecl:
			tc.fnDecls[d.Name] = d
			tc.globals.Set(d.Name, tc.buildFunctionType(d))
		case *ast.EnumDecl:
			tc.globals.Set(d.Name, types.Named(d.Name))
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
	}
}

func (tc *TypeChecker) buildNamedType(d *ast.TypeDecl) *types.NamedType {
	nt := &types.NamedType{Name: d.Name}
	for _, field := range d.Fields {
		nt.FieldClassifications = append(nt.FieldClassifications, classificationOf(field.Annotations))
	}
	return nt
}

func (tc *TypeChecker) buildFunctionType(d *ast.FunctionDecl) *types.FunctionType {
	ft := &types.FunctionType{}
	for _, p := range d.Params {
		ft.Params = append(ft.Params, tc.resolveTypeExpr(p.Type))
	}
	if d.ReturnType != nil {
		ft.Return = tc.resolveTypeExpr(d.ReturnType)
	} else {
		ft.Return = types.Void
	}
	return ft
}

// resolveMemberType resolves the type of a field access on a named type.
// objType is the type of the object being accessed; member is the field name;
// optional indicates the ?. operator was used (result is wrapped in OptionalType).
func (tc *TypeChecker) resolveMemberType(objType types.Type, member string, optional bool, pos token.Position) types.Type {
	// Unwrap optional: Person? → Person for field lookup
	inner := objType
	if opt, ok := objType.(*types.OptionalType); ok {
		inner = opt.Inner
	}

	nt, ok := inner.(*types.NamedType)
	if !ok {
		return types.Unknown
	}

	decl, ok := tc.typeDecls[nt.Name]
	if !ok {
		return types.Unknown
	}

	for _, field := range decl.Fields {
		if field.Name == member {
			fieldType := tc.resolveTypeExpr(field.Type)
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

// pass2: type-check all function bodies.
func (tc *TypeChecker) pass2(file *ast.File, typed *TypedFile) {
	for _, decl := range file.Declarations {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			tc.checkFunction(d, typed)
		case *ast.TypeDecl:
			tc.checkTypeDecl(d)
		}
	}
}

func (tc *TypeChecker) checkTypeDecl(d *ast.TypeDecl) {
	for _, field := range d.Fields {
		resolved := tc.resolveTypeExpr(field.Type)
		if resolved == types.Unknown {
			tc.errorf(field.Position, "unknown type %q", field.Type.String())
		}
	}
}

func (tc *TypeChecker) checkFunction(d *ast.FunctionDecl, typed *TypedFile) {
	env := NewEnv(tc.globals)
	typeParamEnv := make(map[string]*types.TypeParamType)
	for _, tp := range d.TypeParams {
		tpt := &types.TypeParamType{Name: tp.Name, Constraints: tp.Constraints}
		typeParamEnv[tp.Name] = tpt
		env.Set(tp.Name, tpt)
	}
	for _, p := range d.Params {
		env.Set(p.Name, tc.resolveTypeExprWithParams(p.Type, typeParamEnv))
	}
	var returnType types.Type = types.Void
	if d.ReturnType != nil {
		ret := tc.resolveTypeExprWithParams(d.ReturnType, typeParamEnv)
		if ret == types.Unknown {
			tc.errorf(d.ReturnType.Pos(), "unknown return type %q", d.ReturnType.String())
		}
		returnType = ret
	}
	if d.Body != nil {
		tc.checkBlock(d.Body, env, typed, typeParamEnv, returnType)
	}
}

func (tc *TypeChecker) checkBlock(block *ast.BlockStmt, env *Env, typed *TypedFile, typeParamEnv map[string]*types.TypeParamType, returnType types.Type) {
	for _, stmt := range block.Stmts {
		tc.checkStmt(stmt, env, typed, typeParamEnv, returnType)
	}
}

func (tc *TypeChecker) checkStmt(stmt ast.Stmt, env *Env, typed *TypedFile, typeParamEnv map[string]*types.TypeParamType, returnType types.Type) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		valType := tc.checkExpr(s.Value, env, typed, typeParamEnv)
		if s.Type != nil {
			declType := tc.resolveTypeExprWithParams(s.Type, typeParamEnv)
			if declType == types.Unknown {
				tc.errorf(s.Type.Pos(), "unknown type %q", s.Type.String())
			} else if !valType.IsAssignableTo(declType) {
				tc.errorf(s.Pos(), "cannot assign %s to %s", valType.TypeName(), declType.TypeName())
			}
			env.Set(s.Name, declType)
		} else {
			env.Set(s.Name, valType)
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

func (tc *TypeChecker) checkExpr(expr ast.Expr, env *Env, typed *TypedFile, typeParamEnv map[string]*types.TypeParamType) types.Type {
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
		if t, ok := env.Get(e.Name); ok {
			result = t
		} else {
			tc.errorf(e.Pos(), "undefined: %s", e.Name)
			result = types.Unknown
		}
	case *ast.CallExpr:
		result = tc.checkCallExpr(e, env, typed, typeParamEnv)
	case *ast.MemberExpr:
		objType := tc.checkExpr(e.Object, env, typed, typeParamEnv)
		result = tc.resolveMemberType(objType, e.Member, e.Optional, e.Pos())
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
		tc.checkExpr(e.Operand, env, typed, typeParamEnv)
		result = types.Bool
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
		// Look up the named type if it exists
		if t, ok := tc.globals.Get(e.TypeName); ok {
			result = t
		} else {
			result = types.Unknown
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
func (tc *TypeChecker) checkCallExpr(e *ast.CallExpr, env *Env, typed *TypedFile, typeParamEnv map[string]*types.TypeParamType) types.Type {
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
						resolved := tc.resolveTypeExprWithParams(e.TypeArgs[0], typeParamEnv)
						return &types.ResultType{Ok: resolved, Err: types.Named("AIError")}
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

	ft, ok := calleeType.(*types.FunctionType)
	if !ok {
		return types.Unknown
	}

	// Check constraint satisfaction for generic calls.
	calleeName := ""
	if ident, ok2 := e.Callee.(*ast.Ident); ok2 {
		calleeName = ident.Name
	}
	if calleeName != "" {
		if fnDecl, found := tc.fnDecls[calleeName]; found && len(fnDecl.TypeParams) > 0 {
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
	}

	if ft.Return != nil {
		return ft.Return
	}
	return types.Void
}

func (tc *TypeChecker) findFunctionDecl(name string) (*ast.FunctionDecl, bool) {
	if d, ok := tc.fnDecls[name]; ok {
		return d, true
	}
	return nil, false
}

func (tc *TypeChecker) resolveTypeExpr(expr ast.TypeExpr) types.Type {
	return tc.resolveTypeExprWithParams(expr, nil)
}

func (tc *TypeChecker) resolveTypeExprWithParams(expr ast.TypeExpr, typeParams map[string]*types.TypeParamType) types.Type {
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
			return &types.ListType{Element: tc.resolveTypeExprWithParams(e.TypeArgs[0], typeParams)}
		}
		if e.Name == "Result" && len(e.TypeArgs) == 2 {
			return &types.ResultType{
				Ok:  tc.resolveTypeExprWithParams(e.TypeArgs[0], typeParams),
				Err: tc.resolveTypeExprWithParams(e.TypeArgs[1], typeParams),
			}
		}
		if t, ok := tc.globals.Get(e.Name); ok {
			return t
		}
		return types.Unknown
	case *ast.OptionalTypeExpr:
		inner := tc.resolveTypeExprWithParams(e.Inner, typeParams)
		return &types.OptionalType{Inner: inner}
	case *ast.FnTypeExpr:
		ft := &types.FunctionType{}
		for _, p := range e.Params {
			ft.Params = append(ft.Params, tc.resolveTypeExprWithParams(p, typeParams))
		}
		if e.ReturnType != nil {
			ft.Return = tc.resolveTypeExprWithParams(e.ReturnType, typeParams)
		} else {
			ft.Return = types.Void
		}
		return ft
	}
	return types.Unknown
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
