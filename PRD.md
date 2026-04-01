# Splash

**Language Specification v0.1**

*A programming language for the age of AI agents, human-AI collaboration, safe deployments, and systems you can sleep at night after shipping.*

`safe by default` · `observable` · `data-aware` · `agent-native` · `compiled` · `typed` · `structured concurrency` · `supply chain safe`

---

## 01 — Philosophy

Every mainstream language was designed before LLMs could write code, before AI agents needed sandboxing, before "vibe coding" was a legitimate methodology, and before the biggest production risk was **an autonomous system doing something you didn't expect with data it shouldn't have**.

Splash is designed for a world where your coworker might be an LLM, your deployment target might be an agent swarm, and the most important language feature is **making the dangerous thing harder than the safe thing**.

**🛡 Safe by Default** — All data is classified. All network calls are declared. All side effects are tracked. You opt INTO danger, never out of safety.

**🔭 Observable by Nature** — Traces, metrics, and structured logs are language primitives — not afterthoughts bolted on with middleware.

**🤝 Agent-Legible** — Syntax designed so LLMs parse intent, not just tokens. Explicit contracts, no magic. If an agent wrote it, a human audits it in seconds.

**🎯 Vibes-Compatible** — Describe what you want in English and the generated Splash code reads like what you said. No ceremony for ceremony's sake.

**🔗 Supply Chain Safe** — Every dependency declares its capabilities. Permissions are granted explicitly, locked in a reviewable file, and enforced at compile time. No silent escalation.

**⏱ Deadline-Aware** — Every operation inherits a shrinking time budget. Context, cancellation, and deadlines propagate automatically. Nothing runs forever.

**🔌 Adapter-First** — Every stdlib service is defined by a constraint interface. Swap backends without changing application code. Redis, Postgres, S3, in-memory — the adapter decides, the contract guarantees.

---

## 02 — Language Basics

Splash takes the best ergonomics from Go (simplicity, fast compilation, single binary), Swift (type safety, optionals, value semantics), and Rust (ownership clarity, enums, pattern matching) — then throws away everything that exists purely to make the compiler happy. No `nil`, no `null`, no `undefined`, no unchecked exceptions, no implicit type coercion.

```Splash
// basics.splash

module greeter

expose greet, Greeting

type Greeting {
  message:  String
  locale:   Locale   = Locale.en_US
  created:  Instant  = now()
}

fn greet(name: String) -> Greeting {
  return Greeting { message: "Hello, {name}. Systems nominal." }
}

// Absence is explicit via optionals
let name: String = "Zach"
let nickname: String? = none
let display = nickname ?? name
let upper = nickname?.upper()            // String?

// Result types for fallible operations — no exceptions
fn parse_id(raw: String) -> Result<UserId, ParseError> {
  guard raw.matches(/^usr_[a-z0-9]{12}$/) else {
    return err(ParseError.invalid_format(raw))
  }
  return ok(UserId(raw))
}

// `try` propagates errors — only in fns that return Result
fn load_user(raw: String) -> Result<User, AppError> {
  let id = try parse_id(raw)
  let user = try db.find(User, id)
  return ok(user)
}

// Pattern matching is exhaustive. No wildcard by default.
fn respond(g: Greeting) -> String {
  match g.locale {
    .en_US => "Welcome"
    .es_MX => "Bienvenido"
    .ja_JP => "ようこそ"
  }
}
```

---

## 03 — Generics

Type parameters have **constraint bounds** — a flat list of functions the type must implement. No inheritance, no associated types, no HKTs. Constraints compose with `+`. Data classification interacts with bounds: `@sensitive` types cannot satisfy `Loggable`. If you can read the constraint, you know every operation available.

```Splash
// generics.splash

constraint Ordered {
  fn compare(self, other: Self) -> Ordering
}

constraint Serializable {
  fn to_json(self) -> JSON
  static fn from_json(json: JSON) -> Result<Self, DecodeError>
}

// @sensitive / @restricted types CANNOT implement Loggable
constraint Loggable {
  fn to_log_string(self) -> String
}

fn sort<T: Ordered>(items: List<T>) -> List<T> { ... }

fn cache_put<T: Serializable + Hashable>(key: String, val: T, ttl: Duration)
  needs Cache
{ ... }

type Page<T> {
  items:    List<T>
  cursor:   String?
  has_more: Bool
}

fn merge_sorted<T, K>(a: List<T>, b: List<T>, key: fn(T) -> K) -> List<T>
  where K: Ordered
{ ... }
```

---

## 04 — Adapter Pattern

Every stdlib service is defined by a **constraint interface** — not a concrete implementation. The stdlib ships default adapters (Postgres, Redis, S3, etc.), but developers plug in their own by implementing the constraint. Application code depends on the interface. Backends are wired at startup.

This is the foundational design pattern of Splash's stdlib. Every section that follows uses it.

```Splash
// adapters.splash — how the pattern works

// ─── The constraint defines the contract ──────────────
// std/cache ships this interface. All application code
// depends on this — never on a concrete backend.

constraint CacheAdapter {
  async fn get<T: Serializable>(self, key: String) -> Result<T?, CacheError>
  async fn set<T: Serializable>(self, key: String, value: T, ttl: Duration) -> Result<Void, CacheError>
  async fn delete(self, key: String) -> Result<Bool, CacheError>
  async fn ping(self) -> Result<Void, CacheError>
}

// ─── stdlib ships default adapters ────────────────────

// Built-in: Redis adapter (default for production)
type RedisCache implements CacheAdapter {
  fn new(url: String, options: RedisOptions = .defaults) -> Self
  // ... all CacheAdapter methods implemented
}

// Built-in: In-memory adapter (default for dev/test)
type MemoryCache implements CacheAdapter {
  fn new() -> Self
  // ...
}

// ─── Developers write their own ───────────────────────

// Your custom adapter — just implement the constraint
type DragonflyCache implements CacheAdapter {
  fn new(cluster: ClusterConfig) -> Self
  // ... implement all CacheAdapter methods
}

// ─── Wiring happens at startup ────────────────────────

module main

fn main() {
  let cache = match env.get("CACHE_BACKEND") ?? "redis" {
    "redis"     => RedisCache.new(env.require("REDIS_URL"))
    "memory"    => MemoryCache.new()
    "dragonfly" => DragonflyCache.new(cluster_config)
  }

  let server = http.server({
    port:   env.get("PORT") ?? 8080,
    routes: autodiscover(),
    // Inject adapters — all downstream code uses the interface
    adapters: {
      cache: cache,
      db:    PostgresDB.new(env.require("DATABASE_URL")),
      store: S3Store.new(env.require("S3_BUCKET")),
    }
  })

  server.start()
}

// ─── Application code never knows the backend ─────────

// This function works with Redis, Dragonfly, memory, or anything
// that implements CacheAdapter. Zero changes needed.
async fn get_profile(id: UserId) -> Result<Profile, AppError>
  needs Cache, DB
{
  return cache.get_or_set("profile:{id}", ttl: 5.minutes) {
    try db.find(Profile, id)
  }
}
```

> **Design rule:** If it talks to infrastructure, it's behind a constraint. `DB`, `Cache`, `Store`, `Queue`, `AI` — all of them. Your tests swap in mocks. Your migration from Redis to Dragonfly is a one-line change in `main()`. Your application code never imports a backend driver directly.

```Splash
// The full adapter landscape:

constraint DBAdapter       { ... }  // Postgres, SQLite, MySQL, CockroachDB
constraint CacheAdapter    { ... }  // Redis, Memcached, Dragonfly, in-memory
constraint StoreAdapter    { ... }  // S3, GCS, R2, local filesystem
constraint QueueAdapter    { ... }  // SQS, Redis Streams, NATS, in-memory
constraint AIAdapter       { ... }  // OpenAI, Anthropic, Grok, local models
constraint MetricAdapter   { ... }  // OTLP, Prometheus, Datadog, stdout
```

---

## 05 — Concurrency

Every concurrent operation has a parent scope, a lifetime, and automatic cancellation. When a `group` exits, every child is cancelled and awaited. Spawned tasks inherit the effect permissions and deadline of their parent. No orphaned goroutines, no forgotten futures, no privilege escalation via spawn.

```Splash
// concurrency.splash

use std/async

// Parallel — if any task fails, the group cancels
@trace
async fn prepare_checkout(user_id: UserId, cart_id: CartId) -> Result<CheckoutReady, AppError>
  needs DB, Net
{
  let (user, cart) = try group {
    async fetch_user(user_id)
    async fetch_cart(cart_id)
  }

  let fraud = try check_fraud(user, cart)
  guard fraud.score < 0.7 else { return err(AppError.fraud_detected(fraud)) }
  return ok(CheckoutReady { user, cart, fraud })
}

// Bounded fan-out
async fn enrich_all(ids: List<SermonId>) -> List<Result<Sermon, AIError>>
  needs DB, AI
{
  return group.map(ids, concurrency: 5) { id =>
    let s = try db.find(Sermon, id)
    return s.with(insight: try analyze(s.transcript))
  }
}

// First-wins racing
async fn fetch_with_fallback(key: String) -> Result<Data, FetchError>
  needs Cache, DB
{
  return race {
    async cache.get<Data>(key)
    async { delay(50.ms); db.find_by_key(key) }
  }
}

// Channels for producer/consumer
async fn process_stream() needs Net, DB {
  let events = Channel<WebhookEvent>.buffered(100)
  group {
    async { for e in webhook_listener { events.send(e) } }
    async.repeat(4) { for e in events.receive() { try handle(e) } }
  }
}
```

> **The structured guarantee:** When a `group` exits — success, failure, or cancellation — every child task is cancelled and awaited before the parent continues. No leaked goroutines. No zombie futures. No OOM from abandoned tasks holding connections.

---

## 06 — Context & Deadlines

A first-class `Context` propagates deadlines, cancellation, trace IDs, and request-scoped values through the call graph — implicitly. No `ctx` as first param. Deadlines shrink as they flow downstream: if 25 of 30 seconds are spent, the next call gets 5 seconds, not 30.

```Splash
// context.splash

use std/context

@route("POST /api/analyze")
@deadline(30.seconds)
async fn analyze_handler(req: Request<AnalyzeBody>) -> Response<Analysis>
  needs DB, AI
{
  let sermon = try db.find(Sermon, req.body.sermon_id)
  let insight = try analyze(sermon.transcript)
  return Response.ok(insight)
}

// Sub-deadlines
async fn pipeline(data: RawData) -> Result<Output, PipelineError>
  needs DB, AI, Net
{
  let enriched = try within(5.seconds) { fetch_enrichment(data) }

  guard ctx.remaining > 10.seconds else {
    return err(PipelineError.insufficient_time(ctx.remaining))
  }
  return ok(try run_analysis(enriched))
}

// Cooperative cancellation in loops
async fn long_sync(items: List<Item>) needs DB {
  for item in items {
    try ctx.check()            // err(Cancelled) if deadline expired
    try sync_item(item)
  }
}

// Request-scoped values
async fn auth_middleware(req: Request) -> Request {
  ctx.set(AuthUser, try authenticate(req.headers))
  return req
}
```

---

## 07 — Data Classification

Every piece of data has a **sensitivity classification** baked into its type. The compiler enforces what you can do at each level. An AI agent generating code can't accidentally log PII — the type system makes it structurally impossible.

```Splash
// data_safety.splash

type User {
  id:       UserId                          // public by default
  display:  String
  @sensitive
  email:    Email                           // PII
  @restricted
  ssn:      SSN                             // never leaves the process
  @sensitive
  location: GeoPoint?
}

// ❌ COMPILE ERROR: cannot interpolate @sensitive value
fn bad(u: User) { log.info("User: {u.email}") }

// ✅ Explicit masking
fn good(u: User) { log.info("User: {u.email.masked}") }   // z***@g***.com

// Declassification: explicit audit trail
@audit("support_resolution")
fn reveal(u: User, t: Ticket) -> String {
  return u.email.declassify(reason: t.id)
}
```

| Classification | Log | Serialize | Send to LLM | Cross Process |
|---|---|---|---|---|
| `public` | ✅ | ✅ | ✅ | ✅ plaintext |
| `@internal` | ✅ redacted | ✅ | ❌ | ✅ signed |
| `@sensitive` | ❌ masked | policy | ❌ | encrypted |
| `@restricted` | ❌ | ❌ | ❌ | ❌ process-local |

---

## 08 — Observability

Every function call is part of a distributed trace. Structured logs, metrics, and span attributes are language primitives. Set `Splash_TRACE_ENDPOINT` and traces export to any OpenTelemetry backend. No SDK. No init code. The `MetricAdapter` constraint allows plugging in Prometheus, Datadog, or any custom backend.

```Splash
// observe.splash

@trace(attrs: { cart_id, user_id })
fn process_checkout(cart_id: CartId, user_id: UserId) -> Result<Order, CheckoutError>
  needs DB, Net
{
  metric.count("checkout.started")

  let cart = try db.find(Cart, cart_id)
  let charge = timed("payment.charge") {
    try payments.charge(cart.total, cart.payment_method)
  }

  span.event("charge_complete", { amount: cart.total })
  metric.count("checkout.completed")
  metric.histogram("checkout.total_usd", cart.total.as_f64())

  return ok(Order.from(cart, charge))
}
```

---

## 09 — Effects System

Functions declare **effect requirements** in their signature. Pure functions are provably pure. An AI agent can't secretly add a network call — the compiler rejects it. Effects are testable by swapping providers.

```Splash
// effects.splash

// Pure — compiler guaranteed
fn calculate_tax(subtotal: Money, rate: Decimal) -> Money {
  return subtotal * rate
}

// Declares DB + Net + Clock
fn create_order(cart: Cart) -> Result<Order, OrderError>
  needs DB, Net, Clock
{ ... }

// Testing: swap providers at call site
@test
fn test_create_order() {
  let env = TestEnv {
    db: MockDB.new(), net: MockNet.capture(),
    clock: FrozenClock("2026-04-01T12:00:00Z"),
  }
  let result = env.run({ create_order(test_cart) })
  assert(result.is_ok())
  assert(env.net.calls.len == 1)
}
```

---

## 10 — AI Tools & Structured Outputs

`@tool` turns any function into an AI-callable tool. Doc comments become descriptions. The type signature becomes JSON Schema. `ai.prompt<T>` uses the type as the structured output contract and validates the response before returning. One source of truth for humans, agents, and the compiler.

```Splash
// tools.splash

use std/ai

/// Search sermons by theme, speaker, or scripture reference.
@tool
fn search_sermons(
  /// The search query — theme, topic, or verse reference
  query:   String,
  /// Max results to return
  limit:   Int      = 10,
  /// Filter by speaker name
  speaker: String?,
  /// Filter by scripture book
  book:    String?,
) needs DB -> List<SermonResult> { ... }

/// Look up a Bible verse by reference.
@tool
fn lookup_verse(
  /// e.g. "John 3:16", "Romans 8:28"
  reference:   VerseRef,
  translation: Translation = .ESV,
) needs DB -> Verse { ... }

// Types ARE structured outputs
type SermonInsight {
  /// Primary theological themes
  themes:     List<String>
  /// Key scripture references
  key_verses: List<VerseRef>
  /// One-paragraph summary
  summary:    String
  /// Overall tone
  sentiment:  Sentiment
}

enum Sentiment { hopeful, convicting, celebratory, lament, teaching, exhortation }

// ai.prompt<T> — type drives schema + validation
async fn analyze(transcript: String) needs AI -> Result<SermonInsight, AIError> {
  return ai.prompt<SermonInsight>({
    model: "grok-4-1-fast", system: "You are a biblical scholar.",
    input: transcript, budget: Cost.usd(0.05), timeout: 30.seconds, cache: true,
  })
}

// Register tools with a sandboxed agent
@sandbox(allow: [DB.read, AI], deny: [Net, FS, DB.write])
@budget(max_cost: Cost.usd(0.50), max_calls: 20)
async fn answer_question(q: String) needs Agent -> Result<Answer, AgentError> {
  return agent.execute(q, tools: [search_sermons, lookup_verse])
}
```

---

## 11 — JWT & Authentication

`std/jwt` is a stdlib module for token-based auth. JWTs are `@internal` by default — the payload can be logged (redacted) but never sent to an LLM or serialized without policy. Signing keys are always `@restricted`. The module follows the adapter pattern: HMAC and RSA ship built-in, but developers can plug in custom signers (e.g. KMS-backed).

```Splash
// auth.splash

use std/jwt

// ─── Token types carry classification ─────────────────

type Claims {
  sub:     UserId
  role:    Role
  exp:     Instant
  iat:     Instant
  @sensitive
  email:   Email?            // PII in the token — classified automatically
}

// ─── Signing uses @restricted keys ────────────────────

// Keys are always @restricted — can't be logged, serialized, or leaked
let signer = jwt.hmac256(secrets.get("JWT_SECRET"))

// Or plug in your own: KMS, Vault, HSM
let kms_signer = AwsKmsSigner.new(key_id: env.require("KMS_KEY_ID"))

// ─── Issue tokens ─────────────────────────────────────

fn issue_token(user: User) -> Result<String, JWTError> {
  return jwt.sign(signer, Claims {
    sub:   user.id,
    role:  user.role,
    email: user.email,        // @sensitive flows into the Claims type
    exp:   now() + 1.hour,
    iat:   now(),
  })
}

// ─── Verify + extract in middleware ───────────────────

async fn jwt_middleware(req: Request) -> Request {
  let token = req.headers.get("Authorization")?.strip_prefix("Bearer ")
    ?? return req.reject(401, "Missing token")

  let claims = try jwt.verify<Claims>(signer, token) else {
    return req.reject(401, "Invalid token")
  }

  // Claims flow into context — available everywhere downstream
  ctx.set(AuthUser, claims)
  return req
}

// ─── Route-level authorization ────────────────────────

@route("DELETE /api/users/{id}")
@deadline(10.seconds)
async fn delete_user(req: Request<Void>) -> Response<Void>
  needs DB
{
  let caller = ctx.get(AuthUser) ?? return Response.unauthorized()

  guard caller.role == .admin else {
    return Response.forbidden("Admin role required")
  }

  try db.delete(User, req.params.id)
  return Response.no_content()
}

// ─── Token refresh ────────────────────────────────────

@route("POST /api/auth/refresh")
async fn refresh(req: Request<RefreshBody>) -> Response<TokenPair>
  needs DB
{
  let old_claims = try jwt.verify<Claims>(signer, req.body.refresh_token)

  // Check refresh token hasn't been revoked
  guard try db.exists(RefreshToken, old_claims.sub) else {
    return Response.unauthorized()
  }

  let user = try db.find(User, old_claims.sub)
  let access  = try issue_token(user)
  let refresh = try jwt.sign(signer, RefreshClaims {
    sub: user.id, exp: now() + 7.days, iat: now(),
  })

  return Response.ok(TokenPair { access, refresh })
}
```

> **Adapter pattern in action:** `jwt.hmac256()`, `jwt.rsa256()`, and `jwt.ecdsa()` ship in stdlib. Need a KMS-backed signer? Implement the `JWTSigner` constraint and wire it in. Application code doesn't change.

```Splash
// The signer constraint — implement this to bring your own
constraint JWTSigner {
  fn sign(self, payload: Bytes) -> Result<Bytes, SignError>
  fn verify(self, token: Bytes, signature: Bytes) -> Result<Bool, VerifyError>
  fn algorithm(self) -> String
}
```

---

## 12 — Migrations

Splash migrations are code — not SQL strings in numbered files. The compiler type-checks migrations against your model types, catches "you added a field but forgot the migration" errors, and guarantees every migration has a matching rollback. Tooling follows the `golang-migrate` philosophy: explicit, versioned, up/down, with strong repair tooling.

```Splash
// migrations/003_add_user_location.migration.splash

migration "003_add_user_location" {
  description: "Add optional location field to users"
  depends_on:  "002_add_user_email_index"

  up {
    alter_table(User) {
      add_column(location: GeoPoint?, default: none)
    }
    create_index(User, .location, type: .gist)
  }

  down {
    alter_table(User) {
      drop_column(.location)
    }
  }
}
```

```Splash
// migrations/004_sermon_embeddings.migration.splash

migration "004_sermon_embeddings" {
  description: "Add vector column for sermon embeddings"
  depends_on:  "003_add_user_location"

  up {
    alter_table(Sermon) {
      add_column(embedding: Vector(768)?, default: none)
    }
    create_index(Sermon, .embedding, type: .hnsw, options: {
      m: 16, ef_construction: 200
    })
  }

  down {
    alter_table(Sermon) {
      drop_column(.embedding)
    }
  }
}
```

```Splash
// What the compiler catches at build time:

type User {
  id:       UserId
  display:  String
  @sensitive
  email:    Email
  @sensitive
  location: GeoPoint?       // ← exists in type
}

// ❌ COMPILE ERROR: User.location is defined in the type but
//    no migration creates this column.
//    Expected migration after "002_add_user_email_index"

// ❌ COMPILE ERROR: migration "003_add_user_location" adds
//    column `location` as GeoPoint but User.location is
//    GeoPoint? (nullable). Type mismatch.

// ❌ COMPILE ERROR: migration "005_drop_legacy" has no `down` block.
//    All migrations must define rollback behavior.
//    Use `down { irreversible("reason") }` to explicitly mark.
```

```shell
# Migration CLI — inspired by golang-migrate

$ Splash migrate status
  001_initial_schema          ✓ applied  2026-03-15T10:00:00Z
  002_add_user_email_index    ✓ applied  2026-03-20T14:00:00Z
  003_add_user_location       ○ pending
  004_sermon_embeddings       ○ pending

# Apply all pending
$ Splash migrate up
  ✓ 003_add_user_location     applied in 0.4s
  ✓ 004_sermon_embeddings     applied in 1.2s

# Roll back the last one
$ Splash migrate down 1
  ✓ 004_sermon_embeddings     rolled back in 0.3s

# Roll back to a specific version
$ Splash migrate goto 002
  ✓ 004_sermon_embeddings     rolled back
  ✓ 003_add_user_location     rolled back

# Force-set version (repair after manual intervention)
$ Splash migrate force 003
  ⚠ Forcing version to 003_add_user_location (dirty state cleared)

# Generate a new migration from type diff
$ Splash migrate gen "add_sermon_summary"
  → migrations/005_add_sermon_summary.migration.splash (scaffolded from type diff)

# Validate migrations match types without running them
$ Splash migrate check
  ✓ All migrations consistent with model types
  ✓ All migrations have rollback blocks
  ✓ No version gaps
  ✓ Dependency graph is acyclic
```

> **The compiler guarantee:** If you add a field to a type and forget the migration, the build fails. If you write a migration that doesn't match the type's classification (e.g. adding a `String` column for an `@sensitive Email` field), the build fails. If you write a migration without a `down` block, the build fails. Migrations are code, and code gets compiled.

---

## 13 — Storage

`std/storage` abstracts over S3-compatible backends with full data classification enforcement. An uploaded document marked `@sensitive` stays encrypted and audited. The adapter pattern means you swap from S3 to R2 to GCS with a one-line change in `main()`.

```Splash
// storage.splash

use std/storage

// ─── The adapter constraint ───────────────────────────

constraint StoreAdapter {
  async fn put(self, key: String, data: Bytes, opts: PutOptions) -> Result<StoreMeta, StoreError>
  async fn get(self, key: String) -> Result<StoreObject, StoreError>
  async fn delete(self, key: String) -> Result<Void, StoreError>
  async fn list(self, prefix: String, opts: ListOptions) -> Result<Page<StoreMeta>, StoreError>
  async fn presign_url(self, key: String, expires: Duration) -> Result<URL, StoreError>
  async fn ping(self) -> Result<Void, StoreError>
}

// ─── Built-in adapters ────────────────────────────────

let store = S3Store.new({
  bucket:   env.require("S3_BUCKET"),
  region:   env.get("AWS_REGION") ?? "us-west-2",
  encrypt:  .aes256,                    // server-side encryption default
})

// Or: R2Store.new(...), GCSStore.new(...), LocalStore.new("./uploads")

// ─── Upload with classification ───────────────────────

@route("POST /api/sermons/{id}/audio")
@deadline(60.seconds)
async fn upload_audio(req: Request<MultipartBody>) -> Response<AudioMeta>
  needs Store, DB
{
  let sermon = try db.find(Sermon, req.params.id)
  let file = req.body.file("audio")

  let meta = try store.put("sermons/{sermon.id}/audio.mp3", file.bytes, {
    content_type: "audio/mpeg",
    classification: .internal,          // not public, but not PII
    max_size: 500.megabytes,
    metadata: { sermon_id: sermon.id.to_string() },
  })

  try db.update(sermon, { audio_key: meta.key, audio_size: meta.size })

  return Response.created(AudioMeta.from(meta))
}

// ─── Sensitive document handling ──────────────────────

@route("POST /api/users/{id}/documents")
async fn upload_id_document(req: Request<MultipartBody>) -> Response<DocMeta>
  needs Store, DB
{
  let file = req.body.file("document")

  // @sensitive storage: encrypted at rest, access logged, presigned URLs only
  let meta = try store.put("users/{req.params.id}/id-doc", file.bytes, {
    classification: .sensitive,         // compiler enforces encryption
    content_type: file.content_type,
    max_size: 10.megabytes,
  })

  return Response.created(DocMeta.from(meta))
}

// ─── Classification enforced at the storage layer ─────

// ❌ COMPILE ERROR: cannot store @restricted data in external storage
//    @restricted values are process-local and cannot cross boundaries
let ssn_bytes = user.ssn.to_bytes()
try store.put("bad-idea", ssn_bytes, { classification: .restricted })

// ✅ @sensitive data requires encryption — the adapter enforces it
// If you set classification: .sensitive and the adapter doesn't
// support encryption, it's a compile-time error.
```

---

## 14 — Resilience

`std/resilience` provides circuit breakers, retry policies, bulkheads, and fallbacks as composable primitives. These wrap outbound calls — when Stripe is down, stop hammering it. Follows the adapter pattern: state can be stored in-memory (single instance) or Redis (distributed).

```Splash
// resilience.splash

use std/resilience

// ─── Circuit breaker ──────────────────────────────────

let stripe_breaker = CircuitBreaker.new({
  name:              "stripe",
  failure_threshold: 5,               // open after 5 failures
  reset_timeout:     30.seconds,      // try half-open after 30s
  success_threshold: 2,               // close after 2 successes in half-open
  state_store:       RedisState.new(env.require("REDIS_URL")),  // distributed
  // Or: MemoryState.new() for single-instance
})

// ─── Retry policy ─────────────────────────────────────

let stripe_retry = RetryPolicy.new({
  max_attempts: 3,
  backoff:      .exponential(base: 100.ms, max: 5.seconds),
  jitter:       true,
  retry_on:     [StripeError.timeout, StripeError.rate_limited],
  abort_on:     [StripeError.invalid_card],   // don't retry user errors
})

// ─── Compose them ─────────────────────────────────────

// Resilience policies compose: retry wraps the call, breaker wraps the retry
async fn charge_customer(amount: Money, method: PaymentMethod) -> Result<Charge, PaymentError>
  needs Net
{
  return stripe_breaker.call {
    stripe_retry.execute {
      try stripe.charge(amount, method)
    }
  }
}

// ─── Fallback on breaker open ─────────────────────────

async fn get_exchange_rate(from: Currency, to: Currency) -> Result<Decimal, RateError>
  needs Net, Cache
{
  return rate_breaker.call_with_fallback(
    primary: { try rate_api.get(from, to) },
    fallback: {
      // When the API circuit is open, fall back to cached rate
      let cached = cache.get<Decimal>("rate:{from}:{to}")
      cached ?? return err(RateError.unavailable)
    }
  )
}

// ─── Bulkhead — limit concurrent calls ────────────────

let stripe_bulkhead = Bulkhead.new({
  name:            "stripe",
  max_concurrent:  10,                // max 10 in-flight Stripe calls
  max_wait:        5.seconds,         // queue for 5s then reject
})

async fn safe_charge(amount: Money, method: PaymentMethod) -> Result<Charge, PaymentError>
  needs Net
{
  return stripe_bulkhead.execute {
    stripe_breaker.call {
      stripe_retry.execute {
        try stripe.charge(amount, method)
      }
    }
  }
}

// ─── Observability built in ───────────────────────────
// Circuit breakers auto-emit metrics:
//   resilience.circuit.{name}.state      gauge (0=closed, 1=half, 2=open)
//   resilience.circuit.{name}.calls      counter (success/failure/rejected)
//   resilience.retry.{name}.attempts     histogram
//   resilience.bulkhead.{name}.queued    gauge
```

> **Adapter pattern:** Circuit breaker state is behind the `StateStore` constraint. `MemoryState` for single-process, `RedisState` for distributed fleets. Same breaker config, different state backend, zero application code changes.

---

## 15 — AI Safety

Splash treats AI safety as a systems engineering problem, not a philosophy debate. Some guarantees are enforced by the compiler — they're structural, and no agent or human can bypass them. Others are runtime observability concerns handled by `std/safety`. The line is clear: if it's about **preventing** dangerous behavior, the compiler owns it. If it's about **detecting** unexpected behavior, the runtime owns it.

### Compiler-Enforced

#### `@approve` — Human-in-the-Loop as a Keyword

When an agent is about to do something with real-world consequences, the runtime pauses execution and waits for human approval. This isn't a library call you can forget — the compiler *requires* it on functions that meet certain effect criteria when called from an agent context.

```Splash
// safety_approve.splash

use std/safety

// ─── Explicit approval gates ──────────────────────────

@approve(
  prompt: "Charge {amount} to {method.last4}?",
  timeout: 5.minutes,           // auto-reject if no human responds
  on_timeout: .reject,          // or .approve for low-risk defaults
)
async fn charge_card(amount: Money, method: PaymentMethod) -> Result<Charge, PaymentError>
  needs Net
{ ... }

// When an agent calls this, execution suspends:
//
//   ┌─────────────────────────────────────────────┐
//   │  🛑 Agent requires approval                 │
//   │                                             │
//   │  Action: charge_card                        │
//   │  Charge $49.99 to •••• 4242?                │
//   │                                             │
//   │  Agent reasoning:                           │
//   │  "User requested premium plan upgrade.      │
//   │   Verified account is active, card on file  │
//   │   matches billing profile."                 │
//   │                                             │
//   │  [Approve]  [Reject]  [Inspect Context]     │
//   └─────────────────────────────────────────────┘

// ─── Compiler-enforced approval requirements ──────────

// The compiler can REQUIRE @approve based on effect combinations.
// Configured in Splash.policy:

policy "owyhee-holdings" {
  agent_policy {
    // Any fn with DB.write + Net called from Agent context
    // MUST have @approve or @agent_allowed
    require_approve: [DB.write + Net]

    // Any fn that touches @sensitive data from Agent context
    // MUST have @approve
    require_approve_on_sensitive: true
  }
}

// This WON'T compile if called from an agent without @approve:
async fn send_receipt(user: User, order: Order) -> Result<Void, EmailError>
  needs Net, DB.write
{
  // ❌ COMPILE ERROR: fn has effects [Net, DB.write] and is reachable
  //    from Agent context, but is missing @approve or @agent_allowed.
  //    Add @approve(...) to require human approval, or
  //    @agent_allowed(reason: "...") to explicitly permit.
}

// ─── @agent_allowed — explicit opt-out with justification ─

// For agent-callable fns that don't need approval, you must
// explicitly say so and explain why. The compiler requires the reason.
@agent_allowed(reason: "Read-only search, no side effects beyond DB read")
@tool
fn search_sermons(query: String, limit: Int = 10) -> List<SermonResult>
  needs DB.read
{ ... }
```

#### `@redline` — Absolute Prohibitions

Some operations are so dangerous that no agent can ever perform them, regardless of sandboxing, approval gates, or budget. `@redline` is a compiler-enforced prohibition: any call path from an `Agent` context to a `@redline` function is a compile error. No configuration can relax this.

```Splash
// safety_redline.splash

// ─── Functions no agent can ever call ─────────────────

@redline(reason: "Schema mutations require human DBA review")
fn drop_table(table: String) needs DB.admin { ... }

@redline(reason: "Security policy changes require human approval chain")
fn update_policy(policy: Policy) needs FS { ... }

@redline(reason: "Signing key rotation is a manual security operation")
fn rotate_jwt_secret(new_key: SecretKey) needs Secrets.write { ... }

@redline(reason: "User deletion has legal and compliance implications")
fn hard_delete_user(id: UserId) needs DB.write { ... }

@redline(reason: "Billing configuration affects all customers")
fn update_pricing(plans: List<Plan>) needs DB.write { ... }

// ─── The compiler traces call graphs ──────────────────

// Fine — human-initiated route
@route("DELETE /admin/users/{id}")
async fn admin_delete_user(req: Request<Void>) -> Response<Void> {
  let caller = ctx.get(AuthUser) ?? return Response.unauthorized()
  guard caller.role == .admin else { return Response.forbidden() }
  try hard_delete_user(req.params.id)     // ✅ human context
  return Response.no_content()
}

// WON'T compile — agent can reach a @redline fn
@sandbox(allow: [DB.write])
async fn agent_cleanup(goal: String) -> Result<AgentResult, AgentError>
  needs Agent
{
  return agent.execute(goal, tools: [
    hard_delete_user,     // ❌ COMPILE ERROR: @redline fn "hard_delete_user"
                          //    is not callable from Agent context.
                          //    This restriction cannot be overridden.
  ])
}
```

#### `@containment` — Module-Level Agent Boundaries

Entire modules can be declared off-limits to agent-generated code and agent runtime execution. Broader than `@redline` on individual functions — a blanket declaration that a module is human-only territory.

```Splash
// billing.splash

@containment(agent: .none)          // no agent can call ANY fn in this module
module billing

// Everything in here is invisible to agents.
// The compiler removes these from tool registration,
// agent call graphs, and agent-visible type exports.

fn update_subscription(user: UserId, plan: Plan) needs DB, Net { ... }
fn process_refund(charge: ChargeId, amount: Money) needs DB, Net { ... }
fn generate_invoice(user: UserId, period: DateRange) needs DB { ... }
```

```Splash
// Containment levels:

@containment(agent: .none)          // agents can't touch anything
@containment(agent: .read_only)     // agents can call read fns, not write
@containment(agent: .approved_only) // all fns require @approve from agent context
```

#### Capability Decay

Agent permissions narrow over time within a session. An agent granted `DB.read` to answer a question shouldn't still have that permission 30 minutes later in an open-ended loop. Capabilities have TTLs, and the runtime enforces them.

```Splash
// safety_decay.splash

@sandbox(allow: [DB.read, AI, Net])
@budget(max_cost: Cost.usd(2.00), max_calls: 50)
@capability_decay({
  DB.read: { ttl: 10.minutes },       // DB access expires after 10min
  Net:     { ttl: 5.minutes },        // network access expires after 5min
  AI:      { ttl: 30.minutes },       // AI calls last longer
})
async fn research_agent(goal: String) -> Result<Research, AgentError>
  needs Agent
{
  return agent.execute(goal, tools: [search_sermons, lookup_verse, web_search])
}

// After 5 minutes, the agent loses Net access.
// After 10 minutes, it loses DB.read.
// The runtime returns CapabilityExpired errors for those effects.
// The agent can request renewal — which triggers @approve if configured.

policy "owyhee-holdings" {
  agent_policy {
    capability_renewal: .require_approve   // human must re-grant expired caps
    // Or: .auto_renew (for low-risk scenarios)
    // Or: .deny (expired means done)
  }
}
```

### Runtime / `std/safety`

> **API Stability:** The `std/safety` runtime APIs (provenance chains, drift detection, capability decay, output contracts) are marked **unstable in v0.1**. The compiler-enforced primitives (`@redline`, `@containment`, `@approve`, `@agent_allowed`) are stable — they will not change without a major version. The runtime APIs ship so teams can use them, break them, and tell us what actually matters in production. Survivors get promoted to stable in v0.2.

#### Provenance Chains

Every mutation in the system is traceable to either a human action or an agent decision, with the full reasoning chain attached. Not just "who did it" but "why did it think this was right."

```Splash
// safety_provenance.splash

use std/safety

// When an agent calls a @tool fn, the runtime automatically records:
//
//   {
//     action_id:   "act_8f3k2j",
//     timestamp:   "2026-04-01T14:23:01Z",
//     actor:       { type: "agent", session: "sess_abc", model: "grok-4-1-fast" },
//     function:    "search_sermons",
//     input:       { query: "grace in Romans", limit: 5 },
//     output:      { results: [...], count: 5 },
//     reasoning:   "User asked about grace. Searching Romans-specific sermons.",
//     parent:      "act_7x2m1n",
//     context:     { goal: "Answer user question about grace", step: 3 of 5 },
//     effects:     ["DB.read"],
//     cost:        0.002,
//     duration_ms: 145,
//   }

// ─── Query the provenance chain ───────────────────────

@route("GET /admin/provenance/{action_id}")
async fn get_provenance(req: Request<Void>) -> Response<ProvenanceChain>
  needs DB
{
  let chain = try safety.provenance.trace(req.params.action_id)

  // Returns the full decision tree:
  // act_5a1b2c: agent.execute("Answer question about grace")
  //   → act_6d3e4f: search_sermons(query: "grace in Romans")
  //   → act_7x2m1n: lookup_verse(reference: "Romans 5:8")
  //   → act_8f3k2j: ai.prompt<Answer>(...)

  return Response.ok(chain)
}

// Human actions recorded too — with authenticated user, no reasoning field.

// Provenance storage is behind an adapter:
constraint ProvenanceAdapter {
  async fn record(self, entry: ProvenanceEntry) -> Result<Void, ProvenanceError>
  async fn trace(self, action_id: ActionId) -> Result<ProvenanceChain, ProvenanceError>
  async fn query(self, filter: ProvenanceFilter) -> Result<Page<ProvenanceEntry>, ProvenanceError>
}
// adapters: PostgresProvenance, S3Provenance, StdoutProvenance (dev)
```

#### Drift Detection

If an agent's behavior deviates from expected patterns — more API calls than projected, accessing unfamiliar data, spending faster than linear — the runtime detects it and can pause or halt the session.

```Splash
// safety_drift.splash

use std/safety

@sandbox(allow: [DB.read, AI])
@budget(max_cost: Cost.usd(1.00))
@drift_policy({
  call_rate:      .halt_on(3.0x_median),       // halt if > 3x median calls
  data_access:    .alert_on(new_tables),        // alert on unfamiliar data
  cost_velocity:  .halt_on(2.0x_projected),     // halt if burning cash too fast
  call_pattern:   .alert_on(sequence_anomaly),  // alert on unusual tool order
})
async fn analysis_agent(goal: String) -> Result<Analysis, AgentError>
  needs Agent
{ ... }

// Drift events integrate with std/metric and std/trace:
//   metric: safety.drift.{policy}.triggered   counter
//   span:   safety.drift.halt                 span event on the agent trace

// Baselines are learned per goal-type over time.
// First runs use conservative defaults.
constraint DriftBaselineAdapter {
  async fn get_baseline(self, goal_type: String) -> Result<DriftBaseline?, DriftError>
  async fn update_baseline(self, goal_type: String, session: DriftSession) -> Result<Void, DriftError>
}
```

#### Output Validation Contracts

Before agent-generated output reaches a user or another system, it passes through semantic validation — not just schema checks, but meaning checks.

```Splash
// safety_output.splash

use std/safety

type MedicalSummary {
  condition:  String
  overview:   String
  sources:    List<Citation>
}

@output_contract(MedicalSummary, [
  must_not_contain(fields: [.overview], patterns: [
    "you should take", "I recommend", "increase your dosage",
  ], reason: "Must not contain treatment recommendations"),

  must_contain(fields: [.sources], min: 1,
    reason: "Must cite at least one source"),

  must_contain(fields: [.overview],
    patterns: ["consult", "healthcare provider", "medical professional"],
    reason: "Must include professional consultation disclaimer"),
])

// When ai.prompt<MedicalSummary> returns, the contract is checked
// BEFORE the response reaches the caller.
// Violation → err(AIError.output_contract_violation(...))

// Financial outputs:
type FinancialAnalysis {
  summary:     String
  projections: List<Projection>
  disclaimer:  String
}

@output_contract(FinancialAnalysis, [
  must_not_contain(fields: [.summary], patterns: [
    "guaranteed return", "risk-free", "certain to",
  ], reason: "Must not contain financial guarantees"),

  field_required(.disclaimer, reason: "Disclaimer is mandatory"),
  min_length(fields: [.disclaimer], min: 50,
    reason: "Disclaimer must be substantive, not boilerplate"),
])
```

### AI Safety at a Glance

| Layer | Mechanism | Enforced By | Bypassable? |
|---|---|---|---|
| `@redline` | Absolute prohibition — agent can never call | Compiler | No. Not by config, flags, or code. |
| `@containment` | Entire modules off-limits to agents | Compiler | No. Module-level declaration. |
| `@approve` | Human must approve before execution | Compiler + Runtime | No (if policy requires it). |
| `@agent_allowed` | Explicit opt-in for agent-callable fns | Compiler | Requires stated reason. |
| `@capability_decay` | Permissions expire over time | Runtime | Renewal requires approval. |
| Provenance chains | Full decision tree for every mutation | Runtime (`std/safety`) | Always on. Can't disable in prod. |
| Drift detection | Behavioral anomaly detection | Runtime (`std/safety`) | Thresholds configurable. Detection always on. |
| Output contracts | Semantic validation of agent outputs | Runtime (`std/safety`) | Contracts are compile-time declarations. |

> **The Anthropic pitch:** "Splash doesn't just sandbox agents — it makes AI alignment observable and enforceable at the systems level. Every agent action has provenance. Every dangerous operation has a gate. Every permission decays. Every output is validated. And the compiler guarantees you can't ship a system that skips any of it."

---

## 16 — Modules & Supply Chain Safety

Every module declares its maximum effect permissions. Your lockfile records what you've granted. A dependency can't escalate privileges between versions. Transitive dependencies are recursively audited. There are no build-time hooks. Unverified packages are gated. Organizational policies enforce guardrails across every project.

```Splash
// Splash.mod

module thatsermon/api

Splash 0.1.0

require {
  std/http                       0.1
  std/db                         0.1
  std/ai                         0.1
  pkg/stripe                     2.1.0
  github/someone/markdown        1.4.2
}

permissions {
  pkg/stripe:                    [Net]
  github/someone/markdown:       []          // pure — zero effects
}
```

```toml
# Splash.lock (auto-generated, committed to git)

[[package]]
name     = "pkg/stripe"
version  = "2.1.0"
hash     = "sha256:a1b2c3d4..."
effects  = ["Net"]                  # locked — v2.2.0 adding DB = build failure

[[package]]
name     = "github/someone/markdown"
version  = "1.4.2"
hash     = "sha256:e5f6g7h8..."
effects  = []                        # pure — v1.5.0 adding Net = build failure

[[package]]
name     = "pkg/retry"              # transitive dep of pkg/stripe
version  = "1.0.3"
hash     = "sha256:i9j0k1l2..."
effects  = []
parent   = "pkg/stripe"            # can't exceed parent's grants
```

```shell
# Adding a dependency — explicit permission grant
$ Splash add pkg/stripe
  ℹ pkg/stripe@2.1.0 requests effects: [Net]
  ? Grant Net to pkg/stripe? [y/n] y
  ✓ Added pkg/stripe@2.1.0 with effects [Net]

# Version bump detects privilege escalation
$ Splash update pkg/stripe
  ⚠ pkg/stripe@2.2.0 requests NEW effects: [Net, DB]
  ⚠ Previously granted: [Net]
  ⚠ New effect requested: [DB]
  ? Grant DB to pkg/stripe? [y/n] n
  ✗ Update aborted — pkg/stripe@2.2.0 requires DB effect

# Typosquatting protection
$ Splash add pkg/striipe
  ⚠ pkg/striipe has 0 downloads and no verified publisher
  ⚠ Did you mean pkg/stripe (2.4M downloads, verified)?
  ✗ Use --trust-unverified to install unverified packages
    (AI agents cannot pass this flag)

# Full dependency audit
$ Splash audit
  thatsermon/api
  ├── std/http       0.1.0  [Net]
  ├── std/db         0.1.0  [DB]
  ├── std/ai         0.1.0  [Net, AI]
  ├── pkg/stripe     2.1.0  [Net]
  │   └── pkg/retry  1.0.3  []     ← transitive, pure
  └── github/someone/markdown 1.4.2  []

  ✓ No ungranted effects
  ✓ All hashes match lockfile
  ✓ 0 known vulnerabilities
  ✓ No build-time hooks
```

**Four attack vectors, eliminated at the language level:**

1. **Silent privilege escalation:** A version bump adding a new effect requires explicit human approval. The lockfile diff is the audit trail.
2. **Transitive dep compromise:** If `pkg/retry` starts making network calls, `pkg/stripe`'s `max_effects` must change — cascading up to your lockfile.
3. **Typosquatting:** Unverified, zero-download packages require `--trust-unverified` which AI agents cannot pass.
4. **Build-time code execution:** No `postinstall`, no `build.rs`, no `setup.py`. Compilation is pure. Code generation runs in a sandbox with zero effects.

```Splash
// Splash.policy — organizational guardrails

policy "owyhee-holdings" {

  deny_effects:           [FS]
  net_proxy:              "https://egress.internal.owyhee.dev"

  require_publisher:      verified
  max_dep_effects:        2
  block_vulnerabilities:  critical, high

  ci_lockfile_review:     true

  agent_policy {
    can_add_deps:          true
    can_grant_effects:     false     // human must approve
    can_trust_unverified:  false
    can_update_policy:     false
    max_budget_per_call:   Cost.usd(1.00)
  }
}

// Per-project: inherits and restricts further — never relaxes
policy "thatsermon" extends "owyhee-holdings" {
  deny_effects:    [FS, Exec]
  max_dep_effects: 1
}
```

> **The CISO pitch:** "Your dependency tree is a capability graph. Every package declares what it can do. Your team explicitly grants permissions. The lockfile records those grants. CI blocks unauthorized changes. AI agents physically cannot escalate privileges. It's auditable, SOC 2 friendly, and something you can actually reason about in a risk assessment."

---

## 17 — CLI

The Splash CLI is the developer's primary interface — project scaffolding, code generation, dev server, build, deploy, and migration management in one tool. Designed so AI agents can invoke the same commands humans use, with the same policy restrictions.

```shell
# ─── Project scaffolding ───────────────────────────────

$ Splash new thatsermon-api
  ✓ Created thatsermon-api/
    ├── Splash.mod
    ├── Splash.policy
    ├── src/
    │   └── main.splash
    ├── migrations/
    │   └── 001_initial.migration.splash
    └── tests/
        └── main_test.splash

$ Splash new thatsermon-api --template api
  # Scaffolds with std/http routes, health checks, JWT middleware

$ Splash new thatsermon-api --template worker
  # Scaffolds with std/queue subscriber, retry policies

# ─── Code generation ──────────────────────────────────

$ Splash gen model Sermon
  → src/models/sermon.splash (scaffolded with id, created, updated)

$ Splash gen route "POST /api/sermons"
  → src/routes/sermons.splash (handler with @route, @validate, @deadline)

$ Splash gen migration "add_sermon_embeddings"
  → migrations/005_add_sermon_embeddings.migration.splash

$ Splash gen tool search_sermons
  → src/tools/search_sermons.splash (scaffolded @tool fn with doc comments)

# ─── Development ───────────────────────────────────────

$ Splash dev
  listening on :8080
  traces → stdout (pretty)
  db → postgres://localhost/thatsermon
  hot reload enabled — watching src/**/*.splash

$ Splash dev --adapters memory
  # All adapters swapped to in-memory (no Postgres, no Redis needed)
  # Perfect for rapid prototyping and vibe coding

# ─── Build & test ──────────────────────────────────────

$ Splash build
  ✓ type check            0.8s
  ✓ generics monomorphize 0.3s
  ✓ effect check          0.2s
  ✓ data classification   0.1s
  ✓ tool schema gen       0.1s
  ✓ migration check       0.2s
  ✓ permission audit      0.1s
  ✓ policy check          0.0s
  ✓ deploy policy         0.0s
  ✓ compile               1.6s
  → build/app (13.2 MB, linux/amd64)

$ Splash test
  ✓ test_create_order          0.01s
  ✓ test_search_sermons        0.03s
  ✓ test_jwt_round_trip        0.00s
  ✓ test_migration_003_up      0.12s
  ✓ test_migration_003_down    0.08s
  5 passed, 0 failed (0.24s)

$ Splash test --coverage
  # Coverage report with effect coverage — which effect
  # combinations have been tested

# ─── Deploy ────────────────────────────────────────────

$ Splash deploy --target railway
  ✓ env vars present
  ✓ migrations applied (2 pending)
  ✓ canary deployed (5%)
  ⏳ monitoring for 10m...
  ✓ error rate 0.00% — promoting to 100%
  ✓ deployed v1.4.2

# ─── Migrations (standalone) ───────────────────────────

$ Splash migrate status
$ Splash migrate up
$ Splash migrate down 1
$ Splash migrate goto 002
$ Splash migrate force 003
$ Splash migrate gen "add_field"
$ Splash migrate check

# ─── Dependency management ─────────────────────────────

$ Splash add pkg/stripe
$ Splash update pkg/stripe
$ Splash audit
$ Splash outdated
```

> **`Splash dev --adapters memory`** is the vibe coding enabler. Zero infrastructure to start. No Docker, no Postgres, no Redis. Just `Splash new myapp && Splash dev --adapters memory` and you're writing handlers. Swap to real backends when you're ready — the adapter pattern means zero code changes.

---

## 18 — Standard Library

Every module follows the adapter pattern. Application code depends on constraint interfaces. Backends are swapped at startup.

```Splash
// ─── Core ──────────────────────────────────────────────
use std/db          // query, find, insert, update, transact, migrate
                    // adapters: PostgresDB, SQLiteDB, MySQLDB
use std/cache       // get, set, get_or_set, invalidate, ttl
                    // adapters: RedisCache, MemoryCache
use std/http        // server, client, @route, @validate, @rate_limit
use std/ai          // prompt<T>, embed, stream, @tool, @sandbox, @budget
                    // adapters: OpenAIAdapter, AnthropicAdapter, GrokAdapter
use std/queue       // publish, @subscribe, @retry, dead letter
                    // adapters: SQSQueue, RedisQueue, NATSQueue, MemoryQueue
use std/storage     // put, get, delete, list, presign_url
                    // adapters: S3Store, GCSStore, R2Store, LocalStore

// ─── Auth & Crypto ─────────────────────────────────────
use std/jwt         // sign, verify, decode, refresh
                    // adapters: HMACSigner, RSASigner, KMSSigner
use std/crypto      // sha256, hmac, encrypt, decrypt
use std/secrets     // get (@restricted), rotate

// ─── Resilience ────────────────────────────────────────
use std/resilience  // CircuitBreaker, RetryPolicy, Bulkhead, Fallback
                    // state adapters: MemoryState, RedisState

// ─── AI Safety ─────────────────────────────────────────
use std/safety      // provenance, drift detection, output contracts
                    // @approve, @redline, @containment (compiler-enforced)
                    // adapters: PostgresProvenance, S3Provenance, StdoutProvenance
                    //           MemoryBaseline, RedisBaseline

// ─── Concurrency & Context ─────────────────────────────
use std/async       // group, race, Channel, delay, Mutex, Semaphore
use std/context     // @deadline, within, ctx.check, ctx.remaining

// ─── Observability ─────────────────────────────────────
use std/metric      // count, histogram, gauge
                    // adapters: OTLPMetric, PrometheusMetric, StdoutMetric
use std/health      // checks, readiness, liveness
use std/trace       // @trace, span, timed (auto-export to OTLP)

// ─── Lifecycle ─────────────────────────────────────────
use std/schedule    // @every, @cron
use std/test        // @test, assert, MockDB, MockNet, FrozenClock, MemoryStore
```

---

## 19 — Deployment

Single statically-linked binary. Embedded migrations, health checks, graceful shutdown. Deploy policies checked at compile time and enforced at deploy time.

```Splash
// main.splash

module main

fn main() {
  let server = http.server({
    port:     env.get("PORT") ?? 8080,
    routes:   autodiscover(),
    health:   health.checks([db.ping, cache.ping, store.ping]),
    shutdown: .graceful(30.seconds),
    adapters: {
      db:     PostgresDB.new(env.require("DATABASE_URL")),
      cache:  RedisCache.new(env.require("REDIS_URL")),
      store:  S3Store.new(env.require("S3_BUCKET")),
      ai:     GrokAdapter.new(secrets.get("GROK_KEY")),
      queue:  SQSQueue.new(env.require("SQS_URL")),
    }
  })
  server.start()
}

@deploy {
  require_env:  ["DATABASE_URL", "REDIS_URL", "S3_BUCKET", "GROK_KEY"]
  migrations:   .auto
  canary:       5.percent for 10.minutes
  max_replicas: 20
  rollback_on:  error_rate > 0.5.percent
}
```

---

## 20 — At a Glance

| Concern | Today's Stack | Splash |
|---|---|---|
| PII in logs | Runtime scanning / prayer | Compile-time data classification |
| LLM calls | Untyped JSON, no budget | Typed, budgeted, cached, traced |
| Structured outputs | Hand-written JSON Schema | Type signature IS the schema |
| AI tool calling | Manual OpenAPI specs | `@tool` on a function |
| Agent sandboxing | Docker + hope | Effect-system capability bounds |
| Generics | Varies wildly | Flat constraint bounds |
| Concurrency | Goroutines / async-await | Structured — no orphans, no leaks |
| Context / deadlines | `ctx` as first param | Implicit propagation + `@deadline` |
| Backend portability | Rewrite half your code | Adapter pattern — one-line swap |
| Auth / JWT | 3rd party lib + config maze | `std/jwt` with `@restricted` keys |
| Database migrations | Separate tool, no type checking | Compiler-verified, up/down required |
| Object storage | Raw S3 SDK | `std/storage` with classification |
| Circuit breakers | Library + manual wiring | `std/resilience` composable primitives |
| Dep privilege escalation | Invisible in lockfile | Lockfile diff + human approval |
| Transitive dep attacks | Dependabot (reactive) | Recursive effect audit (proactive) |
| Typosquatting | Hope | `--trust-unverified` gate |
| Build-time hooks | `postinstall` runs anything | No hooks. Compilation is pure. |
| Org-level guardrails | Wiki page nobody reads | `Splash.policy` enforced at build |
| Agent dep management | Unrestricted | `agent_policy` — no escalation |
| Observability | 3 libraries, 200 LOC init | Language primitive, zero config |
| Null safety | Depends on language | No null. Period. |
| Error handling | Exceptions / panic / ignore | Result types, exhaustive matching |
| Dev onboarding | Docker + .env + README | `Splash new && Splash dev --adapters memory` |
| Agent danger zones | Hope the prompt is good enough | `@redline` + `@containment` — compiler-enforced |
| Human-in-the-loop | Manual integration per vendor | `@approve` keyword — compiler requires it |
| Agent permission creep | Persistent until process dies | `@capability_decay` — TTLs on effects |
| Agent audit trail | Grep the logs | Provenance chains — full decision tree |
| Agent behavior monitoring | Custom dashboards | Drift detection — anomaly halt/alert |
| Agent output safety | Pray the model behaves | `@output_contract` — semantic validation |
| Deploy safety | Manual canary scripts | Declarative `@deploy` block |

---

## 21 — Implementation Roadmap

Splash compiles to Go. `splash build` is a frontend that emits Go source and wraps `go build`, producing a single statically-linked binary. This gives Splash Go's runtime (goroutines, GC, fast compilation, excellent tooling) while keeping the Splash type system, effect checks, and safety primitives in the compiler frontend.

### Phase 1 — Parser & Type Checker

- Lexer and parser for Splash syntax (`.splash` files)
- Type inference and constraint checking (generics, optionals, Result)
- Module system and `expose` declarations
- Error reporting with source locations

### Phase 2 — Effect System & Go Codegen

- Effect declaration and propagation (`needs DB, Net, Clock`)
- Call graph analysis (foundation for `@approve` and `@redline` checks)
- `@redline` and `@containment` enforcement via call graph tracing
- `@approve` gate insertion — compiler rewrites agent-reachable calls to suspend and await human approval
- Data classification checks (`@sensitive`, `@restricted` — block `Loggable`, log masking)
- Go code generation — Splash types, functions, and effects map to idiomatic Go
- `splash build` and `splash dev` CLI wrappers

### Phase 3 — Stdlib Adapters

- `std/db`, `std/cache`, `std/http`, `std/queue`, `std/storage` with default adapters
- `std/jwt`, `std/crypto`, `std/secrets`
- `std/resilience` (CircuitBreaker, RetryPolicy, Bulkhead)
- `std/ai` with `@tool`, `ai.prompt<T>`, and `@sandbox`/`@budget` enforcement
- `std/metric`, `std/trace`, `std/health` (OTLP export)
- `std/safety` (unstable) — provenance, drift detection, output contracts, capability decay
- Migration tooling (`splash migrate`)

### Compiler Architecture Note

The effect system already requires whole-program call graph analysis for `needs` propagation — every call site must satisfy the callee's declared effects. `@approve` and `@redline` checking piggybacks on this same pass: once the compiler knows which call paths reach `agent.execute()` or a `needs Agent` function, it traces forward from those entry points and flags violations. No second pass needed.

---

*Splash v0.1 — a language for systems you can trust.*

