package toolschema_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/compiler/internal/toolschema"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
)

func schemaFor(t *testing.T, typ types.Type) *toolschema.SchemaProperty {
	t.Helper()
	prop, err := toolschema.TypeToSchema(typ)
	if err != nil {
		t.Fatalf("parameter schema: %v", err)
	}
	return prop
}

func TestCheckedParameterSchemas(t *testing.T) {
	for _, test := range []struct {
		typ  types.Type
		want string
	}{
		{types.String, "string"},
		{types.Int, "integer"},
		{types.Float, "number"},
		{types.Bool, "boolean"},
		{&types.OptionalType{Inner: types.String}, "string"},
		{&types.NamedType{Declaration: &ast.TypeDecl{Name: "Record"}}, "object"},
	} {
		if prop := schemaFor(t, test.typ); prop.Type != test.want {
			t.Errorf("expected type %q, got %q", test.want, prop.Type)
		}
	}
	list := schemaFor(t, &types.ListType{Element: types.Int})
	if list.Type != "array" || list.Items == nil || list.Items.Type != "integer" {
		t.Errorf("expected a list of integers, got %+v", list)
	}
	enum := schemaFor(t, &types.EnumType{Declaration: &ast.EnumDecl{
		Name: "Color", Variants: []ast.EnumVariant{{Name: "red"}, {Name: "green"}},
	}})
	if enum.Type != "string" || !slices.Equal(enum.Enum, []string{"red", "green"}) {
		t.Errorf("expected a two-variant string enum, got %+v", enum)
	}
}

func TestUnsupportedParameterSchema(t *testing.T) {
	for _, typ := range []types.Type{
		types.Unknown, types.Void, types.Named("Unresolved"),
		&types.ListType{Element: types.Unknown},
		&types.EnumType{Declaration: &ast.EnumDecl{
			Name: "Payload", Variants: []ast.EnumVariant{{
				Name: "value", Payload: &ast.NamedTypeExpr{Name: "Int"},
			}},
		}},
	} {
		if prop, err := toolschema.TypeToSchema(typ); err == nil || prop != nil {
			t.Errorf("%s: expected admission failure, got %+v, %v", typ.TypeName(), prop, err)
		}
	}
}

// parseFile helper for Extract tests
func parseFile(src string) *ast.File {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	return file
}

func extractSchemas(t *testing.T, file *ast.File) []toolschema.ToolSchema {
	t.Helper()
	typed, diagnostics := typechecker.New().Check(file)
	if len(diagnostics) != 0 {
		t.Fatalf("check tool declarations: %v", diagnostics)
	}
	schemas, err := toolschema.Extract([]*typechecker.TypedFile{typed})
	if err != nil {
		t.Fatal(err)
	}
	return schemas
}

// Extract tests

func TestExtract_NoTools(t *testing.T) {
	file := parseFile(`
module foo
fn helper() -> String { return "hi" }
`)
	schemas := extractSchemas(t, file)
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas, got %d", len(schemas))
	}
}

func TestExtract_SingleTool_Name(t *testing.T) {
	file := parseFile(`
module foo
tool fn search(query: String) -> String { return query }
`)
	schemas := extractSchemas(t, file)
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
	schemas := extractSchemas(t, file)
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
	schemas := extractSchemas(t, file)
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
	schemas := extractSchemas(t, file)
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
	schemas := extractSchemas(t, file)
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
	schemas := extractSchemas(t, file)
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
	tools := extractSchemas(t, file)
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
	tools := extractSchemas(t, file)
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

func TestToolSchema_DefaultParamNotRequired(t *testing.T) {
	src := `
module demo
/// Search with optional limit.
tool fn search(query: String, limit: Int = 10) -> String { return query }
`
	file := parseFile(src)
	tools := extractSchemas(t, file)
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
	tools := extractSchemas(t, file)
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
	schemas := extractSchemas(t, file)

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
	schemas := extractSchemas(t, file)

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
	schemas := extractSchemas(t, parseFile(`module demo`))
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
	schemas := extractSchemas(t, file)
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
