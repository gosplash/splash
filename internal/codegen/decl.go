package codegen

import (
	"fmt"
	"strings"

	"gosplash.dev/splash/internal/ast"
)

func (e *Emitter) emitDecl(d ast.Decl) {
	switch decl := d.(type) {
	case *ast.TypeDecl:
		e.emitTypeDecl(decl)
	case *ast.EnumDecl:
		e.emitEnumDecl(decl)
	case *ast.FunctionDecl:
		e.emitFunctionDecl(decl)
	// ModuleDecl, UseDecl, ConstraintDecl: skip in v0.1
	}
}

func (e *Emitter) emitTypeDecl(decl *ast.TypeDecl) {
	e.writef("type %s struct {\n", decl.Name)
	e.indent++
	for _, field := range decl.Fields {
		e.writeLine("%s %s", field.Name, e.emitTypeName(field.Type))
	}
	e.indent--
	e.writeLine("}\n")
}

func (e *Emitter) emitEnumDecl(decl *ast.EnumDecl) {
	// Marker interface: type Color interface{ colorVariant() }
	marker := strings.ToLower(decl.Name[:1]) + decl.Name[1:] + "Variant"
	e.writef("type %s interface{ %s() }\n\n", decl.Name, marker)
	// Variant structs
	for _, v := range decl.Variants {
		typeName := decl.Name + v.Name
		if v.Payload != nil {
			e.writef("type %s struct{ Value %s }\n", typeName, e.emitTypeName(v.Payload))
		} else {
			e.writef("type %s struct{}\n", typeName)
		}
		e.writef("func (%s) %s() {}\n\n", typeName, marker)
	}
}

func (e *Emitter) emitFunctionDecl(decl *ast.FunctionDecl) {
	isApprove := e.approveFns[decl.Name]
	isCaller := e.approveCallers[decl.Name]
	isMain := decl.Name == "main"

	// Set per-function emission state for stmt emitters
	e.inApprovalFn = (isApprove || isCaller) && !isMain
	e.currentFnReturnType = decl.ReturnType
	e.currentFnIsMain = isMain

	sig := e.funcSignature(decl)
	e.writef("%s {\n", sig)
	e.indent++

	if isApprove {
		// Inject approval gate: if denied, return before body runs.
		e.needsApproval = true
		if decl.ReturnType != nil {
			e.writeLine("if err := splashApprove(%q); err != nil {", decl.Name)
			e.indent++
			e.writeLine("return %s, err", e.zeroValueFor(decl.ReturnType))
			e.indent--
			e.writeLine("}")
		} else {
			e.writeLine("if err := splashApprove(%q); err != nil {", decl.Name)
			e.indent++
			e.writeLine("return err")
			e.indent--
			e.writeLine("}")
		}
	}

	e.emitBlock(decl.Body)

	// Void approval functions need an explicit return nil — Go requires it.
	// Non-void functions have explicit Splash returns rewritten to "return x, nil" by emitReturnStmt.
	if e.inApprovalFn && decl.ReturnType == nil {
		e.writeLine("return nil")
	}

	e.indent--
	e.writeLine("}\n")

	// Clear per-function state
	e.inApprovalFn = false
	e.currentFnReturnType = nil
	e.currentFnIsMain = false
}

func (e *Emitter) funcSignature(decl *ast.FunctionDecl) string {
	var params []string
	for _, p := range decl.Params {
		params = append(params, fmt.Sprintf("%s %s", p.Name, e.emitTypeName(p.Type)))
	}

	isApprovalFn := e.approveFns[decl.Name] || e.approveCallers[decl.Name]
	isMain := decl.Name == "main"

	ret := e.emitTypeName(decl.ReturnType)
	sig := fmt.Sprintf("func %s(%s)", decl.Name, strings.Join(params, ", "))

	switch {
	case isApprovalFn && !isMain && ret != "":
		sig += fmt.Sprintf(" (%s, error)", ret)
	case isApprovalFn && !isMain && ret == "":
		sig += " error"
	case ret != "":
		sig += " " + ret
	}
	return sig
}
