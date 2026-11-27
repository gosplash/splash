// Package lowering translates admitted, typed Splash source into semantic IR.
package lowering

import (
	"fmt"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/ir"
	"gosplash.dev/splash/internal/token"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

const vmSubset = "SPLASH_VM_UNSUPPORTED_SOURCE"

type AdmissionError struct {
	Source token.Position
	Detail string
}

func (e *AdmissionError) Error() string {
	if e.Source.File == "" {
		return fmt.Sprintf("%s: %s", vmSubset, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Source, vmSubset, e.Detail)
}

func unsupported(source token.Position, detail string) *AdmissionError {
	return &AdmissionError{Source: source, Detail: detail}
}

// LowerVM consumes the same typed bindings as the compiler. The experimental
// subset supports functions in a single ordinary module.
func LowerVM(modules []*typechecker.TypedFile) (ir.Program, error) {
	if len(modules) == 0 || modules[len(modules)-1] == nil || modules[len(modules)-1].File == nil {
		return ir.Program{}, unsupported(token.Position{}, "VM requires a checked entry module")
	}
	module := modules[len(modules)-1]
	file := module.File
	if len(file.Uses) != 0 {
		return ir.Program{}, unsupported(file.Uses[0].Pos(), "VM-002 does not support imports")
	}
	if len(modules) != 1 || file.Module == nil {
		return ir.Program{}, unsupported(file.Pos(), "VM-002 requires one module")
	}
	if len(file.Module.Annotations) != 0 || len(file.Exposes) != 0 {
		return ir.Program{}, unsupported(file.Module.Pos(), "VM-002 does not support module annotations or exports")
	}

	program := ir.Program{Entry: -1}
	targets := make(map[*ast.FunctionDecl]int)
	declarations := make([]*ast.FunctionDecl, 0, len(file.Declarations))
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || len(fn.TypeParams) != 0 || len(fn.Effects) != 0 || len(fn.Annotations) != 0 || fn.IsAsync || fn.Body == nil {
			return ir.Program{}, unsupported(decl.Pos(), "VM-002 supports ordinary, non-generic functions with bodies")
		}
		signature, err := signatureFor(fn)
		if err != nil {
			return ir.Program{}, err
		}
		id := len(program.Functions)
		if fn.Name == "main" {
			if signature.Result != ir.Void || len(signature.Params) != 0 {
				return ir.Program{}, unsupported(fn.Pos(), "main must have no parameters and return Void")
			}
			program.Entry = id
		}
		targets[fn] = id
		declarations = append(declarations, fn)
		program.Functions = append(program.Functions, signature)
	}
	if program.Entry < 0 {
		return ir.Program{}, unsupported(file.Module.Pos(), "VM requires a main function")
	}

	for id, fn := range declarations {
		builder := functionBuilder{
			module: module, targets: targets, functions: program.Functions,
			fn: &program.Functions[id], slots: make(map[typechecker.LocalBinding]int),
		}
		for i := range fn.Params {
			builder.slots[typechecker.LocalBinding{Param: &fn.Params[i]}] = i
		}
		builder.current = builder.addBlock()
		if err := builder.lowerBlock(fn.Body); err != nil {
			return ir.Program{}, err
		}
		if builder.current >= 0 {
			if builder.fn.Result != ir.Void {
				return ir.Program{}, unsupported(fn.Pos(), "non-Void function can reach its end without returning")
			}
			builder.terminate(ir.Terminator{Op: ir.Return, Value: -1, Source: fn.Pos()})
		}
	}
	return program, nil
}

func signatureFor(fn *ast.FunctionDecl) (ir.Function, error) {
	result := ir.Function{Result: ir.Void, Source: fn.Pos()}
	if fn.ReturnType != nil {
		result.Result = declaredKind(fn.ReturnType)
		if result.Result == 0 {
			return ir.Function{}, unsupported(fn.ReturnType.Pos(), "VM-002 requires a primitive result type")
		}
	}
	for _, param := range fn.Params {
		kind := declaredKind(param.Type)
		if kind == 0 || kind == ir.Void || param.Default != nil || param.Variadic {
			return ir.Function{}, unsupported(param.Pos, "VM-002 requires primitive parameters without defaults")
		}
		result.Params = append(result.Params, kind)
		result.Locals = append(result.Locals, kind)
	}
	return result, nil
}

func declaredKind(expr ast.TypeExpr) ir.Kind {
	named, ok := expr.(*ast.NamedTypeExpr)
	if !ok || len(named.TypeArgs) != 0 {
		return 0
	}
	switch named.Name {
	case "Void":
		return ir.Void
	case "String":
		return ir.String
	case "Int":
		return ir.Int
	case "Bool":
		return ir.Bool
	default:
		return 0
	}
}

func valueKind(typ types.Type) ir.Kind {
	switch typ {
	case types.Void:
		return ir.Void
	case types.String:
		return ir.String
	case types.Int:
		return ir.Int
	case types.Bool:
		return ir.Bool
	default:
		return 0
	}
}

type functionBuilder struct {
	module    *typechecker.TypedFile
	targets   map[*ast.FunctionDecl]int
	functions []ir.Function
	fn        *ir.Function
	slots     map[typechecker.LocalBinding]int
	current   int
	nextValue int
}

func (b *functionBuilder) addBlock() int {
	b.fn.Blocks = append(b.fn.Blocks, ir.Block{})
	return len(b.fn.Blocks) - 1
}

func (b *functionBuilder) terminate(end ir.Terminator) {
	b.fn.Blocks[b.current].End = end
	b.current = -1
}

func (b *functionBuilder) emit(op ir.Instruction) int {
	op.Dest = -1
	if op.Type != ir.Void {
		op.Dest = b.nextValue
		b.nextValue++
	}
	block := &b.fn.Blocks[b.current]
	block.Ops = append(block.Ops, op)
	return op.Dest
}

func (b *functionBuilder) lowerBlock(block *ast.BlockStmt) error {
	for _, stmt := range block.Stmts {
		if err := b.statement(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (b *functionBuilder) statement(stmt ast.Stmt) error {
	if b.current < 0 {
		return unsupported(stmt.Pos(), "VM-002 does not lower statements after termination")
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		return b.lowerBlock(s)
	case *ast.LetStmt:
		value, err := b.value(s.Value)
		if err != nil {
			return err
		}
		kind := valueKind(b.module.Types[s.Value])
		if s.Type != nil && declaredKind(s.Type) != kind {
			return unsupported(s.Pos(), "local binding requires the exact declared type")
		}
		slot := len(b.fn.Locals)
		b.fn.Locals = append(b.fn.Locals, kind)
		b.slots[typechecker.LocalBinding{Let: s}] = slot
		b.emit(ir.Instruction{Op: ir.Store, Type: ir.Void, Index: slot, Args: []int{value}, Source: s.Pos()})
	case *ast.ReturnStmt:
		value := -1
		if s.Value != nil {
			var err error
			value, err = b.value(s.Value)
			if err != nil {
				return err
			}
		}
		b.terminate(ir.Terminator{Op: ir.Return, Value: value, Source: s.Pos()})
	case *ast.ExprStmt:
		call, ok := s.Expr.(*ast.CallExpr)
		if !ok {
			return unsupported(s.Pos(), "VM-002 supports only call expressions as statements")
		}
		_, err := b.expression(call)
		return err
	case *ast.IfStmt:
		return b.conditional(s)
	default:
		return unsupported(stmt.Pos(), "statement is outside the VM-002 subset")
	}
	return nil
}

func (b *functionBuilder) conditional(stmt *ast.IfStmt) error {
	test, err := b.value(stmt.Cond)
	if err != nil {
		return err
	}
	thenBlock, elseBlock := b.addBlock(), b.addBlock()
	b.terminate(ir.Terminator{Op: ir.Branch, Value: test, Target: thenBlock, Else: elseBlock, Source: stmt.Pos()})
	b.current = thenBlock
	if err := b.lowerBlock(stmt.Then); err != nil {
		return err
	}
	thenEnd := b.current
	b.current = elseBlock
	if stmt.Else != nil {
		if err := b.statement(stmt.Else); err != nil {
			return err
		}
	}
	elseEnd := b.current
	if thenEnd < 0 && elseEnd < 0 {
		return nil
	}
	join := b.addBlock()
	for _, predecessor := range []int{thenEnd, elseEnd} {
		if predecessor >= 0 {
			b.fn.Blocks[predecessor].End = ir.Terminator{Op: ir.Jump, Target: join, Source: stmt.Pos()}
		}
	}
	b.current = join
	return nil
}

func (b *functionBuilder) value(expr ast.Expr) (int, error) {
	kind := valueKind(b.module.Types[expr])
	if kind == ir.Void || kind == 0 {
		return -1, unsupported(expr.Pos(), "VM requires a known primitive value type")
	}
	return b.expression(expr)
}

func (b *functionBuilder) expression(expr ast.Expr) (int, error) {
	op := ir.Instruction{Type: valueKind(b.module.Types[expr]), Source: expr.Pos()}
	if op.Type == 0 {
		return -1, unsupported(expr.Pos(), "expression has no supported resolved type")
	}
	switch e := expr.(type) {
	case *ast.IntLiteral:
		op.Op, op.Int = ir.Const, e.Value
	case *ast.BoolLiteral:
		op.Op, op.Bool = ir.Const, e.Value
	case *ast.StringLiteral:
		op.Op, op.Text = ir.Const, e.Value
	case *ast.Ident:
		slot, ok := b.slots[b.module.LocalReferences[e]]
		if !ok {
			return -1, unsupported(e.Pos(), "only bound local values are supported as identifiers")
		}
		op.Op, op.Index = ir.Load, slot
	case *ast.UnaryExpr:
		var ok bool
		op.Op, ok = unaryOps[e.Op]
		if !ok {
			return -1, unsupported(e.Pos(), "unary operation is outside the VM-002 subset")
		}
		arg, err := b.value(e.Operand)
		if err != nil {
			return -1, err
		}
		op.Args = []int{arg}
	case *ast.BinaryExpr:
		var ok bool
		op.Op, ok = binaryOps[e.Op]
		if !ok {
			return -1, unsupported(e.Pos(), "binary operation is outside the VM-002 subset")
		}
		left, err := b.value(e.Left)
		if err != nil {
			return -1, err
		}
		right, err := b.value(e.Right)
		if err != nil {
			return -1, err
		}
		op.Args = []int{left, right}
	case *ast.CallExpr:
		return b.call(e, op)
	default:
		return -1, unsupported(expr.Pos(), "expression is outside the VM-002 subset")
	}
	return b.emit(op), nil
}

var unaryOps = map[token.Kind]ir.Op{token.MINUS: ir.Neg, token.BANG: ir.Not}

var binaryOps = map[token.Kind]ir.Op{
	token.PLUS: ir.Add, token.MINUS: ir.Sub, token.STAR: ir.Mul, token.SLASH: ir.Div,
	token.PERCENT: ir.Mod, token.EQ: ir.Eq, token.NEQ: ir.Ne,
	token.LT: ir.Lt, token.LTE: ir.Le, token.GT: ir.Gt, token.GTE: ir.Ge,
}

func (b *functionBuilder) call(call *ast.CallExpr, op ir.Instruction) (int, error) {
	if len(call.TypeArgs) != 0 {
		return -1, unsupported(call.Pos(), "VM-002 does not support explicit type arguments")
	}
	if b.module.PrintlnCalls[call] {
		if len(call.Args) != 1 || op.Type != ir.Void {
			return -1, unsupported(call.Pos(), "VM requires builtin println with one argument")
		}
		op.Op = ir.Print
	} else {
		id, ok := b.targets[b.module.FunctionReferences[call.Callee]]
		if !ok || op.Type != b.functions[id].Result {
			return -1, unsupported(call.Pos(), "VM requires a resolved call to an admitted named function")
		}
		op.Op, op.Index = ir.Call, id
	}
	for _, arg := range call.Args {
		value, err := b.value(arg)
		if err != nil {
			return -1, err
		}
		op.Args = append(op.Args, value)
	}
	return b.emit(op), nil
}
