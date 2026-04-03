# std/ai Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `std/ai` runtime so that `ai.prompt<T>` compiles to working Go code that calls real AI providers, with a provider registry supporting OpenAI-compatible and Anthropic endpoints.

**Architecture:** The runtime is injected into generated Go code as a preamble (same pattern as `splashApprove`). When `use std/ai` is detected during codegen, the emitter writes the full runtime — `AIAdapter` interface, `ProviderRegistry`, `OpenAIAdapter`, `AnthropicAdapter`, and `splashAIPrompt[T]` — into the generated source. No external Go dependencies; the runtime is self-contained. `ai.prompt<T>` call sites are rewritten from the current broken `ai.prompt(opts)` to `splashAIPrompt[T any](opts, tools)`.

**Tech Stack:** Go 1.22 (generics), `net/http`, `encoding/json`. No new module dependencies.

---

## Settled Design Decisions

These were resolved in conversation and are not open for debate during implementation.

**Provider is explicit, never inferred from model name.**
`PromptOptions` has a `provider` field. Model names change too fast for inference to be reliable.

**`provider` is a registry key, not a protocol name.**
`"my-azure"`, `"local-llama"`, `"prod-openai"` are all valid provider names. The protocol (OpenAI wire format vs Anthropic wire format) is a property of the adapter registered under that name.

**Two adapter types for two wire protocols.**
`OpenAIAdapter` handles all OpenAI-compatible endpoints (OpenAI, Azure, Together, Groq, LM Studio, Ollama, etc.). `AnthropicAdapter` handles Anthropic's API. Adding a new provider that speaks OpenAI's protocol is one `RegisterProvider` call, not a new adapter type.

**Registry is configured in Go at startup, not in Splash.**
Splash code references provider names as strings. The Go side registers adapters with credentials. This keeps secrets out of Splash source.

**`ai.prompt<T>` returns `T`; errors propagate to the `needs Agent` boundary.**
The generated Go function `splashAIPrompt[T any]` returns `(T, error)`. Error cascade follows the same pattern as `@approve` — the CLI computes which functions need `(T, error)` widening via call graph analysis.

**Tool schemas are embedded in the binary.**
When `use std/ai` is present and `@tool` functions exist, the codegen emits a `var _splashToolSchemas` JSON literal. The adapter serializes this to its wire format on each call.

**`@budget` runtime enforcement is deferred.**
Compile-time argument validation is done. Injecting a counter into `ai.prompt` calls requires the runtime to exist first. Budget enforcement is Task 6 and depends on Tasks 1–5.

---

## Current State

`ai.prompt<T>(opts)` emits today as `ai.prompt(opts)` — `ai` is an undefined Go identifier. `splash build` on any file using `ai.prompt` produces broken Go that fails to link. `splash check` and `splash tools` work correctly; the gap is entirely in codegen and the missing runtime.

`PromptOptions` is currently a user-defined type in Splash source. After this plan, it moves to `std/ai` as a built-in type with the `provider` field added.

---

## Open Questions

> These do not block Tasks 1–3 but must be resolved before Task 4.

**Q1 — Tool call dispatch: how many round-trips?**
When the AI returns a tool call, the runtime invokes the Splash `@tool` function and re-submits. Should there be a max-rounds limit? If so, where is it configured — `@budget(max_calls)`, a `PromptOptions` field, or a registry-level default?

**Q2 — Streaming.**
The current `ai.prompt<T>` model is request/response. Does v0.2 need streaming? If yes, `Stream<T>` is the return type and the adapter interface needs a `Stream` method. If no, streaming is a v0.3 concern.

**Q3 — `PromptOptions` as a built-in vs user-defined.**
Currently users define `PromptOptions` themselves (as in the finance example). After this plan, `std/ai` provides it. Migration: user-defined `PromptOptions` types that match the built-in shape should either be deprecated with a clear error or merged automatically. Simplest: emit a compile error if `use std/ai` is present and a user type named `PromptOptions` is also declared.

---

## File Map

| File | Change | Why |
|------|--------|-----|
| `internal/codegen/codegen.go` | Detect `use std/ai`, emit AI preamble | Entry point for preamble injection |
| `internal/codegen/ai_preamble.go` | New — Go source template for the full AI runtime | Keeps preamble generation isolated |
| `internal/codegen/expr.go` | Rewrite `ai.prompt<T>` call sites to `splashAIPrompt[T]` | Current emit is broken |
| `internal/codegen/decl.go` | Emit `var _splashToolSchemas` when `@tool` fns exist + `use std/ai` | Schema embedding |
| `internal/codegen/codegen_test.go` | Tests for AI preamble presence, `splashAIPrompt` call rewrite, schema embedding | |
| `internal/typechecker/typechecker.go` | Add `provider` field to the built-in `PromptOptions` type shape | Type-check the new field |
| `internal/typechecker/typechecker_test.go` | Test `provider` field is accepted, unknown fields rejected | |

---

## Task 1: AI preamble — registry, adapter interface, PromptOptions

**Files:**
- Create: `internal/codegen/ai_preamble.go`
- Modify: `internal/codegen/codegen.go`
- Modify: `internal/codegen/codegen_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/codegen/codegen_test.go`:

```go
func TestAIPreamble_EmittedWhenStdAIUsed(t *testing.T) {
	src := `
module demo
use std/ai
fn dummy() -> String { return "x" }
`
	out := emitSrc(src)
	if !strings.Contains(out, "splashAIPrompt") {
		t.Errorf("expected splashAIPrompt in output when use std/ai present:\n%s", out)
	}
	if !strings.Contains(out, "AIAdapter") {
		t.Errorf("expected AIAdapter interface in output:\n%s", out)
	}
	if !strings.Contains(out, "RegisterProvider") {
		t.Errorf("expected RegisterProvider in output:\n%s", out)
	}
}

func TestAIPreamble_NotEmittedWithoutStdAI(t *testing.T) {
	src := `
module demo
fn dummy() -> String { return "x" }
`
	out := emitSrc(src)
	if strings.Contains(out, "splashAIPrompt") {
		t.Errorf("expected no AI preamble without use std/ai:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/codegen/... -run TestAIPreamble -v
```

Expected: FAIL — no `splashAIPrompt` in output

- [ ] **Step 3: Create `internal/codegen/ai_preamble.go`**

```go
package codegen

// aiPreamble is the Go runtime emitted when `use std/ai` is present.
// It provides the provider registry, AIAdapter interface, PromptOptions,
// and the splashAIPrompt generic function.
const aiPreamble = `
// ── std/ai runtime ────────────────────────────────────────────────────────────

// PromptOptions configures a single ai.prompt call.
type PromptOptions struct {
	Provider    string
	Model       string
	Input       string
	System      string
	Temperature float64
	MaxTokens   int
	Budget      float64
}

// AIAdapter is the interface every provider adapter implements.
// SerializeTools converts the canonical tool schema JSON (Anthropic format) to
// the wire format expected by this provider's API.
// Chat sends the request and returns the raw response body.
type AIAdapter interface {
	SerializeTools(canonicalJSON []byte) ([]byte, error)
	Chat(req _SplashChatRequest) (_SplashChatResponse, error)
}

// _SplashChatRequest is the provider-agnostic chat request.
type _SplashChatRequest struct {
	Model       string
	System      string
	Input       string
	Temperature float64
	MaxTokens   int
	Tools       []byte // provider-serialized tool schemas
}

// _SplashChatResponse is the provider-agnostic chat response.
type _SplashChatResponse struct {
	Content   string
	ToolCalls []_SplashToolCall
}

// _SplashToolCall represents a single tool invocation requested by the model.
type _SplashToolCall struct {
	Name      string
	InputJSON []byte
}

var _splashProviders = map[string]AIAdapter{}

// RegisterProvider registers an AI provider adapter under the given name.
// Call this before any ai.prompt invocation that references this provider.
// Example:
//
//	ai.RegisterProvider("openai", ai.OpenAIAdapter{
//	    BaseURL: "https://api.openai.com/v1",
//	    Token:   os.Getenv("OPENAI_API_KEY"),
//	})
func RegisterProvider(name string, adapter AIAdapter) {
	_splashProviders[name] = adapter
}

func _splashGetProvider(name string) (AIAdapter, error) {
	a, ok := _splashProviders[name]
	if !ok {
		return nil, fmt.Errorf("unknown AI provider %q: call RegisterProvider before using it", name)
	}
	return a, nil
}

// splashAIPrompt is the runtime for ai.prompt<T>. It is called with the
// provider name, prompt options, canonical tool schema JSON, and a decode
// function that unmarshals the model response into T.
func splashAIPrompt[T any](
	opts PromptOptions,
	toolsJSON []byte,
	decode func([]byte) (T, error),
) (T, error) {
	var zero T
	adapter, err := _splashGetProvider(opts.Provider)
	if err != nil {
		return zero, err
	}
	serializedTools, err := adapter.SerializeTools(toolsJSON)
	if err != nil {
		return zero, fmt.Errorf("serializing tools for provider %q: %w", opts.Provider, err)
	}
	resp, err := adapter.Chat(_SplashChatRequest{
		Model:       opts.Model,
		System:      opts.System,
		Input:       opts.Input,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
		Tools:       serializedTools,
	})
	if err != nil {
		return zero, err
	}
	return decode([]byte(resp.Content))
}
`
```

- [ ] **Step 4: Emit the preamble in `codegen.go` when `use std/ai` is present**

In `internal/codegen/codegen.go`, find where `fmt` import is added to the preamble (currently for `splashApprove`). Add AI preamble detection:

```go
// After existing preamble logic, add:
if e.usesStdAI(file) {
    e.imports["fmt"] = true
    out.WriteString(aiPreamble)
}
```

Add the `usesStdAI` helper on `*Emitter`:

```go
func (e *Emitter) usesStdAI(file *ast.File) bool {
    for _, u := range file.Uses {
        if u == "std/ai" {
            return true
        }
    }
    return false
}
```

Check `ast.File` for the `Uses` field name — it may be `Uses []string` or `Imports`. Read `internal/ast/ast.go` to confirm before writing.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/codegen/... -run TestAIPreamble -v
```

Expected: PASS

- [ ] **Step 6: Run full codegen test suite**

```bash
go test ./internal/codegen/... -v
```

Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/codegen/ai_preamble.go internal/codegen/codegen.go internal/codegen/codegen_test.go
git commit --no-gpg-sign -m "feat: emit std/ai runtime preamble (registry, AIAdapter, splashAIPrompt)"
```

---

## Task 2: OpenAIAdapter

**Files:**
- Modify: `internal/codegen/ai_preamble.go` (append OpenAIAdapter Go source to `aiPreamble` const)
- Modify: `internal/codegen/codegen_test.go`

The `OpenAIAdapter` handles all OpenAI-compatible endpoints. The wire format:

**Request** (`POST {BaseURL}/chat/completions`):
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "..."},
    {"role": "user", "content": "..."}
  ],
  "tools": [...],
  "temperature": 0.1,
  "max_tokens": 256
}
```

**Response** (tool call path):
```json
{
  "choices": [{
    "message": {
      "tool_calls": [{"function": {"name": "...", "arguments": "..."}}]
    }
  }]
}
```

**Response** (text path):
```json
{
  "choices": [{"message": {"content": "..."}}]
}
```

- [ ] **Step 1: Write failing test**

Add to `internal/codegen/codegen_test.go`:

```go
func TestAIPreamble_ContainsOpenAIAdapter(t *testing.T) {
	src := `
module demo
use std/ai
fn dummy() -> String { return "x" }
`
	out := emitSrc(src)
	if !strings.Contains(out, "OpenAIAdapter") {
		t.Errorf("expected OpenAIAdapter in AI preamble:\n%s", out)
	}
	if !strings.Contains(out, "AnthropicAdapter") {
		t.Errorf("expected AnthropicAdapter in AI preamble:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/codegen/... -run TestAIPreamble_ContainsOpenAIAdapter -v
```

Expected: FAIL

- [ ] **Step 3: Append adapter implementations to `aiPreamble` in `ai_preamble.go`**

Append to the `aiPreamble` constant (add before the closing backtick):

```go
// ── OpenAIAdapter ─────────────────────────────────────────────────────────────
// Handles all OpenAI-compatible endpoints: OpenAI, Azure OpenAI, Together,
// Groq, LM Studio, Ollama, and any other service that speaks the OpenAI
// chat completions wire format.

// OpenAIAdapter implements AIAdapter for the OpenAI chat completions API.
type OpenAIAdapter struct {
	BaseURL string // e.g. "https://api.openai.com/v1"
	Token   string // API key — passed as Bearer token
}

func (a OpenAIAdapter) SerializeTools(canonicalJSON []byte) ([]byte, error) {
	// canonical is Anthropic format: [{name, description, input_schema}]
	// OpenAI format:                 [{type:"function", function:{name, description, parameters}}]
	type anthSchema struct {
		Type        string                 `json:"type"`
		Properties  map[string]interface{} `json:"properties"`
		Required    []string               `json:"required,omitempty"`
	}
	type anthTool struct {
		Name        string     `json:"name"`
		Description string     `json:"description,omitempty"`
		InputSchema anthSchema `json:"input_schema"`
	}
	type oaiFunction struct {
		Name        string     `json:"name"`
		Description string     `json:"description,omitempty"`
		Parameters  anthSchema `json:"parameters"`
	}
	type oaiTool struct {
		Type     string      `json:"type"`
		Function oaiFunction `json:"function"`
	}
	var tools []anthTool
	if err := json.Unmarshal(canonicalJSON, &tools); err != nil {
		return nil, err
	}
	out := make([]oaiTool, len(tools))
	for i, t := range tools {
		out[i] = oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return json.Marshal(out)
}

func (a OpenAIAdapter) Chat(req _SplashChatRequest) (_SplashChatResponse, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := map[string]interface{}{
		"model": req.Model,
		"messages": []message{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.Input},
		},
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
	}
	if len(req.Tools) > 0 {
		var tools interface{}
		if err := json.Unmarshal(req.Tools, &tools); err != nil {
			return _SplashChatResponse{}, err
		}
		body["tools"] = tools
	}
	b, err := json.Marshal(body)
	if err != nil {
		return _SplashChatResponse{}, err
	}
	httpReq, err := http.NewRequest("POST", a.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return _SplashChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.Token)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return _SplashChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return _SplashChatResponse{}, fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return _SplashChatResponse{}, err
	}
	if len(result.Choices) == 0 {
		return _SplashChatResponse{}, fmt.Errorf("OpenAI returned no choices")
	}
	msg := result.Choices[0].Message
	var toolCalls []_SplashToolCall
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, _SplashToolCall{
			Name:      tc.Function.Name,
			InputJSON: []byte(tc.Function.Arguments),
		})
	}
	return _SplashChatResponse{Content: msg.Content, ToolCalls: toolCalls}, nil
}

// ── AnthropicAdapter ──────────────────────────────────────────────────────────

// AnthropicAdapter implements AIAdapter for the Anthropic messages API.
type AnthropicAdapter struct {
	BaseURL string // e.g. "https://api.anthropic.com"
	Token   string // API key — passed as x-api-key header
}

func (a AnthropicAdapter) SerializeTools(canonicalJSON []byte) ([]byte, error) {
	// canonical is already Anthropic format — pass through unchanged
	return canonicalJSON, nil
}

func (a AnthropicAdapter) Chat(req _SplashChatRequest) (_SplashChatResponse, error) {
	body := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"system":     req.System,
		"messages": []map[string]string{
			{"role": "user", "content": req.Input},
		},
	}
	if len(req.Tools) > 0 {
		var tools interface{}
		if err := json.Unmarshal(req.Tools, &tools); err != nil {
			return _SplashChatResponse{}, err
		}
		body["tools"] = tools
	}
	b, err := json.Marshal(body)
	if err != nil {
		return _SplashChatResponse{}, err
	}
	httpReq, err := http.NewRequest("POST", a.BaseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return _SplashChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.Token)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return _SplashChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return _SplashChatResponse{}, fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Name  string `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return _SplashChatResponse{}, err
	}
	var text string
	var toolCalls []_SplashToolCall
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			text += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, _SplashToolCall{
				Name:      block.Name,
				InputJSON: []byte(block.Input),
			})
		}
	}
	return _SplashChatResponse{Content: text, ToolCalls: toolCalls}, nil
}
```

Also add the missing imports to the preamble header:

```go
// At the top of aiPreamble, after the comment, add these imports inline:
// (The emitter adds "fmt", "net/http", "encoding/json", "bytes", "io" to e.imports)
```

In `codegen.go`, in `usesStdAI` / preamble emission, add:

```go
if e.usesStdAI(file) {
    e.imports["fmt"] = true
    e.imports["net/http"] = true
    e.imports["encoding/json"] = true
    e.imports["bytes"] = true
    e.imports["io"] = true
    out.WriteString(aiPreamble)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/codegen/... -run TestAIPreamble -v
```

Expected: all `TestAIPreamble_*` tests PASS

- [ ] **Step 5: Full suite**

```bash
go test ./...
```

Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/codegen/ai_preamble.go internal/codegen/codegen.go internal/codegen/codegen_test.go
git commit --no-gpg-sign -m "feat: OpenAIAdapter and AnthropicAdapter in std/ai preamble"
```

---

## Task 3: Rewrite `ai.prompt<T>` call sites in codegen

**Files:**
- Modify: `internal/codegen/expr.go`
- Modify: `internal/codegen/codegen_test.go`

Currently `ai.prompt<FraudInsight>(opts)` emits as `ai.prompt(opts)` — broken Go. It needs to emit as:

```go
splashAIPrompt[FraudInsight](opts, _splashToolSchemas, func(b []byte) (FraudInsight, error) {
    var v FraudInsight
    return v, json.Unmarshal(b, &v)
})
```

The `_splashToolSchemas` variable is emitted in Task 4. For now, emit `[]byte(nil)` as a placeholder so the code compiles.

- [ ] **Step 1: Write failing test**

Add to `internal/codegen/codegen_test.go`:

```go
func TestAIPrompt_EmitsCallToSplashAIPrompt(t *testing.T) {
	src := `
module demo
use std/ai

type Insight { summary: String }

type PromptOptions {
  provider:    String
  model:       String
  input:       String
  system:      String
  temperature: Float
  max_tokens:  Int
  budget:      Float
}

@tool
fn dummy_tool(query: String) -> String { return query }

fn run(q: String) needs Agent, AI -> Insight {
  return ai.prompt<Insight>(PromptOptions{
    provider: "openai",
    model: "gpt-4o",
    input: q,
    system: "You are helpful.",
    temperature: 0.1,
    max_tokens: 256,
    budget: 0.10,
  })
}
`
	out := emitSrc(src)
	if !strings.Contains(out, "splashAIPrompt[Insight]") {
		t.Errorf("expected splashAIPrompt[Insight] call, got:\n%s", out)
	}
	if strings.Contains(out, "ai.prompt") {
		t.Errorf("expected ai.prompt to be rewritten, still present:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/codegen/... -run TestAIPrompt_EmitsCallToSplashAIPrompt -v
```

Expected: FAIL — `ai.prompt` still in output

- [ ] **Step 3: Detect and rewrite `ai.prompt<T>` in `emitCallExpr`**

In `internal/codegen/expr.go`, `emitCallExpr` currently handles `println` specially. Add a case for `ai.prompt`:

```go
func (e *Emitter) emitCallExpr(ex *ast.CallExpr) string {
    var args []string
    for _, arg := range ex.Args {
        args = append(args, e.emitExprStr(arg))
    }
    if ident, ok := ex.Callee.(*ast.Ident); ok && ident.Name == "println" {
        e.imports["fmt"] = true
        return fmt.Sprintf("fmt.Println(%s)", strings.Join(args, ", "))
    }
    // Rewrite ai.prompt<T>(opts) → splashAIPrompt[T](opts, _splashToolSchemas, decode)
    if member, ok := ex.Callee.(*ast.MemberExpr); ok {
        if ident, ok := member.Object.(*ast.Ident); ok && ident.Name == "ai" && member.Field == "prompt" {
            typeName := "any"
            if len(ex.TypeArgs) > 0 {
                typeName = e.emitTypeName(ex.TypeArgs[0])
            }
            e.imports["encoding/json"] = true
            decode := fmt.Sprintf("func(b []byte) (%s, error) { var v %s; return v, json.Unmarshal(b, &v) }",
                typeName, typeName)
            optsArg := ""
            if len(args) > 0 {
                optsArg = args[0]
            }
            return fmt.Sprintf("splashAIPrompt[%s](%s, _splashToolSchemas, %s)",
                typeName, optsArg, decode)
        }
    }
    return fmt.Sprintf("%s(%s)", e.emitExprStr(ex.Callee), strings.Join(args, ", "))
}
```

Check `ast.CallExpr` for the `TypeArgs` field — it may be named differently. Read `internal/ast/ast.go` to confirm before writing.

Also check `ast.MemberExpr` field names — may be `Object`/`Field` or `Receiver`/`Method`. Confirm in `internal/ast/ast.go`.

- [ ] **Step 4: Emit `_splashToolSchemas` placeholder**

In `internal/codegen/decl.go` or `codegen.go`, when `use std/ai` is present, emit:

```go
var _splashToolSchemas = []byte(nil) // replaced in Task 4
```

This makes the generated code compile even before Task 4.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/codegen/... -run TestAIPrompt -v
```

Expected: PASS

- [ ] **Step 6: Smoke test — `splash emit` on finance example should produce valid Go**

```bash
./splash emit examples/finance/finance.splash 2>&1 | head -5
```

Expected: no errors, output starts with `package finance`

- [ ] **Step 7: Full test suite**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/codegen/expr.go internal/codegen/decl.go internal/codegen/codegen_test.go
git commit --no-gpg-sign -m "feat: rewrite ai.prompt<T> call sites to splashAIPrompt[T]"
```

---

## Task 4: Embed tool schemas in compiled binary

**Files:**
- Modify: `internal/codegen/decl.go`
- Modify: `cmd/splash/main.go`
- Modify: `internal/codegen/codegen_test.go`

When `use std/ai` is present and `@tool` functions exist, replace the `[]byte(nil)` placeholder with the actual serialized tool schemas.

The emitter doesn't have access to `toolschema` today — `internal/codegen` doesn't import `internal/toolschema`. The CLI bridges this gap (same pattern as `ApprovalCallers`): the CLI computes the schema JSON and passes it via `codegen.Options`.

- [ ] **Step 1: Add `ToolSchemasJSON []byte` to `codegen.Options`**

In `internal/codegen/codegen.go`, add to `Options`:

```go
type Options struct {
    ApprovalCallers map[string]bool
    ToolSchemasJSON []byte // pre-serialized canonical tool schema JSON; nil = no tools
}
```

- [ ] **Step 2: Use `ToolSchemasJSON` when emitting `_splashToolSchemas`**

Replace the `[]byte(nil)` placeholder emission with:

```go
// When emitting _splashToolSchemas:
if len(e.opts.ToolSchemasJSON) > 0 {
    // Escape backticks in JSON (shouldn't occur but be safe)
    escaped := strings.ReplaceAll(string(e.opts.ToolSchemasJSON), "`", "`+\"`\"+`")
    out.WriteString(fmt.Sprintf("var _splashToolSchemas = []byte(`%s`)\n\n", escaped))
} else {
    out.WriteString("var _splashToolSchemas = []byte(nil)\n\n")
}
```

- [ ] **Step 3: Compute and pass `ToolSchemasJSON` in CLI**

In `cmd/splash/main.go`, in `runEmit` and `runBuild`, after the call graph is built:

```go
// After: g := callgraph.Build(f)
var toolSchemasJSON []byte
if hasStdAI(f) {
    agentReachable := g.Reachable(g.AgentRoots())
    schemas := toolschema.ExtractReachable(f, agentReachable)
    if schemas == nil {
        schemas = []toolschema.ToolSchema{}
    }
    toolSchemasJSON, _ = toolschema.Serialize(schemas, toolschema.FormatAnthropic) // canonical = anthropic
}

// Pass to codegen:
codegen.Options{
    ApprovalCallers: g.Callers(collectApproveFns(f)),
    ToolSchemasJSON: toolSchemasJSON,
}
```

Add `hasStdAI` helper:

```go
func hasStdAI(f *ast.File) bool {
    for _, u := range f.Uses {
        if u == "std/ai" {
            return true
        }
    }
    return false
}
```

- [ ] **Step 4: Write test**

Add to `internal/codegen/codegen_test.go`:

```go
func TestAISchemaEmbedding_PresentWhenToolsExist(t *testing.T) {
	src := `
module demo
use std/ai
/// Search for items.
@tool
fn search(query: String) needs DB.read -> String { return query }
fn run(q: String) needs Agent, DB.read, AI -> String {
  return ai.prompt<String>(PromptOptions{provider: "openai", model: "gpt-4o", input: q, system: "", temperature: 0.1, max_tokens: 100, budget: 0.01})
}
`
	schemas := []toolschema.ToolSchema{{Name: "search", Description: "Search for items."}}
	schemaJSON, _ := toolschema.Serialize(schemas, toolschema.FormatAnthropic)
	out := emitSrcWithOptions(src, codegen.Options{ToolSchemasJSON: schemaJSON})
	if !strings.Contains(out, `_splashToolSchemas`) {
		t.Errorf("expected _splashToolSchemas in output:\n%s", out)
	}
	if !strings.Contains(out, `"search"`) {
		t.Errorf("expected schema name in embedded JSON:\n%s", out)
	}
}
```

Note: `emitSrcWithOptions` is a test helper that calls `Emit(file, opts)` — add it alongside the existing `emitSrc` and `emitSrcWithApproval` helpers.

- [ ] **Step 5: Run tests and full suite**

```bash
go test ./internal/codegen/... -run TestAISchema -v
go test ./...
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/codegen/codegen.go internal/codegen/decl.go cmd/splash/main.go internal/codegen/codegen_test.go
git commit --no-gpg-sign -m "feat: embed tool schemas as _splashToolSchemas in compiled binary"
```

---

## Task 5: Add `provider` field to `PromptOptions` — type checker

**Files:**
- Modify: `internal/typechecker/typechecker.go`
- Modify: `internal/typechecker/typechecker_test.go`

Currently `PromptOptions` is user-defined. The typechecker knows about `std/ai` and injects `AIError` and the `ai` namespace. It should also validate that `PromptOptions` — when used with `ai.prompt` — contains a `provider` field of type `String`.

> **Scope note:** This task does NOT move `PromptOptions` to a built-in type. Users still define it. The typechecker adds a check: if `ai.prompt<T>(opts)` is called and `opts` is typed as a struct with a field named `provider`, it must be `String`. If `provider` is absent, emit a warning (not an error) for now — existing examples don't have it yet. See Open Question Q3 before making this an error.

- [ ] **Step 1: Write failing test**

Add to `internal/typechecker/typechecker_test.go`:

```go
func TestStdAI_PromptOptions_ProviderFieldAccepted(t *testing.T) {
    src := `
module demo
use std/ai
type PromptOptions {
  provider:    String
  model:       String
  input:       String
  system:      String
  temperature: Float
  max_tokens:  Int
  budget:      Float
}
fn run(q: String) needs Agent, AI -> String {
  return ai.prompt<String>(PromptOptions{
    provider: "openai", model: "gpt-4o", input: q,
    system: "", temperature: 0.1, max_tokens: 100, budget: 0.01,
  })
}
`
    _, diags := typecheck(src)
    if hasError(diags) {
        t.Errorf("expected no errors with provider field present, got: %v", diags)
    }
}
```

- [ ] **Step 2: Run test to confirm it passes already (or fails — establish baseline)**

```bash
go test ./internal/typechecker/... -run TestStdAI_PromptOptions -v
```

- [ ] **Step 3: Update finance and ai_prompt examples to add `provider` field**

`examples/finance/finance.splash` — add `provider: String` to `PromptOptions` type and `provider: "openai"` (or appropriate) to all `ai.prompt` call sites.

`examples/ai_prompt/ai_prompt.splash` — same.

- [ ] **Step 4: Verify examples check clean**

```bash
./splash check examples/finance/finance.splash
./splash check examples/ai_prompt/ai_prompt.splash
```

Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/typechecker/typechecker.go internal/typechecker/typechecker_test.go \
        examples/finance/finance.splash examples/ai_prompt/ai_prompt.splash
git commit --no-gpg-sign -m "feat: add provider field to PromptOptions in std/ai examples"
```

---

## Task 6: `@budget` runtime enforcement — REQUIRES SUB-PLAN

> This task depends on Tasks 1–5 being complete. The runtime call counter must be injected into `splashAIPrompt` calls. Write a dedicated sub-plan at `docs/superpowers/plans/YYYY-MM-DD-budget-enforcement.md` before implementing.

Outline for sub-plan:
- Add `_splashBudgetCounters map[string]*_BudgetCounter` to the preamble
- `splashAIPrompt` increments counter on each call; returns `BudgetExceeded` error when `max_calls` hit
- Cost tracking requires the provider to return token usage — add `TokensUsed int` to `_SplashChatResponse`
- Both `OpenAIAdapter` and `AnthropicAdapter` populate `TokensUsed` from their response (`usage.total_tokens`)
- `max_cost` enforcement: accumulate `cost_per_token * tokens_used` (provider-specific rates, configurable on adapter)

---

## Task 7: Tool call dispatch round-trip — REQUIRES SUB-PLAN

> Write a dedicated sub-plan. This is the loop: AI returns tool call → invoke Splash `@tool` fn → re-submit result → repeat until text response.

Outline:
- `splashAIPrompt` checks `resp.ToolCalls` after each `Chat` call
- If tool calls present: dispatch each via a generated `_splashDispatchTool(name, inputJSON)` function
- `_splashDispatchTool` is a generated switch statement over all `@tool` functions in the file
- Loop until no tool calls or `max_calls` exceeded
- Error if tool name is unknown (model hallucinated a tool name)

---

## Self-Review Checklist

**Spec coverage:**
- [x] Provider registry (`RegisterProvider`, `_splashGetProvider`)
- [x] `AIAdapter` interface with `SerializeTools` + `Chat`
- [x] `OpenAIAdapter` — OpenAI-compatible wire format
- [x] `AnthropicAdapter` — Anthropic messages wire format
- [x] `ai.prompt<T>` → `splashAIPrompt[T]` codegen rewrite
- [x] Tool schema embedding as `_splashToolSchemas`
- [x] `provider` field in `PromptOptions` examples
- [x] `@budget` and tool dispatch flagged as follow-on sub-plans

**Open Questions to resolve before Task 5:**
- Q3: `PromptOptions` as built-in vs user-defined — read before touching typechecker

**Not in this plan (out of scope):**
- Streaming (`Stream<T>` return type)
- `splash build --format` for runtime schema selection (provider adapter handles this)
- `std/db`, `std/http` stdlib adapters
