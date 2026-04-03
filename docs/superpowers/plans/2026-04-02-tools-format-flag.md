# `splash tools --format` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `--format` flag to `splash tools` that serializes tool schemas in either Anthropic or OpenAI wire format.

**Architecture:** A `Serialize(schemas []ToolSchema, format Format) ([]byte, error)` function in `toolschema` handles all format-aware JSON rendering. The internal `ToolSchema` struct is unchanged — it's the canonical intermediate representation. The CLI parses `--format` from `os.Args` and passes the value to `runTools`, which passes it to `Serialize`.

**Tech Stack:** Go standard library only — `encoding/json`, `fmt`. No new dependencies.

---

## Open Questions

> Resolve these before or during Task 1 — they affect the test assertions.

**Q1 — Default format.**
The current output is Anthropic-compatible (`input_schema` key, no wrapper). Options:
- `anthropic` — no breaking change; makes the flag optional
- No default — require `--format` explicitly; cleaner but breaks existing usage

**Resolved: default = `openai`.** This is a pre-open-source release; no existing users to break. OpenAI format is the more widely-used target. Anthropic remains fully supported via `--format anthropic`.

**Q2 — `effects` field in OpenAI output.**
The `effects` field is Splash-specific. OpenAI's API ignores unknown fields, so including it is harmless. Options:
- Always include `effects` in both formats (simplest — no conditional logic)
- Strip `effects` from `openai` output (strictest conformance)
- Always strip `effects` from both `openai` and `anthropic`, expose it only in a future `splash` format

**Resolved: always include `effects` in both formats.** Both APIs ignore unknown fields. The field is useful metadata for tooling and documentation.

---

## File Map

| File | Change |
|------|--------|
| `internal/toolschema/toolschema.go` | Add `Format` type, `Serialize()` function, unexported OpenAI wire structs |
| `internal/toolschema/toolschema_test.go` | Tests for `Serialize()` — both formats, unknown format error |
| `cmd/splash/main.go` | Parse `--format` in `main()`, thread it into `runTools(path, format)` |

---

## Task 1: `Serialize()` in toolschema

**Files:**
- Modify: `internal/toolschema/toolschema.go`
- Test: `internal/toolschema/toolschema_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/toolschema/toolschema_test.go`:

```go
func TestSerialize_AnthropicFormat(t *testing.T) {
	file := parseFile(`
module demo
/// Search the catalog.
@tool
fn search(query: String) needs DB.read -> String { return query }
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
	// No type/function wrapper
	if strings.Contains(s, `"type"`) {
		t.Errorf("unexpected 'type' wrapper in anthropic output, got:\n%s", s)
	}
}

func TestSerialize_OpenAIFormat(t *testing.T) {
	file := parseFile(`
module demo
/// Search the catalog.
@tool
fn search(query: String) needs DB.read -> String { return query }
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
@tool
fn get_user(user_id: Int) -> String { return "x" }
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/toolschema/... -run TestSerialize -v
```

Expected: FAIL — `toolschema.Serialize undefined`, `toolschema.FormatAnthropic undefined`

- [ ] **Step 3: Add `Format` type, `Serialize()`, and OpenAI wire structs to `toolschema.go`**

Add after the `SchemaProperty` struct in `internal/toolschema/toolschema.go`:

```go
// Format controls the JSON wire format emitted by Serialize.
type Format string

const (
	// FormatAnthropic emits the Anthropic tool-calling format:
	// [{name, description, input_schema: {type, properties, required}}]
	FormatAnthropic Format = "anthropic"

	// FormatOpenAI emits the OpenAI tool-calling format:
	// [{type: "function", function: {name, description, parameters: {type, properties, required}}}]
	FormatOpenAI Format = "openai"
)

// openAITool is the top-level wrapper in the OpenAI tools array.
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

// openAIFunction is the inner object in the OpenAI tool wrapper.
type openAIFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  InputSchema `json:"parameters"`
	Effects     []string    `json:"effects,omitempty"`
}

// Serialize marshals schemas to JSON in the requested format.
// Returns an error for unrecognized format values.
func Serialize(schemas []ToolSchema, format Format) ([]byte, error) {
	switch format {
	case FormatAnthropic:
		return json.MarshalIndent(schemas, "", "  ")
	case FormatOpenAI:
		tools := make([]openAITool, len(schemas))
		for i, s := range schemas {
			tools[i] = openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        s.Name,
					Description: s.Description,
					Parameters:  s.InputSchema,
					Effects:     s.Effects,
				},
			}
		}
		return json.MarshalIndent(tools, "", "  ")
	default:
		return nil, fmt.Errorf("unknown format %q: use %q or %q", format, FormatAnthropic, FormatOpenAI)
	}
}
```

Add `"encoding/json"` and `"fmt"` to the import block at the top of `toolschema.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/toolschema/... -run TestSerialize -v
```

Expected: all five `TestSerialize_*` tests PASS

- [ ] **Step 5: Run full package test suite**

```bash
go test ./internal/toolschema/... -v
```

Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/toolschema/toolschema.go internal/toolschema/toolschema_test.go
git commit --no-gpg-sign -m "feat: add Serialize() with Format-aware JSON output to toolschema"
```

---

## Task 2: Wire `--format` flag into the CLI

**Files:**
- Modify: `cmd/splash/main.go`

- [ ] **Step 1: Update `runTools` signature and add format parsing**

In `cmd/splash/main.go`, change:

```go
// Before
case "tools":
    if err := runTools(file); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
```

```go
// After
case "tools":
    format := toolschema.FormatOpenAI // default
    for i := 3; i < len(os.Args)-1; i++ {
        if os.Args[i] == "--format" {
            format = toolschema.Format(os.Args[i+1])
        }
    }
    if err := runTools(file, format); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
```

Update the usage string at the top of `main()`:

```go
// Before
fmt.Fprintln(os.Stderr, "usage: splash <check|build|emit|tools> <file.splash> [-o output]")

// After
fmt.Fprintln(os.Stderr, "usage: splash <check|build|emit|tools> <file.splash> [-o output] [--format anthropic|openai]")
```

Update `runTools` signature and its final serialization block:

```go
// Before
func runTools(path string) error {

// After
func runTools(path string, format toolschema.Format) error {
```

Replace the final marshal+print block in `runTools`:

```go
// Before
schemas := toolschema.ExtractReachable(f, agentReachable)
if schemas == nil {
    schemas = []toolschema.ToolSchema{}
}
out, err := json.MarshalIndent(schemas, "", "  ")
if err != nil {
    return err
}
fmt.Println(string(out))
return nil

// After
schemas := toolschema.ExtractReachable(f, agentReachable)
if schemas == nil {
    schemas = []toolschema.ToolSchema{}
}
out, err := toolschema.Serialize(schemas, format)
if err != nil {
    return err
}
fmt.Println(string(out))
return nil
```

Remove the now-unused `"encoding/json"` import from `cmd/splash/main.go` if nothing else uses it (check the other functions — `runTools` was the only json user).

- [ ] **Step 2: Build and smoke-test both formats**

```bash
go build ./cmd/splash/... && \
  ./splash tools examples/finance/finance.splash && \
  echo "---" && \
  ./splash tools --format anthropic examples/finance/finance.splash && \
  echo "---" && \
  ./splash tools --format openai examples/finance/finance.splash
```

Expected:
- First two outputs are identical (default = openai): `"type": "function"` wrapper present, `"parameters"` key present
- Third output has `"input_schema"` key, no `"type": "function"` wrapper

- [ ] **Step 3: Verify unknown format returns a non-zero exit**

```bash
./splash tools --format badvalue examples/finance/finance.splash
echo "exit: $?"
```

Expected: prints error to stderr, `exit: 1`

- [ ] **Step 4: Run full test suite**

```bash
go test ./...
```

Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/splash/main.go
git commit --no-gpg-sign -m "feat: splash tools --format flag (anthropic|openai)"
```

---

## Task 3: Update docs

**Files:**
- Modify: `internal/toolschema/toolschema.go` — fix package comment
- Modify: `ROADMAP.md` — note `--format` flag under v0.1 tooling

- [ ] **Step 1: Fix the package comment**

In `internal/toolschema/toolschema.go`, change:

```go
// Before
// Package toolschema generates JSON Schema tool definitions from @tool-annotated
// Splash function declarations. Output is compatible with the Anthropic and
// OpenAI tool-calling APIs.

// After
// Package toolschema generates JSON Schema tool definitions from @tool-annotated
// Splash function declarations. Use Serialize() with FormatAnthropic or FormatOpenAI
// to target specific API wire formats.
```

- [ ] **Step 2: Update ROADMAP.md**

In the **v0.1 tooling** bullet list, change:

```markdown
// Before
- `splash tools` — JSON Schema from `@tool` signatures, filtered to agent-reachable set

// After
- `splash tools` — JSON Schema from `@tool` signatures, filtered to agent-reachable set; `--format anthropic|openai` for API-compatible output
```

- [ ] **Step 3: Commit**

```bash
git add internal/toolschema/toolschema.go ROADMAP.md
git commit --no-gpg-sign -m "docs: update toolschema package comment and roadmap for --format flag"
```

---

## Self-Review Checklist

**Spec coverage:**
- [x] `FormatAnthropic` produces current output shape (`input_schema`, no wrapper)
- [x] `FormatOpenAI` produces `{type: "function", function: {name, description, parameters}}`
- [x] Unknown format returns a non-zero exit
- [x] Default format is `openai`
- [x] `effects` field behavior resolved by Open Question 2 (recommendation: always include)
- [x] Package doc comment fixed
- [x] ROADMAP updated

**Placeholder scan:** None found.

**Type consistency:** `toolschema.Format`, `toolschema.FormatAnthropic`, `toolschema.FormatOpenAI`, `toolschema.Serialize` used consistently across all tasks.
