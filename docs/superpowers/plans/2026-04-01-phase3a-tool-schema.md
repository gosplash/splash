# Phase 3a: `@tool` + JSON Schema Generation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a Splash function is annotated with `@tool`, `splash tools <file>` emits a JSON array of tool schemas in the Anthropic tool-calling format — one schema per `@tool` function — with descriptions from `///` doc comments and correct JSON Schema types for all parameters.

**Architecture:** Five sequential tasks. First two are plumbing (doc comment token + AST fields). Third and fourth build the schema types and extractor as a new `internal/toolschema` package. Fifth wires it into the CLI. Each task produces passing tests before the next begins.

**Tech Stack:** Go stdlib only — `encoding/json` for output, no external schema libraries.

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/token/token.go` | Modify | Add `DOC_COMMENT` token kind |
| `internal/lexer/lexer.go` | Modify | Emit `DOC_COMMENT` for `///`, stop skipping them |
| `internal/lexer/lexer_test.go` | Modify | Tests for doc comment lexing |
| `internal/ast/decl.go` | Modify | Add `Doc string` to `FunctionDecl` and `Param` |
| `internal/parser/parser.go` | Modify | Attach doc comments to function decls and params |
| `internal/parser/parser_test.go` | Modify | Tests for doc comment parsing |
| `internal/toolschema/toolschema.go` | Create | `ToolSchema` types + `Extract()` |
| `internal/toolschema/typemap.go` | Create | Splash type → JSON Schema conversion |
| `internal/toolschema/toolschema_test.go` | Create | Schema extraction and type mapping tests |
| `cmd/splash/main.go` | Modify | Add `splash tools` subcommand |

---

## Task 1: `DOC_COMMENT` token (lexer)

**Files:**
- Modify: `internal/token/token.go:79` (after `AT`)
- Modify: `internal/lexer/lexer.go:86-113` (`skipWhitespace`) and `lexer.go:294-296` (`/` case in `nextToken`)
- Modify: `internal/lexer/lexer_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/lexer/lexer_test.go`:

```go
func TestDocComment_EmitsToken(t *testing.T) {
	toks := tokens("/// hello world")
	if len(toks) < 2 {
		t.Fatalf("expected at least 2 tokens, got %d", len(toks))
	}
	if toks[0].Kind != token.DOC_COMMENT {
		t.Errorf("expected DOC_COMMENT, got kind %d literal %q", toks[0].Kind, toks[0].Literal)
	}
	if toks[0].Literal != "hello world" {
		t.Errorf("expected literal %q, got %q", "hello world", toks[0].Literal)
	}
}

func TestDocComment_RegularCommentSkipped(t *testing.T) {
	toks := tokens("// not a doc\nfoo")
	// should see IDENT("foo") with no DOC_COMMENT
	if toks[0].Kind == token.DOC_COMMENT {
		t.Error("regular // comment should be skipped, not emitted as DOC_COMMENT")
	}
	if toks[0].Kind != token.IDENT || toks[0].Literal != "foo" {
		t.Errorf("expected IDENT(foo), got kind %d literal %q", toks[0].Kind, toks[0].Literal)
	}
}

func TestDocComment_MultiLine(t *testing.T) {
	toks := tokens("/// line one\n/// line two\nfn")
	if len(toks) < 3 {
		t.Fatalf("expected at least 3 tokens, got %d", len(toks))
	}
	if toks[0].Kind != token.DOC_COMMENT || toks[0].Literal != "line one" {
		t.Errorf("first doc comment: got kind %d literal %q", toks[0].Kind, toks[0].Literal)
	}
	if toks[1].Kind != token.DOC_COMMENT || toks[1].Literal != "line two" {
		t.Errorf("second doc comment: got kind %d literal %q", toks[1].Kind, toks[1].Literal)
	}
	if toks[2].Kind != token.FN {
		t.Errorf("expected FN after doc comments, got kind %d", toks[2].Kind)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/lexer/... -run "TestDocComment" -v
```

Expected: FAIL — `token.DOC_COMMENT` is undefined.

- [ ] **Step 3: Add `DOC_COMMENT` to `internal/token/token.go`**

After the `AT` line (currently line 79):

```go
	AT          // @ annotation prefix
	DOC_COMMENT // /// doc comment (literal is text after "/// ", trimmed)
```

- [ ] **Step 4: Update `skipWhitespace` in `internal/lexer/lexer.go`**

Change the `// Line comment` branch to return early on `///`:

```go
// skipWhitespace skips spaces, tabs, carriage returns, newlines, and comments.
func (l *Lexer) skipWhitespace() {
	for {
		ch := l.current()
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
			l.advance()
		case ch == '/' && l.peek() == '/':
			if l.peekAt(2) == '/' {
				return // doc comment — let nextToken handle it
			}
			// Regular line comment: skip to end of line
			for l.current() != '\n' && l.current() != 0 {
				l.advance()
			}
		case ch == '/' && l.peek() == '*':
			// Block comment: skip until */
			l.advance() // /
			l.advance() // *
			for l.current() != 0 {
				if l.current() == '*' && l.peek() == '/' {
					l.advance() // *
					l.advance() // /
					break
				}
				l.advance()
			}
		default:
			return
		}
	}
}
```

- [ ] **Step 5: Update the `/` case in `nextToken` in `internal/lexer/lexer.go`**

Replace the current `case '/'` block (which just emits SLASH):

```go
	case '/':
		if l.peek() == '/' && l.peekAt(2) == '/' {
			// Doc comment: consume "///" plus optional leading space
			l.advance() // first /
			l.advance() // second /
			l.advance() // third /
			if l.current() == ' ' {
				l.advance() // strip one leading space
			}
			start := l.pos
			for l.current() != '\n' && l.current() != 0 {
				l.advance()
			}
			// trim trailing whitespace
			src := l.src[start:l.pos]
			for len(src) > 0 && (src[len(src)-1] == ' ' || src[len(src)-1] == '\t' || src[len(src)-1] == '\r') {
				src = src[:len(src)-1]
			}
			return token.Token{Kind: token.DOC_COMMENT, Literal: string(src), Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.SLASH, Literal: "/", Pos: pos}
```

- [ ] **Step 6: Run tests to confirm they pass**

```bash
go test ./internal/lexer/... -v
```

Expected: all PASS, including the three new `TestDocComment_*` tests.

- [ ] **Step 7: Run full test suite to confirm nothing regressed**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/token/token.go internal/lexer/lexer.go internal/lexer/lexer_test.go
git commit -m "feat: lex /// doc comments as DOC_COMMENT tokens"
```

---

## Task 2: Doc comment AST fields and parser attachment

**Files:**
- Modify: `internal/ast/decl.go` (add `Doc string` to `FunctionDecl` and `Param`)
- Modify: `internal/parser/parser.go` (`parseTopLevelDecl`, `parseFunctionDecl`, `parseParam`)
- Modify: `internal/parser/parser_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/parser/parser_test.go`:

```go
func TestFunctionDecl_DocComment(t *testing.T) {
	src := `
module foo
/// Greets the named person.
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Doc != "Greets the named person." {
		t.Errorf("expected doc %q, got %q", "Greets the named person.", fn.Doc)
	}
}

func TestFunctionDecl_MultiLineDocComment(t *testing.T) {
	src := `
module foo
/// First line.
/// Second line.
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	want := "First line.\nSecond line."
	if fn.Doc != want {
		t.Errorf("expected doc %q, got %q", want, fn.Doc)
	}
}

func TestFunctionDecl_NoDocComment(t *testing.T) {
	src := `
module foo
fn greet(name: String) -> String {
  return "hi"
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Doc != "" {
		t.Errorf("expected empty doc, got %q", fn.Doc)
	}
}

func TestParam_DocComment(t *testing.T) {
	src := `
module foo
@tool
fn search(
  /// The search query
  query: String,
  /// Max results to return
  limit: Int,
) -> String {
  return query
}`
	file := parse(t, src)
	fn := file.Declarations[0].(*ast.FunctionDecl)
	if fn.Params[0].Doc != "The search query" {
		t.Errorf("expected param[0].Doc %q, got %q", "The search query", fn.Params[0].Doc)
	}
	if fn.Params[1].Doc != "Max results to return" {
		t.Errorf("expected param[1].Doc %q, got %q", "Max results to return", fn.Params[1].Doc)
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/parser/... -run "TestFunctionDecl_DocComment|TestParam_DocComment" -v
```

Expected: FAIL — `fn.Doc` field does not exist.

- [ ] **Step 3: Add `Doc` fields in `internal/ast/decl.go`**

In `FunctionDecl` (currently at line 92), add `Doc string` as the first field:

```go
// FunctionDecl declares a named function.
type FunctionDecl struct {
	Doc         string
	Name        string
	TypeParams  []TypeParam
	Params      []Param
	ReturnType  TypeExpr
	Effects     []EffectExpr
	Body        *BlockStmt
	Annotations []Annotation
	IsAsync     bool
	Position    token.Position
}
```

In `Param` (currently at line 55), add `Doc string`:

```go
// Param is a function parameter.
type Param struct {
	Doc      string
	Name     string
	Type     TypeExpr
	Default  Expr
	Variadic bool
	Pos      token.Position
}
```

- [ ] **Step 4: Update `parseTopLevelDecl` in `internal/parser/parser.go`**

The function starts at line 241. Add doc comment collection before the annotation loop, and pass `doc` to `parseFunctionDecl`. Replace the full function:

```go
// parseTopLevelDecl parses annotations then dispatches to the right declaration parser.
func (p *Parser) parseTopLevelDecl() ast.Decl {
	// collect leading doc comment (/// lines)
	var docLines []string
	for p.check(token.DOC_COMMENT) {
		docLines = append(docLines, p.advance().Literal)
	}
	doc := strings.Join(docLines, "\n")

	// collect leading annotations
	var annots []ast.Annotation
	for p.check(token.AT) {
		a := p.parseAnnotation()
		annots = append(annots, a)
	}

	switch p.current().Kind {
	case token.FN:
		return p.parseFunctionDecl(annots, false, doc)
	case token.ASYNC:
		p.advance() // consume async
		return p.parseFunctionDecl(annots, true, doc)
	case token.TYPE:
		return p.parseTypeDecl(annots)
	case token.ENUM:
		return p.parseEnumDecl(annots)
	case token.CONSTRAINT:
		return p.parseConstraintDecl(annots)
	case token.EOF:
		return nil
	default:
		cur := p.current()
		p.errorf(cur.Pos, "unexpected token %q at top level", cur.Literal)
		before := p.pos
		p.sync()
		if p.pos == before {
			p.advance()
		}
		return nil
	}
}
```

- [ ] **Step 5: Update `parseFunctionDecl` signature and body in `internal/parser/parser.go`**

Change the signature (line 310) to accept `doc string`, and set it on the returned struct:

```go
// parseFunctionDecl parses: fn name[<TypeParams>](params) [needs Effects] [-> ReturnType] Block
func (p *Parser) parseFunctionDecl(annots []ast.Annotation, isAsync bool, doc string) *ast.FunctionDecl {
	pos := p.current().Pos
	p.eat(token.FN)

	name := p.eat(token.IDENT)

	// optional type params
	typeParams := p.parseTypeParams()

	// params
	p.eat(token.LPAREN)
	params := p.parseParams()
	p.eat(token.RPAREN)

	// optional needs clause (effects come before return type)
	var effects []ast.EffectExpr
	if p.check(token.NEEDS) {
		effects = p.parseEffects()
	}

	// optional return type
	var returnType ast.TypeExpr
	if p.check(token.ARROW) {
		p.advance() // consume ->
		returnType = p.parseTypeExpr()
	}

	// body
	body := p.parseBlockStmt()

	return &ast.FunctionDecl{
		Doc:         doc,
		Name:        name.Literal,
		TypeParams:  typeParams,
		Params:      params,
		ReturnType:  returnType,
		Effects:     effects,
		Body:        body,
		Annotations: annots,
		IsAsync:     isAsync,
		Position:    pos,
	}
}
```

- [ ] **Step 6: Update `parseParam` in `internal/parser/parser.go`**

At the start of `parseParam` (line 402), collect any leading `DOC_COMMENT` tokens:

```go
// parseParam parses a single parameter: [...]name: Type [= default]
func (p *Parser) parseParam() ast.Param {
	// collect leading doc comment
	var docLines []string
	for p.check(token.DOC_COMMENT) {
		docLines = append(docLines, p.advance().Literal)
	}
	doc := strings.Join(docLines, "\n")

	pos := p.current().Pos
	variadic := false
	if p.check(token.DOT) && p.peek().Kind == token.DOT {
		// variadic: ...name
		p.advance()
		p.advance()
		if p.check(token.DOT) {
			p.advance()
		}
		variadic = true
	}

	name := p.eat(token.IDENT)
	p.eat(token.COLON)
	typ := p.parseTypeExpr()

	var def ast.Expr
	if p.check(token.ASSIGN) {
		p.advance()
		def = p.parseExpr(precLowest)
	}

	return ast.Param{
		Doc:      doc,
		Name:     name.Literal,
		Type:     typ,
		Default:  def,
		Variadic: variadic,
		Pos:      pos,
	}
}
```

- [ ] **Step 7: Verify `strings` is already imported in parser.go**

```bash
head -15 internal/parser/parser.go
```

Expected: `"strings"` is already in the import block (it's used by `parseUseDecl`). If missing, add it.

- [ ] **Step 8: Run tests to confirm they pass**

```bash
go test ./internal/parser/... -v
```

Expected: all PASS including the four new doc comment tests.

- [ ] **Step 9: Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/ast/decl.go internal/parser/parser.go internal/parser/parser_test.go
git commit -m "feat: attach /// doc comments to FunctionDecl and Param in AST"
```

---

## Task 3: JSON Schema types and type mapper

**Files:**
- Create: `internal/toolschema/toolschema.go`
- Create: `internal/toolschema/typemap.go`
- Create: `internal/toolschema/toolschema_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/toolschema/toolschema_test.go`:

```go
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
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/toolschema/... 2>&1
```

Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Create `internal/toolschema/toolschema.go`**

```go
// Package toolschema generates JSON Schema tool definitions from @tool-annotated
// Splash function declarations. Output is compatible with the Anthropic and
// OpenAI tool-calling APIs.
package toolschema

// ToolSchema is the complete schema for a single @tool function.
type ToolSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"input_schema"`
}

// InputSchema is the parameter schema for a tool. Always type "object".
type InputSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]*SchemaProperty `json:"properties"`
	Required   []string                   `json:"required,omitempty"`
}

// SchemaProperty is a single value in a JSON Schema object.
type SchemaProperty struct {
	Type        string                     `json:"type,omitempty"`
	Description string                     `json:"description,omitempty"`
	Items       *SchemaProperty            `json:"items,omitempty"`
	Properties  map[string]*SchemaProperty `json:"properties,omitempty"`
	Enum        []string                   `json:"enum,omitempty"`
}
```

- [ ] **Step 4: Create `internal/toolschema/typemap.go`**

```go
package toolschema

import "gosplash.dev/splash/internal/ast"

// TypeExprToSchema converts a Splash type expression to a JSON Schema property.
// Exported so tests can call it directly.
// file provides context for resolving enum variants.
func TypeExprToSchema(te ast.TypeExpr, file *ast.File) *SchemaProperty {
	enumDecls := buildEnumIndex(file)
	return typeExprToSchema(te, enumDecls)
}

func typeExprToSchema(te ast.TypeExpr, enumDecls map[string]*ast.EnumDecl) *SchemaProperty {
	switch t := te.(type) {
	case *ast.NamedTypeExpr:
		return namedTypeToSchema(t, enumDecls)
	case *ast.OptionalTypeExpr:
		// Optional only affects the required array, not the schema shape.
		return typeExprToSchema(t.Inner, enumDecls)
	case *ast.FnTypeExpr:
		return &SchemaProperty{Type: "object"}
	}
	return &SchemaProperty{Type: "string"}
}

func namedTypeToSchema(t *ast.NamedTypeExpr, enumDecls map[string]*ast.EnumDecl) *SchemaProperty {
	switch t.Name {
	case "String":
		return &SchemaProperty{Type: "string"}
	case "Int":
		return &SchemaProperty{Type: "integer"}
	case "Float":
		return &SchemaProperty{Type: "number"}
	case "Bool":
		return &SchemaProperty{Type: "boolean"}
	case "List":
		if len(t.TypeArgs) == 1 {
			return &SchemaProperty{
				Type:  "array",
				Items: typeExprToSchema(t.TypeArgs[0], enumDecls),
			}
		}
		return &SchemaProperty{Type: "array"}
	}
	if decl, ok := enumDecls[t.Name]; ok {
		var variants []string
		for _, v := range decl.Variants {
			variants = append(variants, v.Name)
		}
		return &SchemaProperty{Type: "string", Enum: variants}
	}
	return &SchemaProperty{Type: "object"}
}

func buildEnumIndex(file *ast.File) map[string]*ast.EnumDecl {
	decls := make(map[string]*ast.EnumDecl)
	if file == nil {
		return decls
	}
	for _, d := range file.Declarations {
		if e, ok := d.(*ast.EnumDecl); ok {
			decls[e.Name] = e
		}
	}
	return decls
}
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
go test ./internal/toolschema/... -run "TestTypeMapper" -v
```

Expected: all PASS.

- [ ] **Step 6: Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/toolschema/
git commit -m "feat: JSON Schema types and Splash type → schema mapper"
```

---

## Task 4: Tool schema extractor

**Files:**
- Modify: `internal/toolschema/toolschema.go` (add `Extract` and helpers)
- Modify: `internal/toolschema/toolschema_test.go` (add extraction tests)

- [ ] **Step 1: Write the failing tests**

Add to `internal/toolschema/toolschema_test.go`:

```go
// extraction tests

func parseFile(src string) *ast.File {
	toks := lexer.New("test.splash", src).Tokenize()
	p := parser.New("test.splash", toks)
	file, _ := p.ParseFile()
	return file
}

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
	schema := schemas[0]
	required := schema.InputSchema.Required
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
```

Add required imports at the top of the test file. The test file needs the lexer and parser:

```go
package toolschema_test

import (
	"testing"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/toolschema"
)
```

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/toolschema/... -run "TestExtract" -v
```

Expected: FAIL — `toolschema.Extract` is undefined.

- [ ] **Step 3: Add `Extract` and helpers to `internal/toolschema/toolschema.go`**

Append to the existing `toolschema.go`:

```go
import "gosplash.dev/splash/internal/ast"

// Extract returns a ToolSchema for every @tool-annotated function in file.
func Extract(file *ast.File) []ToolSchema {
	enumDecls := buildEnumIndex(file)
	var tools []ToolSchema
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !hasAnnotation(fn.Annotations, ast.AnnotTool) {
			continue
		}
		tools = append(tools, buildToolSchema(fn, enumDecls))
	}
	return tools
}

func hasAnnotation(anns []ast.Annotation, kind ast.AnnotationKind) bool {
	for _, a := range anns {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

func buildToolSchema(fn *ast.FunctionDecl, enumDecls map[string]*ast.EnumDecl) ToolSchema {
	props := make(map[string]*SchemaProperty)
	var required []string

	for _, p := range fn.Params {
		prop := typeExprToSchema(p.Type, enumDecls)
		if p.Doc != "" {
			prop.Description = p.Doc
		}
		props[p.Name] = prop
		// Optional params (T?) are not in required[]
		if _, isOptional := p.Type.(*ast.OptionalTypeExpr); !isOptional {
			required = append(required, p.Name)
		}
	}

	return ToolSchema{
		Name:        fn.Name,
		Description: fn.Doc,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
}
```

Note: `toolschema.go` already imports nothing — add the import block:

```go
package toolschema

import "gosplash.dev/splash/internal/ast"

// ToolSchema is the complete schema for a single @tool function.
// ... (existing struct definitions) ...
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/toolschema/... -v
```

Expected: all PASS — both `TestTypeMapper_*` and `TestExtract_*`.

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/toolschema/toolschema.go internal/toolschema/toolschema_test.go
git commit -m "feat: Extract() produces ToolSchema for every @tool function"
```

---

## Task 5: `splash tools` CLI command

**Files:**
- Modify: `cmd/splash/main.go`

- [ ] **Step 1: Write the failing integration test manually**

Before implementing, verify the current binary does NOT support `tools`:

```bash
go build -o splash ./cmd/splash && ./splash tools examples/agent_tools/agent_tools.splash 2>&1
```

Expected: `unknown command: tools`

- [ ] **Step 2: Add `splash tools` to `cmd/splash/main.go`**

Add `"encoding/json"` to the import block. Then add the `tools` case to `main()`:

```go
import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/callgraph"
	"gosplash.dev/splash/internal/codegen"
	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/parser"
	"gosplash.dev/splash/internal/safety"
	"gosplash.dev/splash/internal/toolschema"
	"gosplash.dev/splash/internal/typechecker"
)
```

Add the case to the `switch cmd` in `main()`:

```go
case "tools":
	if err := runTools(file); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
```

Add the `runTools` function:

```go
func runTools(path string) error {
	f, err := parseFile(path)
	if err != nil {
		return err
	}

	tc := typechecker.New()
	_, typeErrs := tc.Check(f)
	for _, d := range typeErrs {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(typeErrs) > 0 {
		return fmt.Errorf("type errors")
	}

	schemas := toolschema.Extract(f)
	out, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
```

- [ ] **Step 3: Build and run on the agent_tools example**

```bash
go build -o splash ./cmd/splash && ./splash tools examples/agent_tools/agent_tools.splash
```

Expected output (exact names and types must match):

```json
[
  {
    "name": "search_catalog",
    "input_schema": {
      "type": "object",
      "properties": {
        "query": {
          "type": "string"
        },
        "limit": {
          "type": "integer"
        }
      },
      "required": [
        "query",
        "limit"
      ]
    }
  },
  {
    "name": "get_item",
    "input_schema": {
      "type": "object",
      "properties": {
        "item_id": {
          "type": "integer"
        },
        "category": {
          "type": "string"
        }
      },
      "required": [
        "item_id"
      ]
    }
  }
]
```

Verify:
- `search_catalog` has both `query` and `limit` in `required`
- `get_item` has only `item_id` in `required` (`category` is `String?`, so optional)
- No entry for `rebuild_search_index` (it's `@redline`, not `@tool`)
- No entry for `run_search_agent` (it's an agent entry point, not `@tool`)

- [ ] **Step 4: Run full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/splash/main.go
git commit -m "feat: splash tools command emits JSON Schema for @tool functions"
```

---

## Self-Review

**Spec coverage check:**

| PRD requirement | Covered by |
|---|---|
| `@tool` turns a function into AI-callable tool | Task 4 `Extract()` |
| Doc comment becomes description | Tasks 1 + 2 + 4 |
| Type signature becomes JSON Schema | Tasks 3 + 4 |
| `splash tools` CLI output | Task 5 |
| Optional params not in `required[]` | Task 4 `buildToolSchema` |
| Enum types → `{"enum": [...]}` | Task 3 `TestTypeMapper_Enum` |
| List types → `{"type": "array"}` | Task 3 `TestTypeMapper_ListOfInt` |

**Placeholder scan:** No TBDs, no "add appropriate handling" — every step has complete code.

**Type consistency check:**
- `SchemaProperty` used consistently across `toolschema.go` and `typemap.go`
- `TypeExprToSchema` (exported, used in tests) calls internal `typeExprToSchema` — consistent naming
- `buildEnumIndex` defined in `typemap.go`, called by both `TypeExprToSchema` and `buildToolSchema` in `toolschema.go` — consistent
- `fn.Doc` and `p.Doc` set in Task 2, read in Task 4 — field names match
