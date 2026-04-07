package toolschema_test

import (
	"encoding/json"
	"slices"
	"strings"
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
tool fn search(query: String) -> String { return query }
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
tool fn search(query: String, limit: Int) -> String { return query }
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
tool fn search(query: String, category: String?) -> String { return query }
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
tool fn search(query: String) -> String { return query }
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
tool fn search(
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
tool fn search(query: String) -> String { return query }
tool fn lookup(id: Int) -> String { return "x" }
fn internal() -> String { return "hidden" }
`)
	schemas := toolschema.Extract(file)
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
}

func TestToolSchema_EffectsField(t *testing.T) {
	file := parseFile(`
module demo
/// Find records by query.
tool fn search(query: String) needs DB.read, Net -> String { return query }
`)
	tools := toolschema.Extract(file)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	if len(tool.Effects) != 2 {
		t.Fatalf("expected 2 effects, got %v", tool.Effects)
	}
	if !slices.Contains(tool.Effects, "DB.read") {
		t.Errorf("expected effects to contain %q, got %v", "DB.read", tool.Effects)
	}
	if !slices.Contains(tool.Effects, "Net") {
		t.Errorf("expected effects to contain %q, got %v", "Net", tool.Effects)
	}
}

func TestToolSchema_NoEffectsOmitted(t *testing.T) {
	file := parseFile(`
module demo
/// Simple tool with no effects.
tool fn ping() -> String { return "pong" }
`)
	tools := toolschema.Extract(file)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if len(tools[0].Effects) != 0 {
		t.Errorf("expected no effects, got %v", tools[0].Effects)
	}
	out, err := json.Marshal(tools[0])
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if strings.Contains(string(out), `"effects"`) {
		t.Errorf("expected 'effects' key to be absent from JSON, got %s", string(out))
	}
}

func TestExtractReachable_FiltersNonReachable(t *testing.T) {
	file := parseFile(`
module demo
tool fn reachable(query: String) -> String { return query }
tool fn restricted(id: Int) -> String { return "hidden" }
`)
	agentReachable := map[string]bool{
		"reachable": true,
	}
	tools := toolschema.ExtractReachable(file, agentReachable)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "reachable" {
		t.Errorf("expected tool name %q, got %q", "reachable", tools[0].Name)
	}
}

func TestToolSchema_DefaultParamNotRequired(t *testing.T) {
	src := `
module demo
/// Search with optional limit.
tool fn search(query: String, limit: Int = 10) -> String { return query }
`
	file := parseFile(src)
	tools := toolschema.Extract(file)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]

	// query is required, limit has a default so it is not
	if len(tool.InputSchema.Required) != 1 {
		t.Fatalf("expected 1 required param, got %v", tool.InputSchema.Required)
	}
	if tool.InputSchema.Required[0] != "query" {
		t.Errorf("expected required[0] = query, got %q", tool.InputSchema.Required[0])
	}

	// limit still appears in properties
	if _, ok := tool.InputSchema.Properties["limit"]; !ok {
		t.Error("expected limit to appear in properties")
	}
}

func TestToolSchema_OptionalAndDefaultBothNotRequired(t *testing.T) {
	src := `
module demo
tool fn search(
  /// Required query string.
  query:   String,
  limit:   Int      = 10,
  speaker: String?,
) -> String { return query }
`
	file := parseFile(src)
	tools := toolschema.Extract(file)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	required := tools[0].InputSchema.Required
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("expected only query in required, got %v", required)
	}
}

// Serialize tests

func TestSerialize_AnthropicFormat(t *testing.T) {
	file := parseFile(`
module demo
/// Search the catalog.
tool fn search(query: String) needs DB.read -> String { return query }
`)
	schemas := toolschema.Extract(file)

	out, err := toolschema.Serialize(schemas, toolschema.FormatAnthropic)
	if err != nil {
		t.Fatalf("Serialize returned error: %v", err)
	}
	s := string(out)

	// Anthropic uses "input_schema", not "parameters"
	if !strings.Contains(s, `"input_schema"`) {
		t.Errorf("expected 'input_schema' key in anthropic output, got:\n%s", s)
	}
	if strings.Contains(s, `"parameters"`) {
		t.Errorf("unexpected 'parameters' key in anthropic output, got:\n%s", s)
	}
	// No type/function wrapper (no top-level "type": "function")
	if strings.Contains(s, `"type": "function"`) {
		t.Errorf("unexpected 'type: function' wrapper in anthropic output, got:\n%s", s)
	}
	if strings.Contains(s, `"function"`) {
		t.Errorf("unexpected 'function' wrapper in anthropic output, got:\n%s", s)
	}
}

func TestSerialize_OpenAIFormat(t *testing.T) {
	file := parseFile(`
module demo
/// Search the catalog.
tool fn search(query: String) needs DB.read -> String { return query }
`)
	schemas := toolschema.Extract(file)

	out, err := toolschema.Serialize(schemas, toolschema.FormatOpenAI)
	if err != nil {
		t.Fatalf("Serialize returned error: %v", err)
	}
	s := string(out)

	// OpenAI uses "parameters", not "input_schema"
	if strings.Contains(s, `"input_schema"`) {
		t.Errorf("unexpected 'input_schema' key in openai output, got:\n%s", s)
	}
	if !strings.Contains(s, `"parameters"`) {
		t.Errorf("expected 'parameters' key in openai output, got:\n%s", s)
	}
	// Must have type/function wrapper
	if !strings.Contains(s, `"type": "function"`) {
		t.Errorf("expected 'type: function' wrapper in openai output, got:\n%s", s)
	}
	if !strings.Contains(s, `"function"`) {
		t.Errorf("expected 'function' wrapper object in openai output, got:\n%s", s)
	}
}

func TestSerialize_UnknownFormat_ReturnsError(t *testing.T) {
	schemas := toolschema.Extract(parseFile(`module demo`))
	_, err := toolschema.Serialize(schemas, toolschema.Format("garbage"))
	if err == nil {
		t.Error("expected error for unknown format, got nil")
	}
}

func TestSerialize_OpenAI_PreservesNameAndDescription(t *testing.T) {
	file := parseFile(`
module demo
/// Lookup a user by ID.
tool fn get_user(user_id: Int) -> String { return "x" }
`)
	schemas := toolschema.Extract(file)
	out, err := toolschema.Serialize(schemas, toolschema.FormatOpenAI)
	if err != nil {
		t.Fatalf("Serialize returned error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"name": "get_user"`) {
		t.Errorf("expected name in openai output, got:\n%s", s)
	}
	if !strings.Contains(s, `"description": "Lookup a user by ID."`) {
		t.Errorf("expected description in openai output, got:\n%s", s)
	}
}

func TestSerialize_EmptySchemas_ReturnsEmptyArray(t *testing.T) {
	out, err := toolschema.Serialize([]toolschema.ToolSchema{}, toolschema.FormatAnthropic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(string(out)) != "[]" {
		t.Errorf("expected empty array, got: %s", out)
	}
}
