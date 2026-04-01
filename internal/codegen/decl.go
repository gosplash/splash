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
	sig := e.funcSignature(decl)
	e.writef("%s {\n", sig)
	e.indent++
	e.emitBlock(decl.Body)
	e.indent--
	e.writeLine("}\n")
}

func (e *Emitter) funcSignature(decl *ast.FunctionDecl) string {
	var params []string
	for _, p := range decl.Params {
		params = append(params, fmt.Sprintf("%s %s", p.Name, e.emitTypeName(p.Type)))
	}
	ret := e.emitTypeName(decl.ReturnType)
	sig := fmt.Sprintf("func %s(%s)", decl.Name, strings.Join(params, ", "))
	if ret != "" {
		sig += " " + ret
	}
	return sig
}

// emitBlock is defined in stmt.go (Task 3). Stub here so decl.go compiles.
func (e *Emitter) emitBlock(b *ast.BlockStmt) {}
