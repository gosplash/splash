// Package codegen emits Go source from a Splash AST.
// The Backend interface is the stable surface for driver code; the GoBackend
// implementation wraps the Emitter. Future backends (LLVM, C) satisfy the
// same interface without touching the CLI.
package codegen

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/token"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

// Options carries per-compilation settings to a Backend.
// Adding a new option here is the only change required when the compiler
// gains a new capability that affects code generation.
type Options struct {
	// ApprovalCallers is the set of functions that transitively call any
	// approval-gated function. The backend widens their return signatures to (T, error).
	ApprovalCallers map[string]bool
	// FunctionKey and FunctionReferences are produced by analyzed compilation.
	FunctionKey        func(*ast.FunctionDecl) string
	FunctionReferences map[ast.Expr]*ast.FunctionDecl
	PrintlnCalls       map[*ast.CallExpr]bool
	LocalNames         map[string]bool
	Entry              *ast.File
}

// Backend emits source text for the existing Go build path.
type Backend interface {
	Emit(f *ast.File, opts Options) string
}

// NewGoBackend returns the default Go source backend.
func NewGoBackend() Backend { return &goBackend{} }

type goBackend struct{}

func (b *goBackend) Emit(f *ast.File, opts Options) string {
	e := New()
	e.SetApprovalCallers(opts.ApprovalCallers)
	e.declarationKey = opts.FunctionKey
	e.functionReferences = opts.FunctionReferences
	e.printlnCalls = opts.PrintlnCalls
	if opts.FunctionKey != nil {
		e.prepareFunctionNames(f, opts.Entry, opts.LocalNames)
	}
	return e.EmitFile(f)
}

// Emitter accumulates Go source for a single Splash file.
type Emitter struct {
	body               strings.Builder
	imports            map[string]bool
	indent             int
	needsCoalesce      bool
	needsApproval      bool
	moduleNamespaces   map[string]bool
	declarationKey     func(*ast.FunctionDecl) string
	functionNames      map[*ast.FunctionDecl]string
	functionReferences map[ast.Expr]*ast.FunctionDecl
	printlnCalls       map[*ast.CallExpr]bool
	mainFunction       *ast.FunctionDecl

	// Phase 4b: approval cascade
	approveFns     map[string]bool              // approval-gated function names (built in EmitFile pre-pass)
	approveCallers map[string]bool              // transitive callers of approveFns (set externally via SetApprovalCallers)
	fnDecls        map[string]*ast.FunctionDecl // all function declarations (for return-type lookups at call sites)

	// Per-function emission state (set in emitFunctionDecl, cleared after)
	inApprovalFn        bool
	currentFnReturnType ast.TypeExpr
	currentFnIsMain     bool
}

// New creates a ready-to-use Emitter.
func New() *Emitter {
	return &Emitter{
		imports:          make(map[string]bool),
		moduleNamespaces: make(map[string]bool),
		approveFns:       make(map[string]bool),
		approveCallers:   make(map[string]bool),
		fnDecls:          make(map[string]*ast.FunctionDecl),
	}
}

// SetApprovalCallers provides the set of functions that transitively call any
// approval-gated function. Used by the CLI driver after callgraph analysis.
// The emitter always builds approveFns itself from AST annotations in EmitFile.
func (e *Emitter) SetApprovalCallers(approveCallers map[string]bool) {
	e.approveCallers = approveCallers
}

// prepareFunctionNames keeps unique source names where their Go lookup cannot
// be shadowed. The merged Go package needs distinct names for module functions
// with the same spelling.
func (e *Emitter) prepareFunctionNames(f, entry *ast.File, localNames map[string]bool) {
	e.functionNames = make(map[*ast.FunctionDecl]string)
	reserved := map[string]bool{"main": true}
	counts := make(map[string]int)
	for _, decl := range f.Declarations {
		switch d := decl.(type) {
		case *ast.FunctionDecl:
			counts[d.Name]++
			reserved[d.Name] = true
		case *ast.TypeDecl:
			counts[d.Name]++
			reserved[d.Name] = true
		case *ast.EnumDecl:
			counts[d.Name]++
			reserved[d.Name] = true
			for _, variant := range d.Variants {
				name := d.Name + variant.Name
				counts[name]++
				reserved[name] = true
			}
		}
	}
	if entry != nil {
		for _, decl := range entry.Declarations {
			if fn, ok := decl.(*ast.FunctionDecl); ok && fn.Name == "main" {
				e.mainFunction = fn
				break
			}
		}
	}
	n := 0
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		name := fn.Name
		collision := name == "main" || counts[name] > 1 || localNames[name]
		if fn == e.mainFunction {
			name = "run"
			collision = counts[name] > 0 || localNames[name]
		}
		if collision {
			for {
				n++
				name = fmt.Sprintf("_splash_fn_%d", n)
				if !reserved[name] && !localNames[name] {
					break
				}
			}
		}
		reserved[name] = true
		e.functionNames[fn] = name
	}
}

func (e *Emitter) functionKey(fn *ast.FunctionDecl) string {
	if e.declarationKey != nil {
		return e.declarationKey(fn)
	}
	return fn.Name
}

func (e *Emitter) functionName(fn *ast.FunctionDecl) string {
	if name, ok := e.functionNames[fn]; ok {
		return name
	}
	if fn.Name == "main" {
		return "run"
	}
	return fn.Name
}

func (e *Emitter) isMain(fn *ast.FunctionDecl) bool {
	if e.declarationKey != nil {
		return fn == e.mainFunction
	}
	return fn.Name == "main"
}

func (e *Emitter) approvalCallee(call *ast.CallExpr) *ast.FunctionDecl {
	fn := e.functionReferences[call.Callee]
	if e.functionReferences == nil {
		if ident, ok := call.Callee.(*ast.Ident); ok {
			fn = e.fnDecls[ident.Name]
		}
	}
	if fn != nil {
		key := e.functionKey(fn)
		if e.approveFns[key] || e.approveCallers[key] {
			return fn
		}
	}
	return nil
}

// EmitFile generates a complete Go source file from a Splash AST.
func (e *Emitter) EmitFile(f *ast.File) string {
	for _, u := range f.Uses {
		ns := u.Alias
		if ns == "" {
			parts := strings.Split(u.Path, "/")
			ns = parts[len(parts)-1]
		}
		e.moduleNamespaces[ns] = true
	}
	// Pre-pass: index all function declarations and collect approval-gated names.
	// approveFns is always built from AST annotations regardless of external input.
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		key := e.functionKey(fn)
		e.fnDecls[key] = fn
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				e.approveFns[key] = true
			}
		}
	}

	// Emit declarations into body buffer
	for _, decl := range f.Declarations {
		e.emitDecl(decl)
	}

	// Helpers need imports — add after body emission
	if e.needsApproval {
		e.imports["bufio"] = true
		e.imports["fmt"] = true
		e.imports["os"] = true
		e.imports["strings"] = true
	}

	// Assemble: package + imports + helpers + body
	var out strings.Builder
	pkgName := "main"
	if f.Module != nil {
		pkgName = f.Module.Name
	}
	fmt.Fprintf(&out, "package %s\n\n", pkgName)

	if len(e.imports) > 0 {
		out.WriteString("import (\n")
		importList := make([]string, 0, len(e.imports))
		for imp := range e.imports {
			importList = append(importList, imp)
		}
		sort.Strings(importList)
		for _, imp := range importList {
			fmt.Fprintf(&out, "\t%q\n", imp)
		}
		out.WriteString(")\n\n")
	}

	if e.needsCoalesce {
		out.WriteString(splashCoalesceHelper)
		out.WriteString("\n")
	}
	if e.needsApproval {
		out.WriteString(splashApprovalHelper)
		out.WriteString("\n")
	}

	out.WriteString(e.body.String())
	return out.String()
}

// EmitTypeName returns the Go type string for a Splash TypeExpr.
// Exported for testing.
func (e *Emitter) EmitTypeName(t ast.TypeExpr) string {
	return e.emitTypeName(t)
}

func (e *Emitter) emitTypeName(t ast.TypeExpr) string {
	if t == nil {
		return ""
	}
	switch typ := t.(type) {
	case *ast.NamedTypeExpr:
		name := stripModuleQualifier(typ.Name)
		switch name {
		case "String":
			return "string"
		case "Int":
			return "int"
		case "Float":
			return "float64"
		case "Bool":
			return "bool"
		case "List":
			if len(typ.TypeArgs) == 1 {
				return "[]" + e.emitTypeName(typ.TypeArgs[0])
			}
			return "[]any"
		default:
			return name
		}
	case *ast.OptionalTypeExpr:
		return "*" + e.emitTypeName(typ.Inner)
	case *ast.FnTypeExpr:
		var params []string
		for _, p := range typ.Params {
			params = append(params, e.emitTypeName(p))
		}
		ret := e.emitTypeName(typ.ReturnType)
		return fmt.Sprintf("func(%s) %s", strings.Join(params, ", "), ret)
	}
	return "any"
}

// --- write helpers ---

func (e *Emitter) writef(format string, args ...any) {
	fmt.Fprintf(&e.body, format, args...)
}

func (e *Emitter) writeLine(format string, args ...any) {
	fmt.Fprintf(&e.body, strings.Repeat("\t", e.indent)+format+"\n", args...)
}

func stripModuleQualifier(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// --- runtime helper sources ---

const splashCoalesceHelper = `func splashCoalesce[T any](val *T, fallback T) T {
	if val == nil {
		return fallback
	}
	return *val
}
`

const splashApprovalHelper = `// ApprovalAdapter is the interface for approve fn gate implementations.
// Request blocks until the named function is approved.
// Return nil to approve; return an error to deny.
// StdinApproval (the default) loops until the operator types y — it never returns an error.
// Production adapters (SlackApproval, WebhookApproval) return a non-nil error on denial,
// which propagates up the call stack without killing the process.
type ApprovalAdapter interface {
	Request(name string) error
}

var _splashApproval ApprovalAdapter = &splashStdinApproval{}

// SetApprovalAdapter replaces the package-level approval adapter.
// Call this in tests or in production main() before any approval-gated function runs.
func SetApprovalAdapter(a ApprovalAdapter) { _splashApproval = a }

type splashStdinApproval struct{}

func (*splashStdinApproval) Request(name string) error {
	for {
		fmt.Fprintf(os.Stderr, "[approve] %s — approve? (y/N): ", name)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) == "y" {
			return nil
		}
		fmt.Fprintf(os.Stderr, "Not approved. Try again or Ctrl+C to abort.\n")
	}
}

func splashApprove(name string) error {
	return _splashApproval.Request(name)
}
`

// VMErrorCode distinguishes admission, validation and execution failures.
type VMErrorCode string

const (
	VMUnsupported      VMErrorCode = "SPLASH_VM_UNSUPPORTED_SOURCE"
	VMInvalidIR        VMErrorCode = "SPLASH_VM_INVALID_IR"
	VMInvalidBytecode  VMErrorCode = "SPLASH_VM_INVALID_BYTECODE"
	VMInstructionLimit VMErrorCode = "SPLASH_VM_INSTRUCTION_LIMIT"
	VMFrameLimit       VMErrorCode = "SPLASH_VM_FRAME_LIMIT"
	VMOutputError      VMErrorCode = "SPLASH_VM_OUTPUT_ERROR"
)

// VMError carries source provenance when available. Execution failures also
// report the number of dispatches and the frame depth at the failure.
type VMError struct {
	Code             VMErrorCode
	Source           token.Position
	InstructionCount uint64
	FrameDepth       int
	Detail           string
}

func (e *VMError) Error() string {
	if e.Source.File == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Source, e.Code, e.Detail)
}

// VMLimits bounds execution. A zero limit grants no instructions or frames.
type VMLimits struct {
	Instructions uint64
	Frames       int
}

// RunVM executes the experimental subset of a checked source snapshot. The
// admitted entry is one unannotated main function containing literal prints.
func RunVM(modules []*typechecker.TypedFile, output io.Writer, limits VMLimits) error {
	ir, err := lowerVMSource(modules)
	if err != nil {
		return err
	}
	program, err := lowerVMBytecode(ir)
	if err != nil {
		return err
	}
	return executeVMBytecode(program, output, limits)
}

type vmType uint8

const (
	vmVoid vmType = iota + 1
	vmString
)

type vmIROpKind uint8

const (
	vmIRStringConst vmIROpKind = iota + 1
	vmIRPrint
)

type vmIROp struct {
	kind   vmIROpKind
	typ    vmType
	value  int // destination for constants and input for print
	text   string
	source token.Position
}

type vmIRTerminator uint8

const vmIRReturn vmIRTerminator = 1

// VM-001 admits one function and one basic block. The terminator remains
// explicit even though branching is outside the subset.
type vmIRBlock struct {
	ops        []vmIROp
	terminator vmIRTerminator
	source     token.Position
}

type vmIR struct {
	result vmType
	entry  vmIRBlock
}

func lowerVMSource(modules []*typechecker.TypedFile) (vmIR, error) {
	if len(modules) == 0 || modules[len(modules)-1] == nil || modules[len(modules)-1].File == nil {
		return vmIR{}, &VMError{Code: VMUnsupported, Detail: "VM-001 requires a checked entry module"}
	}
	module := modules[len(modules)-1]
	file := module.File
	if len(file.Uses) != 0 {
		return vmIR{}, &VMError{Code: VMUnsupported, Source: file.Uses[0].Pos(), Detail: "VM-001 does not support imports"}
	}
	pos := file.Pos()
	if file.Module != nil {
		pos = file.Module.Pos()
	} else if len(file.Declarations) != 0 {
		pos = file.Declarations[0].Pos()
	}
	if len(modules) != 1 || file.Module == nil || len(file.Module.Annotations) != 0 ||
		len(file.Exposes) != 0 || len(file.Declarations) != 1 {
		return vmIR{}, &VMError{Code: VMUnsupported, Source: pos, Detail: "VM-001 requires one unannotated module with only fn main()"}
	}
	main, ok := file.Declarations[0].(*ast.FunctionDecl)
	if !ok || main.Name != "main" || len(main.Params) != 0 || main.ReturnType != nil ||
		len(main.TypeParams) != 0 || len(main.Effects) != 0 || len(main.Annotations) != 0 || main.IsAsync || main.Body == nil {
		return vmIR{}, &VMError{Code: VMUnsupported, Source: file.Declarations[0].Pos(), Detail: "VM-001 requires an unannotated fn main() with an implicit Void return"}
	}

	result := vmIR{result: vmVoid, entry: vmIRBlock{terminator: vmIRReturn, source: main.Pos()}}
	for index, stmt := range main.Body.Stmts {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			return vmIR{}, &VMError{Code: VMUnsupported, Source: stmt.Pos(), Detail: "VM-001 supports only println statements"}
		}
		call, ok := expr.Expr.(*ast.CallExpr)
		if !ok || !module.PrintlnCalls[call] || len(call.Args) != 1 || len(call.TypeArgs) != 0 {
			return vmIR{}, &VMError{Code: VMUnsupported, Source: stmt.Pos(), Detail: "VM-001 requires a resolved builtin println with one argument"}
		}
		literal, ok := call.Args[0].(*ast.StringLiteral)
		if !ok || module.Types[literal] != types.String || module.Types[call] != types.Void {
			return vmIR{}, &VMError{Code: VMUnsupported, Source: call.Args[0].Pos(), Detail: "VM-001 requires a typed String literal and a Void println call"}
		}
		result.entry.ops = append(result.entry.ops,
			vmIROp{kind: vmIRStringConst, typ: vmString, value: index, text: literal.Value, source: literal.Pos()},
			vmIROp{kind: vmIRPrint, typ: vmString, value: index, source: stmt.Pos()},
		)
	}
	return result, nil
}

func validateVMIR(ir vmIR) error {
	if ir.result != vmVoid || ir.entry.terminator != vmIRReturn {
		return &VMError{Code: VMInvalidIR, Source: ir.entry.source, Detail: "entry must terminate with a Void return"}
	}
	values := make(map[int]vmType)
	for _, op := range ir.entry.ops {
		switch op.kind {
		case vmIRStringConst:
			if op.value < 0 || op.typ != vmString || values[op.value] != 0 {
				return &VMError{Code: VMInvalidIR, Source: op.source, Detail: "invalid or duplicate String constant definition"}
			}
			values[op.value] = op.typ
		case vmIRPrint:
			if op.typ != vmString || values[op.value] != vmString || op.text != "" {
				return &VMError{Code: VMInvalidIR, Source: op.source, Detail: "print requires a defined String value"}
			}
		default:
			return &VMError{Code: VMInvalidIR, Source: op.source, Detail: "unknown IR operation"}
		}
	}
	return nil
}

type vmOpcode uint8

const (
	vmConstString vmOpcode = iota + 1
	vmPrint
	vmReturn
)

type vmInstruction struct {
	op      vmOpcode
	operand int
	source  token.Position
}

type vmBytecode struct {
	constants []string
	code      []vmInstruction
}

func lowerVMBytecode(ir vmIR) (vmBytecode, error) {
	if err := validateVMIR(ir); err != nil {
		return vmBytecode{}, err
	}
	var result vmBytecode
	type constantLocation struct {
		index  int
		source token.Position
	}
	constants := make(map[int]constantLocation)
	for _, op := range ir.entry.ops {
		switch op.kind {
		case vmIRStringConst:
			constants[op.value] = constantLocation{index: len(result.constants), source: op.source}
			result.constants = append(result.constants, op.text)
		case vmIRPrint:
			constant := constants[op.value]
			result.code = append(result.code,
				vmInstruction{op: vmConstString, operand: constant.index, source: constant.source},
				vmInstruction{op: vmPrint, source: op.source},
			)
		}
	}
	result.code = append(result.code, vmInstruction{op: vmReturn, source: ir.entry.source})
	return result, nil
}

func validateVMBytecode(program vmBytecode) error {
	// String is the only operand type in VM-001.
	depth := 0
	for pc, instruction := range program.code {
		switch instruction.op {
		case vmConstString:
			if instruction.operand < 0 || instruction.operand >= len(program.constants) {
				return &VMError{Code: VMInvalidBytecode, Source: instruction.source, Detail: "String constant index out of range"}
			}
			depth++
		case vmPrint:
			if depth == 0 || instruction.operand != 0 {
				return &VMError{Code: VMInvalidBytecode, Source: instruction.source, Detail: "print needs a String on the stack and has no immediate operand"}
			}
			depth--
		case vmReturn:
			if depth != 0 || instruction.operand != 0 || pc != len(program.code)-1 {
				return &VMError{Code: VMInvalidBytecode, Source: instruction.source, Detail: "return must terminate the entry with an empty stack"}
			}
			return nil
		default:
			return &VMError{Code: VMInvalidBytecode, Source: instruction.source, Detail: "unknown bytecode opcode"}
		}
	}
	return &VMError{Code: VMInvalidBytecode, Detail: "bytecode entry is missing a return"}
}

func executeVMBytecode(program vmBytecode, output io.Writer, limits VMLimits) error {
	if err := validateVMBytecode(program); err != nil {
		return err
	}
	if limits.Frames < 1 {
		return &VMError{Code: VMFrameLimit, Source: program.code[0].source, Detail: "entry frame exceeds budget"}
	}

	var stack []string
	for pc, instruction := range program.code {
		if uint64(pc) >= limits.Instructions {
			return &VMError{Code: VMInstructionLimit, Source: instruction.source, InstructionCount: uint64(pc), FrameDepth: 1, Detail: "instruction budget exhausted"}
		}
		switch instruction.op {
		case vmConstString:
			stack = append(stack, program.constants[instruction.operand])
		case vmPrint:
			if output == nil {
				return &VMError{Code: VMOutputError, Source: instruction.source, InstructionCount: uint64(pc) + 1, FrameDepth: 1, Detail: "output hook is missing"}
			}
			text := stack[len(stack)-1] + "\n"
			stack = stack[:len(stack)-1]
			if _, err := io.WriteString(output, text); err != nil {
				return &VMError{Code: VMOutputError, Source: instruction.source, InstructionCount: uint64(pc) + 1, FrameDepth: 1, Detail: err.Error()}
			}
		case vmReturn:
			return nil
		}
	}
	panic("validated VM bytecode did not terminate")
}
