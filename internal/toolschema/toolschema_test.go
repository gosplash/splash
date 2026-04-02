package toolschema_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/toolschema"
)

// helpers

func namedType(name string, args ...ast.TypeExpr) *ast.NamedTypeExpr {
	return &ast.NamedTypeExpr{Name: name, TypeArgs: args}
}

func optionalType(inner ast.TypeExpr) *ast.OptionalTypeExpr {
	return &ast.OptionalTypeExpr{Inner: inner}
}

func listType(elem ast.TypeExpr) *ast.NamedTypeExpr {
	return &ast.NamedTypeExpr{Name: "List", TypeArgs: []ast.TypeExpr{elem}}
}

func emptyFile() *ast.File { return &ast.File{} }

func fileWithEnum(name string, variants ...string) *ast.File {
	var vs []ast.EnumVariant
	for _, v := range variants {
		vs = append(vs, ast.EnumVariant{Name: v})
	}
	return &ast.File{
		Declarations: []ast.Decl{
			&ast.EnumDecl{Name: name, Variants: vs},
		},
	}
}

// type mapping tests

func TestTypeMapper_String(t *testing.T) {
	prop := toolschema.TypeExprToSchema(namedType("String"), emptyFile())
	if prop.Type != "string" {
		t.Errorf("expected type=string, got %q", prop.Type)
	}
}

func TestTypeMapper_Int(t *testing.T) {
	prop := toolschema.TypeExprToSchema(namedType("Int"), emptyFile())
	if prop.Type != "integer" {
		t.Errorf("expected type=integer, got %q", prop.Type)
	}
}

func TestTypeMapper_Float(t *testing.T) {
	prop := toolschema.TypeExprToSchema(namedType("Float"), emptyFile())
	if prop.Type != "number" {
		t.Errorf("expected type=number, got %q", prop.Type)
	}
}

func TestTypeMapper_Bool(t *testing.T) {
	prop := toolschema.TypeExprToSchema(namedType("Bool"), emptyFile())
	if prop.Type != "boolean" {
		t.Errorf("expected type=boolean, got %q", prop.Type)
	}
}

func TestTypeMapper_Optional_IsInnerType(t *testing.T) {
	// String? → same schema as String (required handling is at param level)
	prop := toolschema.TypeExprToSchema(optionalType(namedType("String")), emptyFile())
	if prop.Type != "string" {
		t.Errorf("expected type=string for optional, got %q", prop.Type)
	}
}

func TestTypeMapper_ListOfInt(t *testing.T) {
	prop := toolschema.TypeExprToSchema(listType(namedType("Int")), emptyFile())
	if prop.Type != "array" {
		t.Errorf("expected type=array, got %q", prop.Type)
	}
	if prop.Items == nil {
		t.Fatal("expected items to be non-nil")
	}
	if prop.Items.Type != "integer" {
		t.Errorf("expected items.type=integer, got %q", prop.Items.Type)
	}
}

func TestTypeMapper_Enum(t *testing.T) {
	file := fileWithEnum("Color", "Red", "Green", "Blue")
	prop := toolschema.TypeExprToSchema(namedType("Color"), file)
	if prop.Type != "string" {
		t.Errorf("expected type=string for enum, got %q", prop.Type)
	}
	if len(prop.Enum) != 3 {
		t.Fatalf("expected 3 enum values, got %v", prop.Enum)
	}
}

func TestTypeMapper_UnknownNamedType_IsObject(t *testing.T) {
	prop := toolschema.TypeExprToSchema(namedType("SearchResult"), emptyFile())
	if prop.Type != "object" {
		t.Errorf("expected type=object for unknown named type, got %q", prop.Type)
	}
}
