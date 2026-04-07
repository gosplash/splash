// Package codegen emits Go source from a Splash AST.
// The Backend interface is the stable surface for driver code; the GoBackend
// implementation wraps the Emitter. Future backends (LLVM, C) satisfy the
// same interface without touching the CLI.
package codegen

import (
	"fmt"
	"sort"
	"strings"

	"gosplash.dev/splash/internal/ast"
)

// Options carries per-compilation settings to a Backend.
// Adding a new option here is the only change required when the compiler
// gains a new capability that affects code generation.
type Options struct {
	// ApprovalCallers is the set of functions that transitively call any
	// approval-gated function. The backend widens their return signatures to (T, error).
	ApprovalCallers map[string]bool
}

// Backend is the interface a code generation backend must satisfy.
// The CLI and driver code depend only on this interface — swapping the Go
// backend for an LLVM or C backend requires no changes outside this package.
type Backend interface {
	Emit(f *ast.File, opts Options) string
}

// NewGoBackend returns the default Go source backend.
func NewGoBackend() Backend { return &goBackend{} }

type goBackend struct{}

func (b *goBackend) Emit(f *ast.File, opts Options) string {
	e := New()
	e.SetApprovalCallers(opts.ApprovalCallers)
	return e.EmitFile(f)
}

// Emitter accumulates Go source for a single Splash file.
type Emitter struct {
	body             strings.Builder
	imports          map[string]bool
	indent           int
	needsCoalesce    bool
	needsApproval    bool
	moduleNamespaces map[string]bool

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
		e.fnDecls[fn.Name] = fn
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				e.approveFns[fn.Name] = true
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
