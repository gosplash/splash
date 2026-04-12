package codegen

import "gosplash.dev/splash/internal/ast"

func (e *Emitter) emitBlock(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		e.emitStmt(s)
	}
}

func (e *Emitter) emitStmt(s ast.Stmt) {
	switch stmt := s.(type) {
	case *ast.LetStmt:
		e.emitLetStmt(stmt)
	case *ast.ReturnStmt:
		e.emitReturnStmt(stmt)
	case *ast.ExprStmt:
		e.emitExprStmt(stmt)
	case *ast.IfStmt:
		e.emitIfStmt(stmt)
	case *ast.GuardStmt:
		e.emitGuardStmt(stmt)
	case *ast.ForStmt:
		e.emitForStmt(stmt)
	case *ast.AssignStmt:
		e.writeLine("%s = %s", e.emitExprStr(stmt.Target), e.emitExprStr(stmt.Value))
	case *ast.BlockStmt:
		e.writeLine("{")
		e.indent++
		e.emitBlock(stmt)
		e.indent--
		e.writeLine("}")
	}
}

func (e *Emitter) emitLetStmt(s *ast.LetStmt) {
	// Check if the RHS is a direct call to an approval-gated function.
	// If so, unwrap the (T, error) return and handle denial.
	if call, ok := s.Value.(*ast.CallExpr); ok {
		if ident, isIdent := call.Callee.(*ast.Ident); isIdent && (e.approveFns[ident.Name] || e.approveCallers[ident.Name]) && e.inApprovalFn {
			// Propagate error up — main() is now run() error, so this path is uniform.
			e.writeLine("%s, err := %s", s.Name, e.emitExprStr(s.Value))
			e.writeLine("if err != nil {")
			e.indent++
			if e.currentFnReturnType != nil {
				e.writeLine("return %s, err", e.zeroValueFor(e.currentFnReturnType))
			} else {
				e.writeLine("return err")
			}
			e.indent--
			e.writeLine("}")
			return
		}
	}
	// Default: no approval-gated call, original behavior.
	if s.Type != nil {
		e.writeLine("var %s %s = %s", s.Name, e.emitTypeName(s.Type), e.emitExprStr(s.Value))
	} else {
		e.writeLine("%s := %s", s.Name, e.emitExprStr(s.Value))
	}
}

func (e *Emitter) emitReturnStmt(s *ast.ReturnStmt) {
	if !e.inApprovalFn {
		if s.Value == nil {
			e.writeLine("return")
			return
		}
		e.writeLine("return %s", e.emitExprStr(s.Value))
		return
	}
	// Inside an approval-gated or approval-propagating function (not main):
	// append ", nil" to carry the error channel alongside the normal return value.
	if s.Value == nil {
		e.writeLine("return nil")
		return
	}
	if call, ok := s.Value.(*ast.CallExpr); ok {
		if ident, isIdent := call.Callee.(*ast.Ident); isIdent && (e.approveFns[ident.Name] || e.approveCallers[ident.Name]) {
			e.writeLine("return %s", e.emitExprStr(s.Value))
			return
		}
	}
	e.writeLine("return %s, nil", e.emitExprStr(s.Value))
}

func (e *Emitter) emitExprStmt(s *ast.ExprStmt) {
	// Check if this is a direct call to an approval-gated function used as a statement
	// (return value discarded). Handle the (T, error) or (error) return.
	if call, ok := s.Expr.(*ast.CallExpr); ok {
		if ident, identOk := call.Callee.(*ast.Ident); identOk && (e.approveFns[ident.Name] || e.approveCallers[ident.Name]) && e.inApprovalFn {
			calleeDecl := e.fnDecls[ident.Name]
			hasReturnVal := calleeDecl != nil && calleeDecl.ReturnType != nil
			// Propagate error up — uniform path for all error-propagating functions.
			if hasReturnVal {
				e.writeLine("if _, err := %s; err != nil {", e.emitExprStr(s.Expr))
			} else {
				e.writeLine("if err := %s; err != nil {", e.emitExprStr(s.Expr))
			}
			e.indent++
			if e.currentFnReturnType != nil {
				e.writeLine("return %s, err", e.zeroValueFor(e.currentFnReturnType))
			} else {
				e.writeLine("return err")
			}
			e.indent--
			e.writeLine("}")
			return
		}
	}
	e.writeLine("%s", e.emitExprStr(s.Expr))
}

func (e *Emitter) emitIfStmt(s *ast.IfStmt) {
	e.writeLine("if %s {", e.emitExprStr(s.Cond))
	e.indent++
	e.emitBlock(s.Then)
	e.indent--
	if s.Else != nil {
		e.writeLine("} else {")
		e.indent++
		// s.Else is always a *ast.BlockStmt or *ast.IfStmt from the parser.
		// Call emitBlock for BlockStmt (avoids double-brace from emitStmt's BlockStmt case).
		// Call emitStmt for IfStmt (else-if chain).
		switch els := s.Else.(type) {
		case *ast.BlockStmt:
			e.emitBlock(els)
		default:
			e.emitStmt(els)
		}
		e.indent--
		e.writeLine("}") // closes the else block
	} else {
		e.writeLine("}") // closes the if block
	}
}

func (e *Emitter) emitGuardStmt(s *ast.GuardStmt) {
	e.writeLine("if !(%s) {", e.emitExprStr(s.Cond))
	e.indent++
	e.emitBlock(s.Else)
	e.indent--
	e.writeLine("}")
}

func (e *Emitter) emitForStmt(s *ast.ForStmt) {
	e.writeLine("for _, %s := range %s {", s.Binding, e.emitExprStr(s.Iter))
	e.indent++
	e.emitBlock(s.Body)
	e.indent--
	e.writeLine("}")
}
