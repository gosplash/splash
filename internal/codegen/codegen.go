// Package codegen emits Go source from a Splash AST.
package codegen

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
)

// Emitter accumulates Go source for a single Splash file.
type Emitter struct {
	body          strings.Builder
	imports       map[string]bool
	indent        int
	needsCoalesce bool
	needsAudit    bool
	approvedFns   map[string]bool
}

// New creates a ready-to-use Emitter.
func New() *Emitter {
	return &Emitter{
		imports:     make(map[string]bool),
		approvedFns: make(map[string]bool),
	}
}

// EmitFile generates a complete Go source file from a Splash AST.
func (e *Emitter) EmitFile(f *ast.File) string {
	// Pass 1: collect @approve function names
	for _, decl := range f.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		for _, ann := range fn.Annotations {
			if ann.Kind == ast.AnnotApprove {
				e.approvedFns[fn.Name] = true
			}
		}
	}

	// Pass 2: emit declarations into body buffer
	for _, decl := range f.Declarations {
		e.emitDecl(decl)
	}

	// Helpers need imports — add them after body emission
	if e.needsAudit {
		e.imports["encoding/json"] = true
		e.imports["os"] = true
		e.imports["time"] = true
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
		for imp := range e.imports {
			fmt.Fprintf(&out, "\t%q\n", imp)
		}
		out.WriteString(")\n\n")
	}

	if e.needsCoalesce {
		out.WriteString(splashCoalesceHelper)
		out.WriteString("\n")
	}
	if e.needsAudit {
		out.WriteString(splashAuditHelper)
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
		switch typ.Name {
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
			return typ.Name
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

// --- runtime helper sources ---

const splashCoalesceHelper = `func splashCoalesce[T any](val *T, fallback T) T {
	if val == nil {
		return fallback
	}
	return *val
}
`

const splashAuditHelper = `func splashAudit(fn string, ts time.Time) {
	enc := json.NewEncoder(os.Stdout)
	enc.Encode(map[string]any{"fn": fn, "ts": ts.Format(time.RFC3339)})
}
`

