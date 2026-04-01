# Splash Phase 1 — Compiler Design Spec

**Date:** 2026-04-01
**Status:** Approved
**Scope:** `splash check` — lexer, parser, type checker, effect checker with `needs` propagation and data classification enforcement

---

## What We're Building

A `splash check` CLI that accepts a `.splash` file and produces typed, effect-annotated diagnostics with source locations. Phase 1 delivers two things a developer will feel immediately:

1. A missing `needs DB` declaration on a function that calls the database is a compile error.
2. Attempting to log a `@sensitive Email` field is a compile error.

These are the demo moments. Everything else in Phase 1 is infrastructure that makes them possible.

---

## Repository Layout

```
gosplash/
  cmd/
    splash/
      main.go              # CLI entry point
  internal/
    token/                 # Token types and Position — imported by lexer and parser
    lexer/                 # []byte → []token.Token
    ast/                   # AST node types, Annotation types
    parser/                # []token.Token → ast.File
    types/                 # Type system: primitives, optionals, Result, generics, constraints, classification
    typechecker/           # ast.File → TypedFile (type inference, constraint satisfaction)
    effects/               # TypedFile → EffectFile (needs checking, classification enforcement)
    diagnostic/            # Diagnostic with Position, Severity, Message
  testdata/
    valid/                 # .splash programs that should produce zero diagnostics
    errors/                # .splash programs with known errors + paired .diag golden files
  docs/
    superpowers/
      specs/
        2026-04-01-splash-phase1-design.md   # this file
  PRD.md
  WHITEPAPER.md
  go.mod                   # module: gosplash.dev/splash
```

---

## Data Flow

```
source.splash
  → lexer        → []token.Token
  → parser       → ast.File           (annotations attached at parse time)
  → typechecker  → TypedFile          (every expr carries a resolved types.Type)
  → effects      → EffectFile         (every fn carries resolved EffectSet, every call site checked)
  → []diagnostic.Diagnostic           (sorted by source location, printed or serialized)
```

Each phase returns its output and any diagnostics it produced. Phases are independent — the typechecker doesn't import the lexer, the effects package doesn't import the parser. All inter-phase communication is through the typed data structures defined in `ast/`, `types/`, and the effect types.

---

## Package Designs

### `internal/token`

Token types and source positions. Kept separate from `lexer` so both the lexer and parser import one package, not each other.

```go
type Kind int
const (
    // Literals
    INT, FLOAT, STRING, BOOL, IDENT

    // Keywords
    FN, TYPE, ENUM, CONSTRAINT, MODULE, EXPOSE, USE,
    LET, RETURN, IF, ELSE, GUARD, MATCH, FOR, IN,
    ASYNC, AWAIT, TRY, NEEDS, NONE, IMPORT,

    // Operators and punctuation
    ARROW, FAT_ARROW, OPTIONAL_CHAIN, NULL_COALESCE,
    QUESTION, BANG, DOT, COLON, COMMA, SEMICOLON,
    LPAREN, RPAREN, LBRACE, RBRACE, LBRACKET, RBRACKET,
    LANGLE, RANGLE, PLUS, MINUS, STAR, SLASH,
    EQ, NEQ, LT, LTE, GT, GTE, AND, OR,
    ASSIGN, PLUS_ASSIGN, PIPE,

    // Annotations (@ prefix)
    AT,

    EOF
)

type Position struct {
    File   string
    Line   int
    Column int
    Offset int
}

type Token struct {
    Kind    Kind
    Literal string
    Pos     Position
}
```

### `internal/lexer`

Hand-written scanner with character lookahead. Produces a flat `[]token.Token` slice. Tracks line and column for every token. Emits an `INVALID` token (rather than panicking) on unknown input so the parser can recover and continue.

Key behaviors:
- Annotation detection: `@` followed by an identifier emits `AT` + `IDENT` tokens
- Operator disambiguation: `?.` vs `.`, `??` vs `?`, `->` vs `-`
- String interpolation: `"Hello {name}"` — the lexer tokenizes the interpolated segments
- Block and line comments stripped (not passed to parser)

### `internal/ast`

AST node types. All nodes implement `Node`:

```go
type Node interface {
    Pos() token.Position
    String() string  // debug representation
}
```

Key node types:

```go
// Top-level
type File struct {
    Module      *ModuleDecl
    Exposes     []string
    Uses        []UseDecl
    Declarations []Decl
}

type ModuleDecl struct {
    Name        string
    Annotations []Annotation
    Pos         token.Position
}

// Declarations
type FunctionDecl struct {
    Name        string
    TypeParams  []TypeParam
    Params      []Param
    ReturnType  TypeExpr
    Effects     []EffectExpr  // parsed `needs DB, Net` clause
    Body        *BlockStmt
    Annotations []Annotation
    Pos         token.Position
}

type TypeDecl struct {
    Name        string
    TypeParams  []TypeParam
    Fields      []FieldDecl
    Annotations []Annotation
    Pos         token.Position
}

type FieldDecl struct {
    Name        string
    Type        TypeExpr
    Default     Expr
    Annotations []Annotation   // @sensitive, @restricted, @internal
    Pos         token.Position
}

type ConstraintDecl struct {
    Name        string
    TypeParams  []TypeParam
    Methods     []ConstraintMethod
    Pos         token.Position
}

type EnumDecl struct {
    Name     string
    Variants []EnumVariant
    Pos      token.Position
}
```

**Annotations** attach to their target node at parse time:

```go
type AnnotationKind int
const (
    // Safety — compiler-enforced in Phase 2
    AnnotRedline AnnotationKind = iota
    AnnotApprove
    AnnotContainment
    AnnotAgentAllowed
    AnnotSandbox
    AnnotBudget
    AnnotCapabilityDecay

    // Data classification — enforced in Phase 1 (constraint satisfaction)
    AnnotSensitive
    AnnotRestricted
    AnnotInternal
    AnnotAudit

    // Stdlib / tools
    AnnotTool
    AnnotTrace
    AnnotDeadline
    AnnotRoute
    AnnotTest
    AnnotDeploy
)

type Annotation struct {
    Kind AnnotationKind
    Args map[string]Expr   // named args: reason, prompt, timeout, etc.
    Pos  token.Position
}
```

Phase 1 parses all annotations and attaches them to AST nodes. Phase 1 enforces only `@sensitive`/`@restricted`/`@internal` (via constraint satisfaction). Phase 2 enforces `@redline`, `@approve`, `@containment`, `@agent_allowed` via call graph analysis. The annotation data is already in the AST — Phase 2 reads it without re-parsing.

### `internal/parser`

Pratt precedence-climbing parser. Produces `ast.File` from `[]token.Token`.

Operator precedence table (low to high):

| Level | Operators |
|-------|-----------|
| 1 | assignment `=` |
| 2 | `\|\|` |
| 3 | `&&` |
| 4 | `==` `!=` |
| 5 | `<` `<=` `>` `>=` |
| 6 | `+` `-` |
| 7 | `*` `/` |
| 8 | unary `!` `-` |
| 9 | `?.` `.` `()` `[]` |

Error recovery: the parser syncs to the next statement boundary (`;`, `}`) on a syntax error, continues parsing, and collects all syntax errors in a single pass rather than stopping at the first failure.

### `internal/types`

The type system. All types implement `Type`:

```go
type Type interface {
    TypeName() string
    IsAssignableTo(other Type) bool
    Classification() Classification
}

type Classification int
const (
    ClassPublic     Classification = iota
    ClassInternal                   // @internal
    ClassSensitive                  // @sensitive
    ClassRestricted                 // @restricted
)
```

Key type implementations:

```go
// Primitives: StringType, IntType, FloatType, BoolType, VoidType
// Optional: OptionalType{Inner Type}
// Result:   ResultType{Ok Type, Err Type}
// List:     ListType{Element Type}
// Map:      MapType{Key Type, Value Type}
// Function: FunctionType{Params []Type, Return Type}
// Generic:  TypeParam{Name string, Constraints []string}
// Named:    NamedType{Name string, TypeArgs []Type, Decl *ast.TypeDecl}
// Constraint: ConstraintType{Name string, Methods []ConstraintMethod}
```

**Classification rules:**
- A named type's classification is the maximum classification of all its fields
- `OptionalType` inherits its inner type's classification
- A type satisfies a constraint only if its classification permits it:
  - `Loggable` requires `Classification <= ClassInternal`
  - `@restricted` types satisfy no stdlib constraints that cross process boundaries

### `internal/typechecker`

Two-pass checker. Produces `TypedFile` — a parallel structure to `ast.File` where every expression node carries a resolved `types.Type`.

**Pass 1:** Register all top-level declarations (types, constraints, enums, functions) into a global symbol table. This allows forward references — a function can reference a type declared later in the file.

**Pass 2:** Type-check all function bodies, field defaults, and constraint implementations. Resolves generic instantiations. Checks constraint satisfaction including classification preconditions.

Constraint satisfaction check (the `@sensitive` → `Loggable` enforcement):

```go
func (tc *TypeChecker) satisfiesConstraint(t types.Type, c *types.ConstraintType) []diagnostic.Diagnostic {
    // Check classification precondition first
    if c.Name == "Loggable" && t.Classification() > types.ClassInternal {
        return []diagnostic.Diagnostic{{
            Severity: diagnostic.Error,
            Message:  fmt.Sprintf("type %s is @%s and cannot implement Loggable", t.TypeName(), classificationName(t.Classification())),
            Pos:      ...,
        }}
    }
    // Check method signatures
    ...
}
```

### `internal/effects`

Consumes `TypedFile`, produces `EffectFile`. Performs two checks:

1. **`needs` propagation:** At every call site, verifies the caller's declared effect set satisfies the callee's declared effect set.
2. **Classification enforcement:** Verifies `@sensitive`/`@restricted` fields don't flow into log statements, external serialization without policy, or cross-process boundaries without encryption markers.

**Effect types:**

```go
type Effect uint64

const (
    EffectDB      Effect = 1 << iota  // grants DB.read + DB.write + DB.admin
    EffectDBRead
    EffectDBWrite
    EffectDBAdmin
    EffectNet
    EffectCache
    EffectAI
    EffectFS
    EffectExec
    EffectClock
    EffectAgent
    EffectStore
    EffectStoreRead
    EffectStoreWrite
    EffectQueue
    EffectQueuePublish
    EffectQueueSubscribe
    EffectMetric
    EffectSecrets
    EffectSecretsRead
    EffectSecretsWrite
    // 20 effects used of 64 available — document ceiling
)

// EffectCeiling: uint64 supports 64 distinct effects.
// Current count: ~20. Headroom exists for adapter-specific refinements
// (Store.read/write, Queue.publish/subscribe already included).
// If the vocabulary grows past 64, migrate EffectSet to []Effect or map[Effect]bool.
// This migration is isolated to the effects package — callers use EffectSet opaquely.

type EffectSet uint64

func (s EffectSet) Satisfies(required EffectSet) bool {
    return s&required == required
}
```

**Effect expansion** — parent effects expand to include all children when resolving a function's declared effects:

```go
var effectExpansion = map[Effect]EffectSet{
    EffectDB:     EffectSet(EffectDB | EffectDBRead | EffectDBWrite | EffectDBAdmin),
    EffectStore:  EffectSet(EffectStore | EffectStoreRead | EffectStoreWrite),
    EffectQueue:  EffectSet(EffectQueue | EffectQueuePublish | EffectQueueSubscribe),
    EffectSecrets: EffectSet(EffectSecrets | EffectSecretsRead | EffectSecretsWrite),
}

func Expand(s EffectSet) EffectSet {
    expanded := s
    for parent, children := range effectExpansion {
        if s&EffectSet(parent) != 0 {
            expanded |= children
        }
    }
    return expanded
}
```

When checking `Satisfies`, the caller's declared set is expanded before the AND comparison. `needs DB` in source → `Expand(EffectDB)` → satisfies a callee that `needs DB.read`.

**Effect-annotated output — the Phase 1/2 boundary:**

```go
type EffectFunctionDecl struct {
    Decl            *ast.FunctionDecl
    Type            *types.FunctionType
    DeclaredEffects EffectSet     // declared in source: needs DB, Net
    ResolvedEffects EffectSet     // expanded + verified
    Annotations     []ast.Annotation  // @redline, @approve, @tool, etc. — ready for Phase 2
}

type EffectCallExpr struct {
    Expr          *ast.CallExpr
    CalleeEffects EffectSet  // callee's declared needs
    CallerEffects EffectSet  // caller's available effects at this call site
    Satisfied     bool       // Phase 1 local check result
}
```

Phase 2 reads `Annotations` off `EffectFunctionDecl` nodes and builds the call graph from the call expression tree. No re-parsing required.

### `internal/diagnostic`

```go
type Severity int
const (
    Error Severity = iota
    Warning
    Note
)

type Diagnostic struct {
    Severity Severity
    Message  string
    Pos      token.Position
    Notes    []string  // additional context lines
}

func (d Diagnostic) String() string {
    return fmt.Sprintf("%s:%d:%d: %s: %s", d.Pos.File, d.Pos.Line, d.Pos.Column, d.Severity, d.Message)
}
```

### `cmd/splash`

```
splash check <file.splash>           # lex → parse → typecheck → effect check
splash check --ast <file.splash>     # also print annotated AST (dev)
splash check --json <file.splash>    # diagnostics as JSON (editor integration)
```

Exit code 0 = clean. Exit code 1 = one or more Error diagnostics.

---

## Testing Strategy

**Unit tests** per package, covering:
- Lexer: every token kind, edge cases (string interpolation, optional chaining, annotation syntax)
- Parser: every declaration form, expression precedence, error recovery
- Type checker: constraint satisfaction, generic instantiation, classification enforcement
- Effects: `needs` propagation, parent effect expansion, call site checking

**Integration tests** via golden files:

```
testdata/
  valid/
    basics.splash           # basics from spec §02 — zero diagnostics expected
    generics.splash         # §03
    adapters.splash         # §04
    effects.splash          # §09
    data_safety.splash      # §07
    tools.splash            # §10
  errors/
    missing_needs.splash    # fn calls db without needs DB
    missing_needs.diag      # expected: file.splash:3:10: error: fn requires effect DB...
    sensitive_log.splash    # log.info interpolates @sensitive field
    sensitive_log.diag      # expected: file.splash:8:3: error: cannot interpolate @sensitive...
    missing_down.splash     # migration without down block
    missing_down.diag       # expected: ...
```

The spec's `// ❌ COMPILE ERROR` comments are the source of truth for error test expectations. If the spec says it's an error, the test says it's an error.

---

## What Phase 1 Does NOT Do

- No Go code generation (Phase 2)
- No call graph analysis — `@redline`, `@approve`, `@containment` are parsed and attached to AST nodes but not enforced (Phase 2)
- No agent-context reachability (Phase 2)
- No stdlib implementation — `db`, `cache`, `ai` etc. are known effect sources but not implemented
- No `splash build`, `splash dev`, `splash run` (Phase 2+)

---

## Go Module

```
module gosplash.dev/splash

go 1.22
```
