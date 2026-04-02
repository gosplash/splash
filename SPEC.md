# Splash Language Specification

**Version:** 0.1  
**Status:** Working draft — authoritative for the current compiler implementation

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

Splash is a statically-typed, compiled language that transpiles to Go. Its distinguishing feature is a compile-time safety model purpose-built for AI agents: an effect system, call graph analysis, data classification, and a suite of annotations (`@redline`, `@approve`, `@containment`, `@sandbox`, `@budget`, `@sensitive`, `@restricted`) that enforce agent safety properties before codegen.

The Go backend receives a verified, effect-annotated AST with no security-relevant decisions remaining.

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
async    await    try      true         false
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
function_decl ::= ["async"] "fn" ident ["<" type_params ">"]
                  "(" params ")" ["needs" effect_list] ["->" type_expr]
                  block_stmt

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
type_expr ::= ident                          -- named type: Int, String, MyType
            | ident "<" type_args ">"        -- generic: List<String>, Result<T, E>
            | type_expr "?"                  -- optional: String?, Int?
            | "fn" "(" type_list ")" ["->" type_expr]  -- function type
            | type_expr "|" type_expr        -- union (parsed, limited typechecker support)

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

**Generic calls** use lookahead to distinguish `f<T>(x)` (generic call) from `f < T` (comparison). The lookahead checks for `< Ident[?] [, Ident[?]]* > (`.

---

## 4. Type System

### 4.1 Built-in Types

| Type         | Description                                    |
|--------------|------------------------------------------------|
| `Int`        | 64-bit integer                                 |
| `Float`      | 64-bit floating point                          |
| `String`     | UTF-8 string                                   |
| `Bool`       | Boolean                                        |
| `Void`       | No return value                                |
| `List<T>`    | Ordered sequence                               |
| `Map<K, V>`  | Key-value map                                  |
| `Option<T>`  | Optional value (also written `T?`)             |
| `Result<T, E>` | Success or error (used by `std/ai`)          |

`T?` is syntactic sugar for `Option<T>`. The type checker treats them identically.

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

Function effect requirements flow transitively: a function's effective effects are the union of its declared `needs` and the `needs` of every function it calls. The type checker enforces that every call site provides a superset of the callee's required effects.

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

### 5.3 Effect Checking

The type checker errors if a call site's declared effects do not cover the callee's required effects:

```splash
fn bad() -> String {
    return summarize(1)   // error: missing DB.read, AI
}
```

### 5.4 Effect Propagation Through `@approve`

`@approve` functions widen their return type to `(T, error)`. All transitive callers also receive the widened signature. The type checker propagates this widening before codegen.

---

## 6. Annotations

Annotations appear immediately before declarations. Multiple annotations may be stacked.

### 6.1 `@tool`

```splash
@tool
fn search_catalog(query: String, limit: Int) needs DB.read -> List<SearchResult> { ... }
```

Marks a function as AI-callable. `splash tools` emits a JSON Schema entry for every `@tool` function. `@tool` functions may not return a type whose data classification exceeds `@internal` (see [Section 7](#7-data-classification)).

### 6.2 `@redline`

```splash
@redline
fn delete_all_data() needs DB.admin -> Void { ... }
```

Build fails if any agent-reachable call path reaches this function. The error includes the full call path from the agent root to the redlined function.

### 6.3 `@approve`

```splash
@approve
fn charge_card(customer_id: Int, amount_cents: Int) needs Net -> Charge { ... }
```

Injects a human approval gate. At compile time:
1. The function's Go return type is widened to `(T, error)`.
2. `splashApprove("fn_name")` is injected as the first statement of the body.
3. All transitive callers also receive `(T, error)` signatures — denial propagates as an error.

At runtime, the `ApprovalAdapter` interface determines the gate behavior. The default adapter prompts stdin. Production adapters (`SlackApproval`, `WebhookApproval`) are not yet implemented.

### 6.4 `@agent_allowed`

```splash
@agent_allowed
fn read_public_catalog() needs DB.read -> List<Item> { ... }
```

Exempts a function from the `@containment(agent: "approved_only")` check. Has no effect when `@containment` is not in force.

### 6.5 `@containment`

Module-level annotation. Applied to the module declaration:

```splash
@containment(agent: "approved_only")
module billing
```

Three policies:

| Value           | Meaning                                                    |
|-----------------|------------------------------------------------------------|
| `"none"`        | No agent may call any function in this module              |
| `"read_only"`   | Agents may only call functions with `DB.read` effects      |
| `"approved_only"` | Agents may only call `@agent_allowed` functions          |

### 6.6 `@sandbox`

```splash
@sandbox(allow: [DB.read, Net])
fn search_agent() needs Agent, DB.read, Net -> List<Result> { ... }
```

Constrains the effects of the entire reachable call graph from this function. The safety checker:
1. Walks every function reachable from the `@sandbox`-annotated function.
2. For each reachable function, checks its declared effects against the `allow`/`deny` lists.
3. Emits an error for any effect not in `allow` (or in `deny`).

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
    email: @sensitive String
    name:  String
}
```

A `@sensitive` field elevates the containing type's data classification. `@tool` functions cannot return a type with any `@sensitive` field. Functions requiring `Loggable` cannot accept a type with `@sensitive` fields.

### 6.9 `@restricted`

Applied to struct fields:

```splash
type Config {
    db_url: @restricted String
}
```

A `@restricted` field means the value is process-local — no storage adapter may accept it. This is the highest classification level.

### 6.10 `@internal`

Applied to struct fields:

```splash
type Report {
    content:   String
    cost_cents: @internal Int
}
```

An `@internal` field elevates the containing type above `public` but below `@sensitive`.

---

## 7. Data Classification

Classification is a property of types, not values. Fields carry classification; the containing type's classification is the maximum of its fields.

### 7.1 Classification Lattice

```
public < @internal < @sensitive < @restricted
```

A type with no annotated fields is `public`. A type with at least one `@internal` field is `@internal`. A type with at least one `@sensitive` field is `@sensitive`. A type with at least one `@restricted` field is `@restricted`.

### 7.2 Compile-Time Enforcement

Two rules are enforced today:

1. **`@tool` return type:** A `@tool` function cannot return a type whose classification exceeds `@internal`. PII (`@sensitive`) and process-local data (`@restricted`) must not flow into the agent's context window.

2. **`Loggable` constraint:** A type parameter constrained with `Loggable` cannot be instantiated with a type whose classification exceeds `public`. Logging PII is rejected at compile time.

### 7.3 `Loggable`

`Loggable` is a built-in constraint. No declaration is required. Any type without `@sensitive` or `@restricted` fields satisfies `Loggable`. The `println` built-in requires `Loggable`.

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

Provides:

| Symbol           | Type                                         | Description                        |
|------------------|----------------------------------------------|------------------------------------|
| `ai.prompt<T>`   | `fn(prompt: String) needs AI -> Result<T, AIError>` | Call an AI model, parse response as `T` |
| `Result<T, E>`   | `enum Result<T, E> { Ok(T) Error(E) }`       | Success or error                   |
| `AIError`        | `type AIError { message: String }`           | AI call failure                    |

`ai.prompt<T>` is a generic function call: `ai.prompt<Report>("Summarize this")`.

The `AI` effect is required in `needs` to call `ai.prompt`.

---

## 10. Compilation Pipeline

```
source (.splash)
  → Lexer         tokenize → []token.Token
  → Parser        []token.Token → *ast.File
  → TypeChecker   type inference, effect propagation, constraint satisfaction
  → CallGraph     directed call graph, AgentRoots(), Reachable(), Callers()
  → SafetyChecker @redline, @approve, @containment, @sandbox, @budget, data classification
  → Emitter       verified AST → Go source
  → go build      Go source → native binary
```

Each stage is independent. The CLI wires them in `cmd/splash/main.go`.

### 10.1 CLI Commands

| Command                     | Stages run                          |
|-----------------------------|-------------------------------------|
| `splash check <file>`       | Lexer → TypeChecker → SafetyChecker |
| `splash build <file> -o <out>` | Full pipeline → `go build`       |
| `splash emit <file>`        | Full pipeline → print Go source     |
| `splash tools <file>`       | Lexer → TypeChecker → JSON Schema   |

### 10.2 Agent Roots

The call graph treats any function with `needs Agent` or `@tool` as an agent root. Safety passes operate over the set of functions reachable from these roots.

### 10.3 Error Reporting

All stages emit `[]diagnostic.Diagnostic`. A `Diagnostic` carries:
- `Severity` — `Error` or `Warning`
- `Message` — human-readable description
- `Position` — filename, line, column

The CLI prints diagnostics and exits non-zero if any errors are present.

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

### 11.4 `@approve` Signature Widening

Any function annotated with `@approve` — and all transitive callers — have their return type widened from `T` to `(T, error)` in the generated Go. The call to `splashApprove("fn_name")` is injected as the first statement.

### 11.5 `@tool` Functions

`@tool` functions are emitted as normal Go functions. Their JSON Schema is a separate output from `splash tools` — it is not embedded in the generated Go.

### 11.6 Multi-File Modules

When multiple `.splash` files are merged (`use` imports), all declarations are emitted into a single Go file (`output.go`). The `go build` step compiles this as a single package.

### 11.7 Runtime Support

The generated Go file includes a small runtime preamble:

- `splashApprove(name string) error` — dispatches to the registered `ApprovalAdapter`
- `ApprovalAdapter` interface — `Approve(name string) error`
- `SetApprovalAdapter(a ApprovalAdapter)` — register before agent startup
- Default `StdinApprovalAdapter` prompts standard input

No other runtime support is injected. All safety enforcement is complete before codegen.

---

## Appendix A: Annotation Quick Reference

| Annotation                                          | Applies To | Purpose                                                   |
|-----------------------------------------------------|------------|-----------------------------------------------------------|
| `@tool`                                             | function   | AI-callable; `splash tools` emits JSON Schema             |
| `@redline`                                          | function   | Build fails if agent-reachable                            |
| `@approve`                                          | function   | Human gate; `(T, error)` cascade through callers          |
| `@agent_allowed`                                    | function   | Exempt from `@containment(agent: "approved_only")`        |
| `@containment(agent: "none"\|"read_only"\|"approved_only")` | module | Module-level agent access policy             |
| `@sandbox(allow: [...], deny: [...])`               | function   | Constrain effects of entire reachable call graph          |
| `@budget(max_cost: Float, max_calls: Int)`          | function   | Compile-time resource limit validation                    |
| `@sensitive`                                        | field      | PII; blocks `@tool` return and `Loggable` usage           |
| `@restricted`                                       | field      | Process-local; no storage adapter accepts it              |
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

| Classification  | Field Annotation | `@tool` returnable? | `Loggable`? |
|-----------------|-----------------|---------------------|-------------|
| `public`        | (none)          | Yes                 | Yes         |
| `@internal`     | `@internal`     | Yes                 | No          |
| `@sensitive`    | `@sensitive`    | No                  | No          |
| `@restricted`   | `@restricted`   | No                  | No          |
