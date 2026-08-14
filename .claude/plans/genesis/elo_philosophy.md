# ELO Philosophy

> Core architecture invariants, patterns, and anti-patterns.
> These are the rules that must hold across every layer of FAB.
> When in doubt, come back here.

---

## 1. The Axioms

Statements that must be true everywhere, always. If a design decision
violates an axiom, the design is wrong — not the axiom.

### A1 — Git is the source of truth for schema

The registry DB is derived state. YAML files are canonical.
A UI that creates types writes YAML files, not DB rows directly.
`fab schema apply` flows one way: files → DB.
`fab schema pull` is for drift detection, not the primary flow.

### A2 — Generated artifacts are never hand-edited

`gen/` and `migrations/` are outputs of the compiler and Atlas respectively.
If a generated artifact is wrong, fix the schema or the generator — not the artifact.
Exception: files explicitly marked `# manual` in `migrations/` for data transforms
the generator cannot produce. These are escape hatches, not the normal path.

### A3 — The object store is always one table per object type

No EAV. No shared `object_instances` table. No `property_name`/`property_value` rows.
The ontology tells you what the table should look like. Atlas migrations make it so.
This is non-negotiable. See §4 for why.

### A4 — Apps never import provider SDKs directly

No `boto3.client('sqs')`. No `redis.Redis()`. No raw `psycopg2` connections.
The generated OSDK is the only interface. The app is portable across providers
and environments because it never knows which provider it is talking to.

### A5 — A layer cannot modify a type it does not own

A layer may add types, implement interfaces, attach aspects, and link to foreign types.
It may not change a property, rename a type, or delete a link defined in another layer.
Immutable contract boundaries are what make independent layer evolution possible.

### A6 — Cross-layer references are validated at publish time, not runtime

If `meta-auth` extends `meta-core/User` but `meta-core` is not active,
`fab schema publish` fails with a clear error.
A runtime failure caused by a missing layer is a framework bug, not user error.

### A7 — Queries are compiled at publish time, not constructed at request time

Dynamic SQL string building in the hot path is prohibited.
The OSDK for each ontology version ships with pre-compiled, static queries.
PostgreSQL caches the plan. The runtime resolves a tag to a version,
loads that version's query module, executes fixed SQL.

### A8 — Every default is the secure path

Least-privilege IAM roles are generated automatically.
Secrets are never in source — injected at deploy time from the secrets adapter.
HTTPS everywhere. The path to less-secure behavior requires deliberate override.
Security is not a layer you add later. It is baked into every scaffold and generator.

### A9 — Every abstraction has a documented escape hatch

| Abstraction | Escape hatch |
|------------|-------------|
| Generated Terraform | `infra/apps/<name>/override.tf` (Terraform override files) |
| Generated K8s manifests | `infra/k8s/apps/<name>/patch.yaml` (Kustomize patches) |
| Generated OSDK | Raw ontology API client, documented |
| Schema compiler output | `# manual` marker in `migrations/` |
| Layer system | Direct schema YAML editing outside a layer |

Without escape hatches, teams fork. Forks never merge back. The ecosystem fragments.

### A10 — Missing dependencies are build-time errors, not runtime failures

If an app declares `type: queue` but `meta-events` is not active,
`fab build` fails with a clear, actionable error message.
If a layer declares `dependsOn: meta-core` but `meta-core` is not in `foundry.lock`,
`fab resolve` fails before anything is built or deployed.
Fail fast, fail loudly, fail before deployment.

### A11 — FAB is agentic first

AI agents are a primary user of the framework — not a secondary integration.
Every interface FAB exposes (CLI, YAML contracts, MCP server, scaffold generators)
must be as navigable by an agent as by a human engineer.

This is not about adding AI features on top. It means:
- **Declarations over code** — an agent can understand what a layer provides
  from `layer.yaml` alone, without reading implementation code
- **Structured errors** — `fab` commands emit machine-parseable errors.
  An agent that reads a build failure can fix it without human translation
- **Scaffolding is agent-driven** — `fab new` generates working starting points
  using an agent, not static templates. The result is idiomatic, not generic
- **MCP server ships with `meta-elo`** — the ontology, layer graph, and build state
  are queryable by agents at runtime via the Model Context Protocol
- **Agent context files are scaffolded** — every new layer and app gets a
  `CLAUDE.md` with the layer's contract, design decisions, and extension points.
  Agents working on the project always have the right context loaded

If an agent cannot navigate a FAB project from its declarations alone,
the project is under-declared — not the agent's fault.

### A12 — Every technology choice must be reversible

FAB exists so you can always adopt the best tool available.
No provider, language, runtime, or infrastructure component is permanently coupled
to business logic. The adapter system is not a convenience — it is the enforcement
mechanism for this axiom.

```
Today                   Tomorrow
──────                  ────────
auth: cognito      →    auth: auth0          (one line in foundry.yaml)
queue: sqs         →    queue: kafka         (one line in foundry.yaml)
language: python   →    language: go         (replace the package, keep the proto)
task-runner: just  →    task-runner: invoke  (one line in foundry.yaml)
bsp: aws           →    bsp: gcp             (one line in foundry.yaml)
```

If adopting a better tool requires rewriting application code, the abstraction is wrong.
If it requires a one-line change in `foundry.yaml`, the design is correct.
The corollary: never introduce a dependency in app code that bypasses a facade.
A direct `boto3` import is not just a style violation — it is a violation of this axiom.

---

## 2. Core Patterns

How things should be done. These are the right answers to recurring design questions.

### P1 — Declare what, not how

Apps declare `type: queue`. Not `type: sqs-queue`. Not `url: https://sqs...`.
Layers declare `dependsOn: meta-core`. Not `import: layers/meta-core/schema/user.yaml`.
`foundry.yaml` declares `adapter.auth: cognito`. Not the Cognito endpoint, region, and pool ID.
The what is stable. The how changes per environment, per customer, per cloud.

### P2 — Convention over configuration

The directory layout is the documentation. A developer who has never seen
a FAB project should know where everything is without reading the README.

```
foundry.yaml          → "I configure the whole stack"
schema/objects/       → "I define business entities"
schema/actions/       → "I define business operations"
layers/meta-*/        → "I am a reusable domain module"
apps/<name>/          → "I am a deployable service"
apps/<name>/app.yaml  → "I declare what this service needs"
infra/                → "I am generated infrastructure"
gen/                  → "I am generated code"
```

If something does not have an obvious home, the structure needs a new convention —
not an exception to the existing ones.

### P3 — Facades for all external integrations

Every external service is accessed through a facade.
The facade defines the interface. Adapters implement it.
`foundry.yaml` selects the adapter. App code binds to the facade.

```
facade: auth  (what your code calls)
    ├── cognito/   (AWS Cognito implementation)
    ├── auth0/     (Auth0 implementation)
    └── okta/      (Okta implementation)
```

Swapping providers is a one-line change in `foundry.yaml`.
Zero app code changes. Zero schema changes.

### P4 — Aspects for cross-layer extension

When a layer needs to add behavior to a type it does not own, it attaches an aspect.
The aspect declares its parent type. The schema merger composes them.
The owning layer's type definition is never modified.

```
meta-core defines:  User { id, name, email }
meta-auth attaches: UserAuthAspect { last_login, mfa_enabled }   extends: User
meta-billing attaches: UserBillingAspect { plan, stripe_id }      extends: User
→ merged User = { id, name, email, last_login, mfa_enabled, plan, stripe_id }
```

Each aspect evolves independently on its own migration path.
A breaking change to `UserAuthAspect` does not affect `UserBillingAspect`.

### P5 — Hooks for extensibility without ownership

The framework provides hooks at every significant event boundary.
Hooks let layers extend behavior without forking the code that owns it.

```
Schema hooks:  on_object_type_registered, on_aspect_merged
Action hooks:  before_action_execute, after_action_execute
Deploy hooks:  before_deploy, after_deploy
Task hooks:    tsk_clb_pre_*, tsk_clb_post_*
```

If you find yourself modifying a layer you do not own to add behavior,
the missing hook is a gap in the framework — raise it as a feature request.

### P6 — Lock files for reproducibility

`foundry.lock` pins every layer version and its content digest.
Two developers running `fab resolve` on the same `foundry.yaml`
get identical results, regardless of when they run it or what has been
published to the layer registry since.

`foundry.lock` is committed. It is updated by `fab resolve` only.
It is never hand-edited.

### P7 — Atomic commits: schema + gen + migrations together

A schema change, its generated artifacts, and its migration file are
committed in a single commit. They are never split across commits.
If the CI pipeline sees a schema change without a corresponding migration,
it fails. This is the Rails `db:migrate` contract applied to the full pipeline.

### P8 — The 15-minute company as a design constraint

Every architectural decision is evaluated against:
*does this make the 15-minute demo harder or easier?*

```bash
fab new acme-corp          # scaffold
vim foundry.yaml               # 5 minutes of config
fab bootstrap dev          # provision base infra
fab new app customer-api --binds "Customer,Order"
fab deploy customer-api --env dev
# working service, talking to ontology, 15 minutes
```

If a new feature adds a required step to this path, it needs strong justification.
Optional steps are fine. Mandatory steps raise the barrier to entry for every user.

### P9 — Scaffolds produce working starting points

`fab new` commands produce valid, runnable code — not empty stubs.
A scaffolded app compiles, starts, and responds to a request before
the developer writes a single line of custom code.
A scaffolded schema file is valid YAML that passes `fab schema validate`.
Scaffolds are templates that follow conventions, not placeholders to fill in.

### P10 — Explicit over implicit at layer boundaries

If a layer contributes schema to the ontology, that contribution is declared
in `layer.yaml` under `provides.schema`. It is not inferred from file presence.
If an app needs a queue, it declares `type: queue` in `app.yaml`. It is not
detected by scanning imports in the source code.
Explicitness makes the system inspectable, debuggable, and auditable.

---

## 3. Anti-Patterns

What to avoid, why it breaks, and the correct alternative.

### X1 — EAV (Entity-Attribute-Value) in the object store

```sql
-- Never do this
CREATE TABLE object_instances (
    id            UUID,
    object_type   TEXT,
    property_name TEXT,
    property_value JSONB
);
```

**Why it breaks:** Every object read is N rows and N JOINs. Query planner is blind —
no column statistics, no column indexes. Type enforcement moves to the application.
`WHERE tier = 'pro'` becomes a full table scan with a JSON filter.
Palantir built OSv2 specifically to escape this pattern.

**Correct alternative:** A1 + A3. One table per object type, managed by Atlas.
The ontology defines the shape. Migrations keep the table in sync.

---

### X2 — Dynamic query construction at request time

```python
# Never do this
columns = registry.resolve(ontology, tag).object_type("Customer").columns
query = f"SELECT {','.join(columns)} FROM customer WHERE tier = $1"
cursor.execute(query, [tier])
```

**Why it breaks:** Bypasses PostgreSQL's plan cache. Every request incurs
string building, parse, and plan overhead. `pg_stat_statements` becomes
unreadable. Prepared statement benefits are lost.

**Correct alternative:** A7. Generate static query modules at `fab schema publish`.
The OSDK ships pre-compiled queries. The hot path executes fixed SQL.

---

### X3 — Provider SDK imports in application code

```python
# Never do this
import boto3
sqs = boto3.client('sqs', region_name='us-east-1')
sqs.send_message(QueueUrl=os.environ['QUEUE_URL'], MessageBody=json.dumps(event))
```

**Why it breaks:** The app is now coupled to AWS SQS. Switching to RabbitMQ
requires finding and rewriting every callsite. Local dev requires AWS credentials
or a LocalStack mock. The facade contract is bypassed.

**Correct alternative:** A4 + P3. Use `app.queues.order_events.publish(event)`.
The OSDK handles the provider routing. The app is portable.

---

### X4 — God objects and mutable globals

WordPress's `$wpdb`, `$wp_query`, `$wp` — mutable globals that every plugin
reads and modifies. FAB's equivalent risk: a sprawling `foundry.yaml` that
every tool reads differently, or a shared runtime object that layers mutate.

**Why it breaks:** Unpredictable state, impossible to test in isolation,
debugging requires understanding the entire call graph.

**Correct alternative:** P1 + P10. `foundry.yaml` has a strict, validated schema.
Layers receive their configuration explicitly, not via global state.
The ontology registry is read-only at runtime — writes go through `schema_publish`.

---

### X5 — Convention drift

Teams start putting custom schema in `infra/` because it was convenient once.
Apps appear in `packages/` instead of `apps/`. Layer manifests are omitted
because "the schema file names are obvious enough".

**Why it breaks:** The convention is the documentation. Once drift starts,
a new team member cannot orient themselves. The tooling (which relies on
conventions to locate files) silently skips things or fails unexpectedly.

**Correct alternative:** P2. Enforce structure. `fab build` fails loudly
if `app.yaml` is missing or malformed. The linter checks directory conventions.
Conventions are not suggestions — they are contracts with the tooling.

---

### X6 — Implicit layer dependencies

```yaml
# Never do this — meta-auth extends User without declaring the dependency
# layer.yaml has no dependsOn entry for meta-core
spec:
  provides:
    schema:
      aspects: [UserAuthAspect]   # silently assumes meta-core/User exists
```

**Why it breaks:** Works until someone removes `meta-core` from `foundry.yaml`.
Then it fails at runtime, not at resolve time. The error message points to a
runtime schema resolution failure, not the missing dependency declaration.

**Correct alternative:** A6 + P10. Every cross-layer reference must be backed
by a `dependsOn` entry in `layer.yaml`. The resolver validates this before
anything is built.

---

### X7 — Upgrade hell from undeclared breaking changes

A layer publishes `meta-auth:2.0.0` that renames `UserAuthAspect.last_login`
to `UserAuthAspect.last_login_at` without declaring it as a breaking change.
Every app that uses the old property name silently breaks.

**Why it breaks:** Semantic versioning only works if breaking changes are
actually detected and flagged. Without automated breaking-change detection,
major version bumps become meaningless.

**Correct alternative:** `buf breaking` detects proto-level breaking changes.
Atlas safety analyzers detect SQL-level breaking changes. The schema merger
detects structural breaking changes at the ontology level.
Breaking changes require a major version bump and a migration path.

---

### X8 — The magic trap

Rails's `method_missing`, autoloading from file paths, callbacks that fire
from inherited class definitions, implicit `before_action` chains.
Every framework that relies on magic produces debugging nightmares.

**Why it breaks:** The mental model breaks down. Stack traces point to
framework internals. New contributors cannot understand behavior without
reading the framework source. The 15-minute onboarding becomes a 3-day investigation.

**Correct alternative:** P10. Explicit at layer boundaries. If an action fires
a side effect, it is declared in the `ActionType` spec. If a layer contributes
schema, it is listed in `layer.yaml`. Behavior is traceable to its declaration.

---

### X9 — Security as an afterthought

LAMP's `register_globals`, early WordPress's direct `$_GET` into SQL,
PHP's `magic_quotes` as a "solution" to injection. Every case where
security was added on top of an insecure default produced a decade of CVEs.

**Why it breaks:** Once an insecure pattern is established and widely used,
it is nearly impossible to remove without breaking the ecosystem.

**Correct alternative:** A8. The scaffold generates least-privilege IAM.
The default database connection uses a read-only role unless writes are
explicitly declared. The secrets adapter injects credentials at runtime —
there is no code path to hardcode a secret. Security is the only available default.

---

## 4. Equivalencies with Prior Art

How FAB concepts map to patterns that have proven themselves at scale.

| FAB concept | Equivalent | Project |
|------------|------------|---------|
| `foundry.yaml` | `settings.py` / `wp-config.php` / `.env` + `Procfile` | Django / WordPress / Heroku |
| `foundry.lock` | `Gemfile.lock` / `package-lock.json` / `go.sum` | Bundler / npm / Go modules |
| `meta-*` layers | Gems / Django apps / WordPress plugins | Rails / Django / WordPress |
| `layer.yaml` manifest | `gemspec` / `composer.json` / `package.json` | Ruby / PHP / Node |
| `fab new` | `rails generate` / `php artisan make:` / `django-admin startapp` | Rails / Laravel / Django |
| `fab schema migrate` | `rails db:migrate` / `manage.py migrate` | Rails / Django |
| Aspect model | `INSTALLED_APPS` model + signals | Django |
| Hooks system | `add_action` / `add_filter` | WordPress |
| Facade/adapter | Laravel Facades / Dapr building blocks | Laravel / Dapr |
| OSDK | ActiveRecord / Eloquent ORM | Rails / Laravel |
| Ontology registry | `$wpdb` schema (but not a god object) | WordPress |
| Multi-ontology versioning | WordPress Multisite (designed in, not retrofitted) | WordPress |
| `app.yaml` resource binding | Score workload spec / `Procfile` | Humanitec / Heroku |
| Pre-compiled query modules | Prepared statements / query objects | Universal |
| Layer topological sort | Yocto `LAYERDEPENDS` / BitBake dependency resolver | Yocto |

---

## 5. First-Class Citizens

These are not optional layers. They are not features to add later.
Every component of FAB — layers, apps, the runtime, the compiler, the CLI —
is built with these three capabilities present from day one.
An implementation that defers any of these is incomplete.

---

### FC1 — Observability (OpenTelemetry)

Every FAB component emits traces, metrics, and logs in OpenTelemetry format.
This is not the app developer's responsibility — it is the framework's.

**What is always instrumented automatically:**
- Every ontology operation (object read, action execute, link traversal) is a span
- Every OSDK call carries trace context
- Every schema publish, migrate, and deploy operation is traced
- Trace context propagates through action side effects and queue messages —
  a trace started by an HTTP request survives into the SQS message it produces
  and the consumer that processes it

**The adapter model applies here too:**

```yaml
# foundry.yaml
adapters:
  observability: grafana     # or: datadog | honeycomb | jaeger | cloudwatch | otlp
```

The OTEL collector is part of `meta-elo`. The backend is a pluggable adapter.
Swapping Datadog for Grafana is a one-line change — no instrumentation code changes.

**The invariant:**
An app that does not emit OTEL traces is not a valid FAB app.
`fab build` configures the OTEL SDK automatically. The developer does not
opt in — they opt out with deliberate override if they have a specific reason.

**What the developer gets for free:**
- Latency histograms for every ontology query
- Error rates per action type
- Slow query detection across ObjectSet operations
- Distributed traces across services that share trace context via queue messages

---

### FC2 — Security (Secrets and Secret Store)

The secrets adapter is mandatory. There is no environment — including local dev —
where credentials reach a running component through any path other than the
secrets adapter.

**The secrets adapter is not optional:**

```yaml
# foundry.yaml
adapters:
  secrets: ssm          # or: vault | gcp-secrets | 1password | local-vault
```

`local-vault` is the dev adapter — a local HashiCorp Vault or equivalent.
Dev does not use `.env` files with real credentials. It uses the secrets adapter
pointed at a local store. This ensures the secret injection path is identical
across every environment.

**What the secrets layer enforces:**
- No credential literal survives in source code — the compiler detects and rejects them
- Secret rotation is supported without container restarts (the OSDK re-fetches on TTL)
- Every secret access is logged via the observability layer (FC1)
- The ontology's two-layer permission model (schema-level + row-level) is enforced
  by the runtime — it cannot be bypassed by going directly to the object store

**mTLS between services:**
Services generated by FAB authenticate to each other via mTLS.
The certificates are managed by the secrets layer. The developer does not
configure this — it is generated by `fab build` and `fab deploy`.

**Zero-trust default:**
No service trusts another by network position alone.
Every inter-service call carries an identity. The auth adapter validates it.
"Works inside the VPC" is not a security model.

---

### FC3 — DevOps Tools as a Layer

The task and deployment tooling is not a monolith bolted onto the side of FAB.
It is itself a layer: `meta-devops`. It follows the same composition rules
as every other layer — versioned, pluggable, and extensible.

**The plugin mechanism is the justfile + layer.yaml registration:**

Every `meta-*` layer that has a `justfile` and declares `provides.tasks`
contributes its recipes to the merged `gen/justfile` automatically when active.

```yaml
# layers/meta-auth/layer.yaml
spec:
  provides:
    schema:
      objects: [Session, Permission]
    tasks:
      justfile: justfile
      namespace: auth
    adapters:
      - facade: auth
        implementations: [cognito, auth0, okta]
```

**What this means:**
- Add `meta-billing` to `foundry.yaml` → `fab task run billing::generate-invoices`,
  `fab task run billing::reconcile` appear automatically
- Remove a layer → its task namespace disappears from `fab task list`
- The devops surface of the platform grows with the platform itself

**`meta-devops` owns the FAB CLI core and the task runner facade:**

```
layers/meta-devops/
├── layer.yaml
├── justfile              # core dev tasks: onboard, sync, doctor
└── packages/
    └── python/
        ├── adapter_just/     # default task runner
        └── adapter_invoke/   # Python-heavy projects
```

**The versioning implication:**
`meta-devops` has its own version in `foundry.lock`. A security patch to the
deploy tooling ships as `meta-devops:1.0.1` — teams pull the update via
`fab resolve` without touching their schema, their apps, or their layers.
The CLI and the platform evolve independently.

---

### FC4 — Agentic Workflows (AI-first collaboration)

FAB is designed for human-agent collaboration from the ground up.
Agents are not an afterthought — they are a primary user of every interface FAB exposes.

**MCP server ships with `meta-elo`:**

The ontology, layer graph, assembly topology, and build state are queryable
via the Model Context Protocol. Any agent working on a FAB project can ask:
- "What object types does `meta-marine` contribute?"
- "What layers depend on `meta-core`?"
- "Which assemblies contain `AISService`?"
- "What would break if I rename `Vessel.mmsi`?"

```yaml
# foundry.yaml
adapters:
  ai: anthropic    # or: openai | bedrock | local
mcp:
  enabled: true
  port: 7777
```

**Structured CLI output — all commands support `--json`:**

```bash
fab build core-services --json
# { "status": "error", "errors": [{ "code": "MISSING_LAYER", "layer": "meta-events",
#   "required_by": "customer-portal", "resource_type": "queue", "fix": "fab layer add meta-events" }] }
```

Agents parse errors and act on them directly. No screen-scraping of human-readable output.

**Agent-driven scaffolding:**

`fab new` uses the configured AI adapter to generate idiomatic, context-aware starting points —
not static templates. The agent has access to the active layer graph, existing schema,
and the project's conventions before it generates anything.

```bash
fab new layer meta-marine --describe "AIS vessel tracking with MarineTraffic and VesselTracker providers"
# Agent reads: active layers, existing schema types, foundry.yaml conventions
# Generates:   layer.yaml, schema/objects/vessel.yaml, idl/proto/marine/v1/,
#              packages/python/ais_service/, justfile, CLAUDE.md
```

**`CLAUDE.md` scaffolded at every boundary:**

Every `fab new` command generates a `CLAUDE.md` alongside the code.
It contains the component's contract, design decisions, extension points,
and what an agent should know before modifying it.

```
layers/meta-marine/CLAUDE.md        ← layer contract + extension guide
apps/customer-portal/CLAUDE.md      ← app bindings + resource declarations
assemblies/core-services/CLAUDE.md  ← what's in this assembly and why
```

**`meta-ai` layer — AI as a pipeline source and function:**

```yaml
# pipelines/enrich_vessel_risk.yaml
spec:
  trigger:
    on_event:
      - object_type: Vessel
        event: updated
  source:
    adapter: ai
    model: claude-opus-4-6
    prompt: |
      Given vessel {{vessel.name}} with route {{vessel.current_route}},
      assess the risk score (0-100) and provide a brief reason.
  target:
    objectType: Vessel
    mapping:
      risk_score:  source.risk_score
      risk_reason: source.risk_reason
  mode: upsert
  editPolicy: function_wins
```

AI enrichment follows the same pipeline model as any other source —
observable, retryable, auditable, governed by edit policy.

---

## 6. The Test

When evaluating any design decision, run it through these four questions:

1. **Does it preserve the axioms?** If it violates an axiom, it is wrong.
2. **Does it make the 15-minute demo harder?** If yes, it needs strong justification.
3. **Does it require magic?** If the behavior cannot be traced to a declaration, redesign it.
4. **What is the escape hatch?** If there is none, the abstraction will be forked around.
5. **Is the technology choice reversible?** If swapping it requires rewriting app code, the abstraction is wrong. If it requires one line in `foundry.yaml`, the design is correct.
6. **Can an agent navigate this from declarations alone?** If understanding the behavior requires reading implementation code, add a declaration. The YAML contracts are the shared language between humans and agents.
