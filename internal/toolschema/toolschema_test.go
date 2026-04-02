package toolschema_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
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

// parseFile helper for Extract tests
func parseFile(src string) *ast.File {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	return file
}

// Extract tests

func TestExtract_NoTools(t *testing.T) {
	file := parseFile(`
module foo
fn helper() -> String { return "hi" }
`)
	schemas := toolschema.Extract(file)
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas, got %d", len(schemas))
	}
}

func TestExtract_SingleTool_Name(t *testing.T) {
	file := parseFile(`
module foo
@tool
fn search(query: String) -> String { return query }
`)
	schemas := toolschema.Extract(file)
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if schemas[0].Name != "search" {
		t.Errorf("expected name=%q, got %q", "search", schemas[0].Name)
	}
}

func TestExtract_RequiredParams(t *testing.T) {
	file := parseFile(`
module foo
@tool
fn search(query: String, limit: Int) -> String { return query }
`)
	schemas := toolschema.Extract(file)
	required := schemas[0].InputSchema.Required
	if len(required) != 2 {
		t.Fatalf("expected 2 required params, got %v", required)
	}
}

func TestExtract_OptionalParamNotRequired(t *testing.T) {
	file := parseFile(`
module foo
@tool
fn search(query: String, category: String?) -> String { return query }
`)
	schemas := toolschema.Extract(file)
	required := schemas[0].InputSchema.Required
	for _, r := range required {
		if r == "category" {
			t.Error("optional param 'category' should not be in required[]")
		}
	}
	if _, ok := schemas[0].InputSchema.Properties["category"]; !ok {
		t.Error("optional param 'category' should still appear in properties")
	}
}

func TestExtract_DocComment_FunctionDescription(t *testing.T) {
	file := parseFile(`
module foo
/// Search the product catalog.
@tool
fn search(query: String) -> String { return query }
`)
	schemas := toolschema.Extract(file)
	if schemas[0].Description != "Search the product catalog." {
		t.Errorf("expected description %q, got %q",
			"Search the product catalog.", schemas[0].Description)
	}
}

func TestExtract_DocComment_ParamDescription(t *testing.T) {
	file := parseFile(`
module foo
@tool
fn search(
  /// The search query
  query: String,
) -> String { return query }
`)
	schemas := toolschema.Extract(file)
	prop := schemas[0].InputSchema.Properties["query"]
	if prop == nil {
		t.Fatal("expected property 'query'")
	}
	if prop.Description != "The search query" {
		t.Errorf("expected param description %q, got %q", "The search query", prop.Description)
	}
}

func TestExtract_MultipleTools(t *testing.T) {
	file := parseFile(`
module foo
@tool
fn search(query: String) -> String { return query }
@tool
fn lookup(id: Int) -> String { return "x" }
fn internal() -> String { return "hidden" }
`)
	schemas := toolschema.Extract(file)
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
}
