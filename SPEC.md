# Splash Language Specification

**Version:** 0.1  
**Status:** Working draft of the intended language. See [implementation status](README.md#implementation-status) for current conformance and known gaps.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Lexical Structure](#2-lexical-structure)
3. [Grammar](#3-grammar)
4. [Type System](#4-type-system)
5. [Effect System](#5-effect-system)
6. [Annotations](#6-annotations)
7. [Data Classification](#7-data-classification)
8. [Module System](#8-module-system)
9. [Standard Library](#9-standard-library)
10. [Compilation Pipeline](#10-compilation-pipeline)
11. [Go Code Generation](#11-go-code-generation)

---

## 1. Overview

Splash is a statically-typed, compiled control-plane language that transpiles to Go. It is designed for software whose authority must be statically understood before it runs, especially AI agent systems and other autonomous control software.

The compiler models authority with declared and inferred effects, resolved calls, and selected type-classification checks. Source verification applies these checks to the selected entry and its loaded imports before artifact production. [Implementation status](README.md#implementation-status)

Go lowering, native compilation, and VM admission each have their own supported forms and can fail after source verification succeeds. Generated approval gates depend on the runtime's configured adapter. [Supported Go subset](docs/design/supported-subset.md) · [VM subset and limits](docs/design/splash-ir-and-vm.md#public-vm-execution)

This working draft describes the v0.1 language design and identifies current implementation limits for statically governed control software.

---

## 2. Lexical Structure

### 2.1 Comments

```
// single-line comment
/// doc comment — attached to the next declaration or parameter
```

`///` comments appearing immediately before a function declaration or inside a parameter list are parsed and attached to the AST. They appear in JSON Schema output from `splash tools`.

### 2.2 Keywords

```
fn       type     enum     constraint   module   expose   use
let      return   if       else         guard    for      in
match    needs    none     static       override where
async    await    try      tool         approve  agent
true     false
```

### 2.3 Identifiers

```
ident ::= [a-zA-Z_][a-zA-Z0-9_]*
```

Type names and enum variants conventionally begin with an uppercase letter. The parser uses this convention to disambiguate struct literals from block statements.

### 2.4 Literals

| Kind    | Examples                      |
|---------|-------------------------------|
| Integer | `0`, `42`, `-1`               |
| Float   | `3.14`, `0.5`, `-2.0`         |
| String  | `"hello"`, `""`               |
| Bool    | `true`, `false`               |
| None    | `none`                        |

Strings use double quotes. No escape sequences beyond what Go's `strconv.ParseFloat` / `strconv.ParseInt` accept are defined in the current lexer.

### 2.5 Operators

| Operator | Meaning            | Precedence |
|----------|--------------------|------------|
| `=`      | Assignment         | 1          |
| `\|\|`   | Logical or         | 2          |
| `&&`     | Logical and        | 3          |
| `==`     | Equal              | 4          |
| `!=`     | Not equal          | 4          |
| `<`      | Less than          | 5          |
| `<=`     | Less or equal      | 5          |
| `>`      | Greater than       | 5          |
| `>=`     | Greater or equal   | 5          |
| `??`     | Null coalesce      | 6          |
| `+`      | Add                | 6          |
| `-`      | Subtract           | 6          |
| `*`      | Multiply           | 7          |
| `/`      | Divide             | 7          |
| `%`      | Modulo             | 7          |
| `!`      | Logical not (unary)| 8          |
| `-`      | Negate (unary)     | 8          |
| `f()`    | Call               | 9          |
| `a[i]`   | Index              | 9          |
| `.`      | Member access      | 10         |
| `?.`     | Optional chaining  | 10         |

### 2.6 Punctuation

`( ) { } [ ] , : ; -> => ? @ ...`

Semicolons are optional statement separators; the parser silently skips them. Newlines act as statement terminators in practice.

---

## 3. Grammar

Grammar notation: `::=` defines a production; `[x]` is optional; `x*` is zero or more; `x+` is one or more; `x | y` is alternation; `"x"` is a literal token.

### 3.1 File

```
file ::= [module_decl] [expose_decl] use_decl* declaration*

module_decl  ::= "module" ident
expose_decl  ::= "expose" ident ("," ident)*
use_decl     ::= "use" use_path

use_path ::= ident ("/" ident)*   -- resolves to sibling .splash file
           | "std" "/" ident      -- standard library module
```

Imported modules are available by namespace (`billing.charge_customer()`). The current compiler also preserves flat symbol injection for compatibility, but namespaced access is the preferred v0.2 style.

### 3.2 Declarations

```
declaration ::= annotation* function_decl
              | annotation* type_decl
              | annotation* enum_decl
              | annotation* constraint_decl
```

### 3.3 Annotations

```
annotation ::= "@" ident
             | "@" ident "(" annotation_args ")"

annotation_args ::= annotation_arg ("," annotation_arg)*
annotation_arg  ::= ident ":" annotation_value

annotation_value ::= expr
                   | "[" annotation_value ("," annotation_value)* "]"
```

Annotation argument values that are identifiers with dots (e.g. `DB.read`) are parsed as effect names. The `allow` and `deny` keys in `@sandbox` expect a list of effect names.

### 3.4 Functions

```
function_decl ::= function_modifiers "fn" ident ["<" type_params ">"]
                  "(" params ")" ["needs" effect_list] ["->" type_expr]
                  block_stmt

function_modifiers ::= ("async" | "tool" | "approve" | "agent")*

type_params ::= type_param ("," type_param)*
type_param  ::= ident [":" constraint_bound ("+" constraint_bound)*]

constraint_bound ::= ident

params ::= param ("," param)*
param  ::= ["///doc"] ["..."] ident ":" type_expr ["=" expr]
```

The `...` prefix marks a variadic parameter. The `///doc` comment immediately before a parameter name is attached to that parameter.

### 3.5 Effects

```
effect_list ::= effect ("," effect)*
effect      ::= ident ["." ident]   -- e.g. "DB", "DB.read", "DB.write"
```

See [Section 5](#5-effect-system) for the complete effect vocabulary.

### 3.6 Types

```
type_expr ::= qualified_ident                -- named type: Int, String, billing.Charge
            | qualified_ident "<" type_args ">" -- generic: List<String>, Result<T, E>
            | type_expr "?"                  -- optional: String?, Int?
            | "fn" "(" type_list ")" ["->" type_expr]  -- function type
            | type_expr "|" type_expr        -- union (parsed, limited typechecker support)

qualified_ident ::= ident ("." ident)*

type_args ::= type_expr ("," type_expr)*
type_list ::= type_expr ("," type_expr)*
```

### 3.7 Type Declarations

```
type_decl ::= "type" ident ["<" type_params ">"] "{" field_decl* "}"

field_decl ::= annotation* ident ":" type_expr ["=" expr]
```

Fields are separated by newlines (semicolons also accepted). Field order is significant for JSON Schema generation.

### 3.8 Enum Declarations

```
enum_decl ::= "enum" ident "{" enum_variant* "}"

enum_variant ::= ident              -- unit variant: None
               | ident "(" type_expr ")"  -- tuple variant: Some(T)
```

### 3.9 Constraint Declarations

```
constraint_decl ::= "constraint" ident ["<" type_params ">"] "{" constraint_method* "}"

constraint_method ::= ["static"] "fn" ident "(" ["self" [","]] params ")" ["->" type_expr]
```

`Loggable` is a built-in constraint — no declaration needed. Unknown constraint names in type parameters are compile errors.

### 3.10 Statements

```
block_stmt ::= "{" stmt* "}"

stmt ::= return_stmt
       | let_stmt
       | if_stmt
       | guard_stmt
       | for_stmt
       | assign_stmt
       | expr_stmt

return_stmt ::= "return" [expr]
let_stmt    ::= "let" ident [":" type_expr] "=" expr
if_stmt     ::= "if" expr block_stmt ["else" (if_stmt | block_stmt)]
guard_stmt  ::= "guard" expr "else" block_stmt
for_stmt    ::= "for" ident "in" expr block_stmt
assign_stmt ::= expr "=" expr
expr_stmt   ::= expr
```

### 3.11 Expressions

```
expr ::= literal
       | ident
       | struct_literal
       | list_literal
       | match_expr
       | unary_expr
       | binary_expr
       | call_expr
       | generic_call_expr
       | member_expr
       | optional_chain_expr
       | index_expr
       | null_coalesce_expr
       | "(" expr ")"

literal ::= INT | FLOAT | STRING | "true" | "false" | "none"

struct_literal ::= UpperIdent "{" struct_field ("," struct_field)* "}"
struct_field   ::= ident ":" expr

list_literal ::= "[" (expr ("," expr)*)? "]"

match_expr ::= "match" expr "{" match_arm ("," match_arm)* "}"
match_arm  ::= expr "=>" expr

unary_expr    ::= ("!" | "-") expr
binary_expr   ::= expr binary_op expr
call_expr     ::= expr "(" (expr ("," expr)*)? ")"
generic_call_expr ::= expr "<" type_args ">" "(" (expr ("," expr)*)? ")"
member_expr   ::= expr "." ident
optional_chain_expr ::= expr "?." ident
index_expr    ::= expr "[" expr "]"
null_coalesce_expr ::= expr "??" expr

binary_op ::= "=" | "||" | "&&" | "==" | "!=" | "<" | "<=" | ">" | ">="
            | "??" | "+" | "-" | "*" | "/" | "%"
```

**Struct literals** are only parsed when the identifier begins with an uppercase letter. This disambiguates `Foo { ... }` (struct literal) from a block following an identifier expression.

Each record declaration defines a nominal type. A record from one module is distinct from a record with the same name and fields declared in another module. An import alias refers to the original declaration. A record literal evaluates its field initializers exactly once, in the order they appear in source.

**Generic calls** use lookahead to distinguish `f<T>(x)` (generic call) from `f < T` (comparison). The lookahead checks for `< Ident[?] [, Ident[?]]* > (`.

---

## 4. Type System

### 4.1 Built-in Types

| Type         | Description                                    |
|--------------|------------------------------------------------|
| `Int`        | Signed 64-bit integer                          |
| `Float`      | 64-bit floating point                          |
| `String`     | UTF-8 string                                   |
| `Bool`       | Boolean                                        |
| `Void`       | No return value                                |
| `List<T>`    | Ordered sequence                               |
| `Map<K, V>`  | Key-value map                                  |
| `Option<T>`  | Optional value (also written `T?`)             |
| `Result<T, E>` | Success or error (used by `std/ai`)          |

`T?` is syntactic sugar for `Option<T>`. The type checker treats them identically.

`Int` arithmetic checks overflow and reports an execution error when the result is out of range. Division truncates toward zero. Remainder is zero or has the dividend's sign. Division and remainder by zero report an error. Dividing the minimum integer by minus one and negating the minimum integer overflow. Its remainder by minus one is zero. Operands and call arguments evaluate from left to right. Numeric operations do not implicitly convert operand types.

The experimental VM implements checked arithmetic. The transitional Go backend still lowers `Int` to Go `int` and can promote mixed numeric constants. Differential tests compare successful executions in the shared source subset where intermediate values fit both backends. [Integer semantics decision](docs/decisions/2026-11-27-checked-integer-semantics.md)

### 4.2 Generic Functions

Type parameters are declared with `<T>` syntax. Constraints are declared with `:`:

```splash
fn log<T: Loggable>(value: T) -> Void { ... }
fn first<T>(list: List<T>) -> T? { ... }
fn map_result<T, E>(r: Result<T, E>, f: fn(T) -> T) -> Result<T, E> { ... }
```

Multiple constraints use `+`:

```splash
fn process<T: Loggable + Serializable>(value: T) -> String { ... }
```

Constraint names must be declared with `constraint` or be the built-in `Loggable`. Unknown constraint names are compile errors.

### 4.3 Optional Types

`T?` denotes an optional. The `none` keyword is the null value. The null-coalescing operator `??` provides a default:

```splash
fn find_user(id: Int) -> User? { ... }
let name: String = find_user(42)?.name ?? "unknown"
```

Optional chaining `?.` short-circuits to `none` if the left side is `none`.

### 4.4 Type Inference

The type checker infers types for `let` bindings when no annotation is given. Explicit annotations are always accepted.

### 4.5 Effect Propagation

For resolved named calls, the declared compatibility rule in [§5.3](#53-effect-checking) carries capability requirements along a call chain. The separate implementation conformance rule propagates effects from modeled operations.

---

## 5. Effect System

### 5.1 Effect Declaration

```splash
fn fetch(id: Int) needs DB.read -> Report { ... }
fn summarize(id: Int) needs DB.read, AI -> String { return fetch(id) }
```

Effects after `needs` are comma-separated. A function with no `needs` clause requires no effects.

### 5.2 Effect Vocabulary

| Effect      | Meaning                                              |
|-------------|------------------------------------------------------|
| `DB.read`   | Read from database                                   |
| `DB.write`  | Write to database                                    |
| `DB.admin`  | Schema-level database operations                     |
| `Net`       | Outbound network calls                               |
| `AI`        | Calls to AI model providers                          |
| `Agent`     | Agent entry point (structural — not a capability)    |
| `FS`        | Filesystem access                                    |
| `Exec`      | Subprocess execution                                 |
| `Cache`     | Cache read/write                                     |
| `Secrets`   | Access to secrets store                              |
| `Queue`     | Message queue operations                             |
| `Metric`    | Metrics emission                                     |

`Agent` is structural: it marks an entry point for call graph analysis and is not subject to `@sandbox` allow/deny constraints.

`DB` is shorthand for `DB.read, DB.write`. `DB.admin` is a separate effect. Unknown labels in `needs` and `@sandbox` effect lists are errors.

### 5.3 Effect Checking

For a resolved direct or qualified call, source verification requires the caller's declared capabilities to cover the callee's declared capabilities. This also applies to grants the callee's current body leaves unused. The structural `Agent` marker is excluded from this comparison.

```splash
fn bad() -> String {
    return summarize(1)   // error: missing DB.read, AI
}
```

The checker also requires every known inferred effect to be covered by its function's declared capabilities. Inference starts from modeled operations and propagates along resolved calls to a fixed point, including recursive groups. A missing grant diagnostic identifies the modeled operation and one deterministic path to it. The frontend models calls to the imported `std/ai` adapter's `prompt` operation as `AI`. Provider execution is a separate runtime concern. Built-in `println` adds no capability in this vocabulary.

Unresolved calls, unsupported source forms, and unmodeled adapter operations leave effect coverage incomplete for the affected function and its callers. Incomplete summaries retain known inferred effects. `splash effects` reports them separately from the unknown remainder, including on source that fails policy verification. Complete summaries may also list declared grants that neither the implementation nor resolved callee contracts require. Narrowing a declaration remains a contract change.

`check`, `emit`, `build`, `tools`, and `run --backend vm` share the source policy check. Required source coverage includes functions declaring effects, agent and tool entry points, sandboxed functions, and their transitive calls. When the entry closure contains an approval declaration, source call coverage is also required throughout that closure for approval error propagation. An uncovered required source operation makes verification indeterminate and blocks these commands. A known violation takes precedence and the compiler still reports any uncovered required evidence.

The compiler-injected Go approval adapter is a trusted host boundary for these source checks. Its effects remain unmodeled in `splash effects`. The checker still requires coverage of the Splash source around it. The VM has its own admission requirements. Ordinary callbacks outside the required policy scopes can still compile, with incomplete summaries available for inspection.

`splash check --json` reports `effectCoverage.status` across all analyzed function summaries, including unmodeled approval adapter effects. A scoped `verified` verdict can accompany `incomplete` effect coverage. [Check JSON v1](docs/design/semantic-compiler-interface.md#check-json-v1)

### 5.4 Effect Propagation Through `approve fn`

The Go backend adds an error return to approval-gated functions and their resolved transitive callers. Source verification requires calls on these paths to occupy a standalone statement or the entire value of a `let` or `return` statement. See [§11.4](#114-the-agent-error-boundary).

---

## 6. Annotations

Annotations appear immediately before declarations. Multiple annotations may be stacked.

### 6.1 `tool fn`

```splash
tool fn search_catalog(query: String, limit: Int) needs DB.read -> List<SearchResult> { ... }
```

Marks a function as a tool candidate and an agent root for source analysis. `splash tools` emits JSON Schema for `tool fn` parameters from their resolved callable signatures. It supports scalar types, optionals, lists, nonempty payload-free enums, and nominal records projected as objects. Unresolved parameter types and unsupported parameter shapes fail schema export. Source verification checks tool return classification under [§7.2](#72-compile-time-enforcement).

### 6.2 `redline fn`

```splash
@reason "Bulk deletion requires DBA approval"
redline fn delete_all_data() needs DB.admin -> Void { ... }
```

Source verification rejects a resolved call path from an agent root to a `redline fn`. The diagnostic includes one such path. The verifier requires source call coverage in the agent scope as described in [§5.3](#53-effect-checking).

`@reason "..."` may precede `redline fn` to attach a human-readable justification that the compiler preserves in declaration metadata.

### 6.3 `approve fn`

```splash
approve fn charge_card(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }
```

The Go backend inserts an approval check before the function body and propagates denial as an error through supported resolved calls. At runtime, the registered `ApprovalAdapter` decides whether the body can run. The default adapter prompts on standard input. Slack and webhook adapters remain proposed. The experimental VM rejects approval declarations at admission. [Go error propagation](#114-the-agent-error-boundary)

### 6.4 `@agent_allowed`

```splash
@agent_allowed
fn read_public_catalog() needs DB.read -> List<Item> { ... }
```

Grants explicit access under `@containment(agent: "approved_only")`. It also satisfies the separate rule requiring `approve fn` or `@agent_allowed` on agent-reachable functions declaring both `DB.write` and `Net`.

### 6.5 `@containment`

Module-level annotation. Applied to the module declaration:

```splash
@containment(agent: "approved_only")
module billing
```

A module may declare containment once. The annotation requires exactly one argument, `agent`, whose value must be one of the three string literals below. The compiler rejects malformed, repeated, or misplaced containment annotations.

| Value           | Meaning                                                    |
|-----------------|------------------------------------------------------------|
| `"none"`        | No agent root or agent-reachable function is allowed in this module |
| `"read_only"`   | Agent-reachable functions may declare at most `DB.read` capability |
| `"approved_only"` | Agent-reachable calls require `approve fn` or `@agent_allowed` on the target |

Containment applies to each declaration in its owning module, including when imported. Under `approved_only`, a function marked `Agent` may serve as a standalone root. Any resolved call to that root from an agent-reachable caller, including a recursive call, requires the same permission as other calls. A tool-only root requires explicit permission to be exposed under `approved_only`.

### 6.6 `@sandbox`

```splash
@sandbox(allow: [DB.read, Net])
fn search_agent() needs Agent, DB.read, Net -> List<Result> { ... }
```

The verifier checks the declared effects of the annotated function and its resolved transitive callees against the supplied `allow` and `deny` lists. It rejects any denied effect or any effect outside a supplied allow list. Required call and effect coverage in this scope follows [§5.3](#53-effect-checking).

`Agent` is excluded from `@sandbox` constraint checking. The `allow` and `deny` keys accept effect names in the same form as `needs` declarations.

### 6.7 `@budget`

```splash
@budget(max_cost: 0.10, max_calls: 5)
fn run_agent() needs Agent, AI -> Result<Report, AIError> { ... }
```

Declares resource limits. At compile time, argument types are validated:
- `max_cost` must be a numeric literal (`Float` or `Int`).
- `max_calls` must be an integer literal.

Runtime budget enforcement (counter injection into `ai.prompt` calls) is not yet implemented.

### 6.8 `@sensitive`

Applied to struct fields:

```splash
type User {
    id:    Int
    @sensitive email: String
    name:  String
}
```

A `@sensitive` field elevates the containing type's classification for the supported tool return and logging checks in [§7.2](#72-compile-time-enforcement).

### 6.9 `@restricted`

Applied to struct fields:

```splash
type Config {
    @restricted db_url: String
}
```

`@restricted` is the highest classification level. It denotes intended process-local handling. Current source enforcement covers the type checks in [§7.2](#72-compile-time-enforcement). General storage-adapter and classified-data flow enforcement remain proposed.

### 6.10 `@internal`

Applied to struct fields:

```splash
type Report {
    content:   String
    @internal cost_cents: Int
}
```

An `@internal` field elevates the containing type above `public` but below `@sensitive`.

---

## 7. Data Classification

Classification belongs to types. A record's directly annotated fields contribute their maximum classification. Tool return checking also traverses the supported nested types described below.

### 7.1 Classification Lattice

```
public < @internal < @sensitive < @restricted
```

Unannotated fields have classification `public`. A field with multiple classification annotations takes the highest level.

### 7.2 Compile-Time Enforcement

The compiler implements two type-based checks.

1. **Tool return classification.** Starting at the declared return type, the checker follows resolved nongeneric records and their fields, optional and list element types, and both `Result` types. Primitives, void returns, and payload-free enums are supported. Known `@sensitive` or `@restricted` fields reject verification. Missing type evidence and unsupported shapes such as enum payloads, generic records, type parameters, and function types make verification indeterminate in the absence of a known violation. The verifier reports both findings when both occur.

2. **Logging classification.** The checker rejects arguments classified above `@internal` at built-in `println` calls and in supported inference of `Loggable` type parameters. It uses the argument type's classification. Field projections use the underlying field type.

### 7.3 `Loggable`

`Loggable` is a built-in constraint with a maximum classification of `@internal`. The built-in `println` applies the same classification threshold.

---

## 8. Module System

### 8.1 Module Declaration

```splash
module billing
expose charge_customer, Charge
```

`module` declares the module name. `expose` declares which symbols are exported. Unexposed symbols are not injected into importing namespaces.

### 8.2 Importing

```splash
use billing
use std/ai
```

`use billing` resolves to `billing.splash` in the same directory. `use std/ai` loads the standard library `ai` module. Import cycles are detected and rejected with a compile error.

### 8.3 Name Resolution

Exposed symbols are injected flat into the importing namespace. No prefix is required:

```splash
// billing.splash
expose charge_customer

// agent.splash
use billing
fn run() needs Agent, Net -> Charge {
    return charge_customer(1, 100)   // no "billing." prefix
}
```

### 8.4 Multi-File Build

`splash emit` and `splash build` merge all imported files into a single Go package. Declaration order is preserved; cycles are rejected.

---

## 9. Standard Library

### 9.1 `std/ai`

```splash
use std/ai
```

The compiler recognizes this source interface. Provider execution and backend error propagation are proposed runtime behavior.

| Symbol           | Type                                         | Description                        |
|------------------|----------------------------------------------|------------------------------------|
| `ai.prompt<T>`   | `fn(input: String) needs AI -> T`            | Call an AI model, parse response as `T` |
| `AIError`        | `type AIError { message: String }`           | Planned error type for AI call failures |

`ai.prompt<T>` is a generic function call: `ai.prompt<Report>("Summarize this")`.

`ai.prompt<T>` has type `T` in source analysis. The proposed Go runtime propagates failures to the nearest `needs Agent` boundary as the agent entry point's error return. See [Section 11.8](#118-fn-main-and-the-agent-error-boundary).

The `AI` effect is required in `needs` to call `ai.prompt`.

---

## 10. Compilation Pipeline

```
source (.splash)
  → Lexer         tokenize → []token.Token
  → Parser        []token.Token → *ast.File
  → Analyze       types, resolved calls, and inferred effects for entry and imports
  → Verify        applicable source policies → verified program
  → Emitter       verified program → Go source
  → go build      Go source → native binary
```

Source verification issues the program handle used for Go emission, tool schema export, and VM lowering. Each output path has its own requirements.

### 10.1 CLI Commands

| Command                     | Stages run                          |
|-----------------------------|-------------------------------------|
| `splash check <file>`       | Source analysis and verification |
| `splash build <file> -o <out>` | Source verification → Go emission → `go build` |
| `splash emit <file>`        | Source verification → Go emission |
| `splash tools <file>`       | Source verification → tool schema export |
| `splash run <file> --backend vm` | Source verification → VM admission and lowering → bytecode validation and execution |

### 10.2 Agent Roots

The call graph treats any function with `needs Agent` or `tool fn` as an agent root. Safety passes operate over the set of functions reachable from these roots.

### 10.3 Error Reporting

Source analysis and verification emit diagnostics with a code, severity, message, and location when available. Backend and runtime failures use the corresponding command's error reporting. `splash check --json` exposes the source verdict, effect coverage, and diagnostics. [Check JSON v1](docs/design/semantic-compiler-interface.md#check-json-v1)

---

## 11. Go Code Generation

### 11.1 Type Mapping

| Splash Type     | Go Type          |
|-----------------|------------------|
| `Int`           | `int`            |
| `Float`         | `float64`        |
| `String`        | `string`         |
| `Bool`          | `bool`           |
| `Void`          | (omitted)        |
| `List<T>`       | `[]T`            |
| `Map<K, V>`     | `map[K]V`        |
| `T?`            | `*T`             |
| `Result<T, E>`  | `(T, error)`     |

### 11.2 Struct Types

Splash `type` declarations become Go `struct` types. Field names are emitted as-is (exported by convention).

### 11.3 Enum Types

Splash `enum` declarations become Go type aliases with constants. Variants with payloads are represented as structs.

### 11.4 The Agent Error Boundary

For `approve fn` and its resolved transitive callers, the Go backend adds an `error` return alongside a value return or uses `error` alone for a function with no return value. A generated adapter call guards the approval-gated body. Denial propagates through the modeled call path, including across `needs Agent` functions. The `Agent` marker alone adds no Go error return.

The proposed AI runtime would route provider failures, including network errors and timeouts, to the same error boundary. Current source analysis models the `AI` effect of `ai.prompt<T>`. The Go backend emits an ordinary call for it.

### 11.5 `tool fn` Functions

`tool fn` functions are emitted as normal Go functions. Their JSON Schema is a separate output from `splash tools` — it is not embedded in the generated Go.

### 11.6 Multi-File Modules

When multiple `.splash` files are merged (`use` imports), all declarations are emitted into a single Go file (`output.go`). The `go build` step compiles this as a single package.

### 11.8 `fn main()` and the Agent Error Boundary

Splash's `fn main()` is emitted as a Go `run() error` function, with a generated `main()` wrapper:

```go
// Generated — developer never sees this
func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

// Generated from Splash fn main()
func run() error {
    insight, err := askHealthCoach(1, "Should I train hard today?")
    if err != nil { return err }
    fmt.Println(insight.Summary)
    return nil
}
```

The generated wrapper prints errors returned by `run()` and exits with status 1. In library use, callers receive the generated error return on approval paths.

### 11.9 Runtime Support

Go output for programs with approval gates includes these runtime helpers.

- `splashApprove(name string) error` — dispatches to the registered `ApprovalAdapter`
- `ApprovalAdapter` interface — `Request(name string) error`
- `SetApprovalAdapter(a ApprovalAdapter)` — register before agent startup
- The injected `splashStdinApproval` prompts standard input until the operator approves

Source verification checks the supported approval call paths. The adapter enforces each generated gate at runtime. Go lowering also emits helpers required by other supported constructs, such as optional-value coalescing.

---

## Appendix A: Annotation Quick Reference

| Annotation                                          | Applies To | Purpose                                                   |
|-----------------------------------------------------|------------|-----------------------------------------------------------|
| `tool fn`                                             | function   | Tool candidate with argument schema and return classification checks |
| `redline fn`                                          | function   | Source verification rejects resolved agent reachability  |
| `approve fn`                                          | function   | Runtime approval gate with Go error propagation          |
| `@reason "..."`                                     | declaration | Human-readable justification for the next declaration   |
| `@agent_allowed`                                    | function   | Explicit permission for `approved_only` and the `DB.write` plus `Net` rule |
| `@containment(agent: "none"\|"read_only"\|"approved_only")` | module | Module-level agent access policy             |
| `@sandbox(allow: [...], deny: [...])`               | function   | Constrain declared effects on resolved reachable functions |
| `@budget(max_cost: Float, max_calls: Int)`          | function   | Compile-time resource limit validation                    |
| `@sensitive`                                        | field      | Sensitive classification for supported tool and logging checks |
| `@restricted`                                       | field      | Highest classification for supported tool and logging checks |
| `@internal`                                         | field      | Internal-only; raises classification above `public`       |

## Appendix B: Effect Quick Reference

| Effect      | Meaning                          | Subject to `@sandbox`? |
|-------------|----------------------------------|------------------------|
| `DB.read`   | Database reads                   | Yes                    |
| `DB.write`  | Database writes                  | Yes                    |
| `DB.admin`  | Schema-level operations          | Yes                    |
| `Net`       | Outbound network                 | Yes                    |
| `AI`        | AI model calls                   | Yes                    |
| `Agent`     | Agent entry point (structural)   | No                     |
| `FS`        | Filesystem                       | Yes                    |
| `Exec`      | Subprocess                       | Yes                    |
| `Cache`     | Cache operations                 | Yes                    |
| `Secrets`   | Secrets store                    | Yes                    |
| `Queue`     | Message queue                    | Yes                    |
| `Metric`    | Metrics emission                 | Yes                    |

## Appendix C: Data Classification Quick Reference

| Classification  | Field Annotation | `tool fn` returnable? | `Loggable`? |
|-----------------|-----------------|---------------------|-------------|
| `public`        | (none)          | Yes                 | Yes         |
| `@internal`     | `@internal`     | Yes                 | Yes         |
| `@sensitive`    | `@sensitive`    | No                  | No          |
| `@restricted`   | `@restricted`   | No                  | No          |
