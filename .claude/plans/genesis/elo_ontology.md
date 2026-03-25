# ELO Ontology

> *ELO — Entity Layer Ontology.* A git-first, schema-driven semantic layer for FAB.
> Think Palantir Ontology, but open-source, forkable, and version-controlled.

---

## 1. What It Is

The ELO Ontology is a **runtime semantic layer** that sits above raw data.
It turns entity definitions into a live, queryable, writable representation of your
business domain — with typed relationships, named business operations, access control,
and auto-generated clients.

The progression from classical ER to full ontology:

```
ER model:     Entity, Attribute, Relationship
↓
ELO Phase 1:  ObjectType, Property, LinkType             (schema + migrations)
↓
ELO Phase 2:  + Interface, Aspect                        (polymorphism + composition)
↓
ELO Phase 3:  + ActionType                               (named business operations)
↓
ELO Phase 4:  + ObjectSet query engine + OSDK            (runtime + generated clients)
↓
ELO Phase 5:  + Access control + Derived properties      (schema-declared policies + Functions)
```

---

## 2. Core Concepts

### Object Type
A business entity. Maps to a DB table in the object store. Has properties, implements
interfaces, and can have aspects attached by other layers.

### Link Type
A first-class typed relationship between two object types. Not a foreign key — a
named, directional, cardinality-constrained edge that is queryable and traversable.

### Interface
An abstract type that cannot be instantiated. Defines a shared set of properties and
link constraints. Object types implement interfaces. Enables polymorphism — a workflow
can operate on `Facility` regardless of whether it is `Airport` or `Warehouse`.

### Aspect
An independently evolvable facet attached to an object type defined in another layer.
Layer `meta-auth` can add auth-specific fields to `User` (defined in `meta-core`)
without owning or modifying `User`. Each aspect evolves on its own migration path.

### Action Type
A named business operation — not a CRUD endpoint. Encodes intent:
`ApprovePurchaseOrder`, `EscalateSupportTicket`, `UpgradeTier`.
Has preconditions (evaluated before commit), writes (which properties change),
and side effects (webhooks, audit log entries, notifications).

### ObjectSet
The query unit. A typed, filtered, permission-scoped cursor over object instances.
Supports filter, sort, aggregate, and link traversal.

```python
# Filter
high_risk = await osdk.vessel.filter(
    risk_score__gte=80,
    flag_state__in=["IR", "KP", "RU"],
)

# Sort + limit
recent = await osdk.voyage.filter(status="active") \
    .order_by("-started_at") \
    .limit(50)

# Link traversal — follow VesselVoyages link
voyages = await osdk.vessel.get(vessel_id) \
    .traverse("vessel_voyages") \
    .filter(status="completed")

# Aggregate
stats = await osdk.invoice.filter(customer_id=cid) \
    .aggregate(total=Sum("amount"), count=Count())

# Search-around (graph hop) — all vessels that called at a sanctioned port
at_risk = await osdk.port.filter(sanctions_status="sanctioned") \
    .traverse("vessel_last_port", direction="reverse") \
    .filter(status="active")
```

ObjectSet queries are pre-compiled at `fab schema publish` into version-pinned
query modules — see §10 (Performance Model). At runtime the OSDK executes static SQL;
no query construction in the hot path.

---

## 3. Why YAML, Not Proto

Proto is optimised for **wire format and service contracts** — how services
communicate over the network. The ontology schema needs to express
**business domain semantics**, which proto cannot do cleanly:

| Requirement | Proto capability |
|-------------|-----------------|
| Bidirectional link types | No — unclear which message owns the relationship |
| Interface polymorphism | No — `oneof` is not inheritance |
| Action preconditions | No — options become unreadable for expressions |
| Action side effects | No |
| SQL constraints (PK, unique, index) | Awkward via custom options |
| Cardinality semantics | No |

**The correct relationship:** YAML is the source of truth. Proto is a generated
artifact, consumed by buf for multi-language client generation.

```
schema/*.yaml  (you write this)
      │
   compiler
      │
  ┌───┴──────────────────────────┐
  ▼                              ▼
gen/proto/*.proto            gen/sql/*.hcl
  │                              │
  buf generate               atlas migrate diff
  │                              │
  ▼                              ▼
Python / TS / Java stubs    migrations/NNN.sql
```

Same relationship as Prisma schema → SQL DDL. The schema is authoritative;
the generated artifacts follow from it.

---

## 4. Schema Layer Layout

```
schema/
├── objects/                # Object type definitions (you edit these)
│   ├── customer.yaml
│   └── order.yaml
├── links/                  # Link type definitions
│   └── customer_orders.yaml
├── interfaces/             # Abstract types
│   └── auditable.yaml
├── aspects/                # Cross-layer extensions
│   └── user_auth.yaml      # meta-auth extending User from meta-core
├── actions/                # Business operations
│   └── approve_order.yaml
│
├── migrations/             # Generated by Atlas — never hand-edited
│   ├── meta-core/
│   │   ├── 0001_initial.sql
│   │   └── 0002_add_team_description.sql
│   ├── meta-auth/
│   │   └── 0001_initial.sql
│   └── app/                # Your company's layer
│       └── 0001_initial.sql
│
└── gen/                    # All generated artifacts — never hand-edited
    ├── proto/
    ├── python/
    └── typescript/
```

### Object Type — example

```yaml
apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Customer
  layer: app
spec:
  primaryKey: id
  implements:
    - Auditable        # from meta-core
    - Ownable          # from meta-core
  properties:
    - name: id
      type: string
    - name: email
      type: string
      unique: true
      indexed: true
    - name: tier
      type: enum
      values: [free, pro, enterprise]
  actions:
    - upgrade_tier     # defined in schema/actions/upgrade_tier.yaml
```

### Link Type — example

```yaml
apiVersion: fab/v1
kind: LinkType
metadata:
  name: CustomerOrders
spec:
  source:
    layer: app
    type: Customer
  target:
    layer: app
    type: Order
  cardinality: one_to_many
  reverseName: customer
```

### Action Type — example

```yaml
apiVersion: fab/v1
kind: ActionType
metadata:
  name: upgrade_tier
spec:
  target: Customer
  inputs:
    - name: tier
      type: string
  preconditions:
    - "current.tier != input.tier"
  writes:
    - tier
  sideEffects:
    - webhook: billing.on_tier_change
    - audit: true
```

### Aspect — example (cross-layer extension)

```yaml
apiVersion: fab/v1
kind: Aspect
metadata:
  name: UserAuthAspect
  layer: meta-auth
spec:
  extends:
    layer: meta-core
    type: User
  properties:
    - name: last_login
      type: timestamp
    - name: mfa_enabled
      type: boolean
    - name: oauth_providers
      type: array
      items: string
```

---

## 5. Layer Composition

Layers compose by convention. Each `meta-*` layer owns its types. Other layers
extend via interfaces, aspects, and cross-layer link types.

```
meta-core        defines:  User, Organization, Team, Role
                 defines interfaces: Auditable, Ownable, Taggable

meta-auth        adds:     Session, Permission, AuthProvider
                 extends:  User via UserAuthAspect
                 implements: Auditable on Session

meta-billing     adds:     Plan, Subscription, Invoice
                 links:    Organization → Subscription
                 implements: Auditable, Ownable on Invoice

your-app         adds:     Customer, Order, Product
                 implements: Auditable, Ownable on Customer
                 extends:  User via CompanyUserAspect
```

**The immutable contract rule:** a layer can add types, implement interfaces, attach
aspects, and create links to foreign types. It cannot modify a type it does not own.

Selected in `foundry.yaml`:

```yaml
layers:
  - meta-core        # always included
  - meta-auth
  - meta-billing
  - meta-comms
```

The merged ontology at runtime is the union of all active layers.

---

## 6. Two Runtime Stores

```
┌──────────────────────────────────────────────┐
│  Ontology Registry  (metadata plane)         │
│  "What types exist and how they relate"      │
│  PostgreSQL: ontologies, ont_object_types,   │
│  ont_link_types, ont_aspects, ...            │
│  Versioned immutable snapshots               │
└──────────────────┬───────────────────────────┘
                   │  defines schema for
                   ▼
┌──────────────────────────────────────────────┐
│  Object Store  (data plane)                  │
│  "The actual instances of those types"       │
│  PostgreSQL: customer, order, ... tables     │
│  Managed by Atlas migrations                 │
└──────────────────────────────────────────────┘
```

The registry and object store are both PostgreSQL, can share an instance,
or be separated per environment.

---

## 7. Registry DB Schema

```sql
-- Named, versioned ontology snapshots
CREATE TABLE ontologies (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,         -- "acme-corp"
    version      TEXT NOT NULL,         -- "1.3.0"
    git_ref      TEXT,                  -- commit SHA this was built from
    status       TEXT NOT NULL          -- draft | published | deprecated
        CHECK (status IN ('draft', 'published', 'deprecated')),
    layers       TEXT[] NOT NULL,       -- ["meta-core", "meta-auth", "app"]
    created_at   TIMESTAMPTZ DEFAULT now(),
    published_at TIMESTAMPTZ,
    UNIQUE (name, version)
);

-- Environment tags — one ontology version per tag
CREATE TABLE ontology_tags (
    name         TEXT NOT NULL,         -- "acme-corp"
    tag          TEXT NOT NULL,         -- "prod" | "staging" | "dev"
    ontology_id  UUID NOT NULL REFERENCES ontologies(id),
    updated_at   TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (name, tag)
);

-- All schema entities scoped to an ontology version
CREATE TABLE ont_object_types (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ontology_id  UUID NOT NULL REFERENCES ontologies(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    layer        TEXT NOT NULL,
    spec         JSONB NOT NULL,
    UNIQUE (ontology_id, name)
);

CREATE TABLE ont_link_types (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ontology_id  UUID NOT NULL REFERENCES ontologies(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    source_type  TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    cardinality  TEXT NOT NULL,
    spec         JSONB NOT NULL,
    UNIQUE (ontology_id, name)
);

CREATE TABLE ont_interfaces (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ontology_id  UUID NOT NULL REFERENCES ontologies(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    spec         JSONB NOT NULL,
    UNIQUE (ontology_id, name)
);

CREATE TABLE ont_aspects (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ontology_id  UUID NOT NULL REFERENCES ontologies(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    extends_type TEXT NOT NULL,
    layer        TEXT NOT NULL,
    spec         JSONB NOT NULL,
    UNIQUE (ontology_id, name)
);

CREATE TABLE ont_action_types (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ontology_id  UUID NOT NULL REFERENCES ontologies(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    spec         JSONB NOT NULL,
    UNIQUE (ontology_id, name)
);
```

---

## 8. Versioning and Multi-Ontology

### Git-first, registry-served

```
git tag v1.3.0
    │
    fab schema publish --version 1.3.0
    │
    ▼
registry: acme-corp:1.3.0  (immutable snapshot)
    │
    fab schema tag staging 1.3.0
    │
    ▼
tag "staging" → acme-corp:1.3.0
```

Multiple versions live simultaneously:

```
acme-corp:1.2.0  ←── prod     (live traffic)
acme-corp:1.3.0  ←── staging  (validation)
acme-corp:1.3.1  ←── dev      (in-progress)
```

Each version is a **complete, self-contained snapshot** — no diffs, no parent
references. Simpler to query, safe to rollback.

### Promotion workflow

```bash
fab schema publish --version 1.3.0   # create snapshot, status=draft
fab schema tag staging 1.3.0          # point staging at this version
fab schema promote staging prod        # atomic swap: prod now points to 1.3.0
fab schema rollback prod               # revert prod to previous version
fab schema list
# acme-corp:1.2.0  prod      published  git:179a9f6
# acme-corp:1.3.0  staging   published  git:5d0ea62
# acme-corp:1.3.1  dev       draft      git:e83e7b4
```

### Runtime resolution

```python
# By tag — follows promotions automatically
ontology = registry.resolve("acme-corp", tag="prod")

# By version — pinned, for testing
ontology = registry.resolve("acme-corp", version="1.3.0")
```

---

## 9. Schema Migration Layout

Two distinct concerns:

- **Schema diff** — structural changes to type definitions
- **Data migration** — transforming existing instances to fit a new schema

### Breaking change classification

| Change | Class | Required action |
|--------|-------|-----------------|
| Add property | Non-breaking | Generate + migrate |
| Remove property (no data) | Non-breaking | Generate + migrate |
| Remove property (has data) | Breaking | `DROP` or `MOVE` migration |
| Change property type | Breaking | `CAST` migration |
| Rename property | Breaking | New prop + `MOVE` + deprecate old |
| Add link type | Non-breaking | Generate + migrate |
| Add interface implementation | Non-breaking | Generate only |
| Change primary key | Breaking | Full re-index required |

### Workflow

```bash
# 1. Edit schema/objects/customer.yaml
fab schema diff              # classify: breaking | non-breaking | data-migration-required

# 2. Generate all artifacts
fab schema generate          # emits .proto, Pydantic, TS types, Atlas HCL

# 3. Generate SQL migration from diff
fab schema migrate diff      # Atlas detects delta → writes migrations/app/0002_*.sql

# 4. Review, then apply
fab schema migrate apply

# 5. Commit atomically: schema + gen/ + migrations/ in one commit
```

**Rule:** `gen/` and `migrations/` are never hand-edited. If a data transform
cannot be generated, add `migrations/app/0002_manual.sql` marked `# manual` —
the tooling will skip regenerating it.

---

## 10. Performance Model

### The core tension

Traditional schema: query plan knows the schema at compile time.
Ontology approach: schema is resolved at runtime.

Every performance problem flows from that gap.

### The EAV trap — never do this

```sql
-- This looks flexible. It is a disaster.
CREATE TABLE object_instances (
    id            UUID,
    object_type   TEXT,
    property_name TEXT,
    property_value JSONB
);
```

Every object read becomes N JOINs. Query planner is blind. Indexes are useless for
column-level filters. Type enforcement moves to the application.

**Rule: the object store is always Atlas-managed tables, one per object type.**
The ontology tells you what the table should look like. Atlas makes it so.

### Known performance problems and mitigations

| Problem | Risk | Mitigation |
|---------|------|-----------|
| Dynamic query construction | High — bypasses plan cache | Pre-compile version-pinned query modules at `schema_publish` |
| Multi-version table drift | Medium — wide tables, NULL accumulation | Column projection at ORM layer; limit concurrently active versions |
| Multi-hop link traversal | Medium — JOIN chaining degrades past ~3 hops | Apache AGE as async read replica for graph queries; no dual synchronous write |
| Runtime resolution overhead | Low with caching | `LISTEN/NOTIFY` cache invalidation on `ontology_tags`; in-process LRU per tag |
| Object set fan-out at scale | High — Palantir warns at 100k objects | Materialized views per common query pattern; push-down filters before joins |
| Write amplification (relational + graph) | Medium | AGE populated asynchronously, not in the write transaction |

### The rule that prevents most of these

**Generate static, version-pinned query modules at `schema_publish`.
Never construct queries dynamically at request time.**

```
fab schema publish --version 1.3.0
    │
    ├── writes ontology snapshot to registry DB
    ├── generates SQL migrations (Atlas)
    ├── generates .proto (buf)
    └── generates query module for v1.3.0
            customer_queries_v1_3_0.py
            → find_by_id: SELECT id, email, tier, preferences ...
            → find_by_tier: SELECT ... WHERE tier = $1
            (compiled, fixed, plan-cacheable)
```

The OSDK for v1.3.0 ships with pre-compiled queries. The runtime resolves the tag
to a version, loads that version's query module, executes static SQL.
PostgreSQL caches the plan. No string building in the hot path.

---

## 11. Full Pipeline

```
schema/*.yaml  (source of truth — you write this, or a UI writes this)
      │
   compiler  (fab schema generate)
      │
  ┌───┼────────────────────────────────┐
  ▼   ▼                                ▼
.proto           Atlas HCL           query modules
  │                  │                    │
  buf generate   atlas migrate        compiled SQL
  │                  │
  ▼                  ▼
Python stubs    migrations/NNN.sql
TS types
Java clients
      │
      └─── fab schema publish ──► Ontology Registry DB
                                  ontologies table
                                  ont_object_types
                                  ont_link_types ...
                                        │
                                  registry.resolve(name, tag)
                                        │
                                        ▼
                                  Runtime clients (OSDK)
                                  pre-compiled query modules
                                        │
                                        ▼
                                  Object Store DB
                                  customer, order, ... (Atlas-managed)
```

---

## 12. Agent Access via MCP

The ontology is queryable by agents at any time through the MCP server shipped with `meta-elo`.
An agent working on a FAB project can orient itself from ontology state without reading source code.

```
# What types exist?
mcp: ontology.types()                     → all object types across active layers

# Full schema for a specific type
mcp: ontology.type("Vessel")              → properties, links, aspects, actions, interfaces

# Which layers contribute to a type?
mcp: ontology.providers("Vessel")         → ["meta-marine"] — layer that owns it + aspect contributors

# Impact analysis before a breaking change
mcp: ontology.breaking_changes("mmsi → vessel_id")
→ { affected_apps: [...], affected_pipelines: [...], migration_required: true }
```

This makes the ontology the agent's primary navigation surface — not source code.
An agent that reads `ontology.type("Customer")` knows the full schema, its links,
which layer owns it, and which aspects extend it. No file reading required.

---

## 13. Prior Art

| Project | What it does | What it misses for ELO |
|---------|-------------|----------------------|
| **Palantir Ontology** | The reference model | Proprietary, not forkable |
| **Dapr** | Runtime facade/adapter — closest conceptual ancestor for provider swapping | No business domain types; no bootstrap scope |
| **DataHub (LinkedIn)** | Aspect-composition model, PDL annotations, entity registry | Data catalog, not operational ontology; no action types |
| **LinkML** | YAML schema → multi-target codegen (proto, SQL, Python, TS) | No migration engine; no runtime; no action types |
| **Atlas** | SQL schema-as-code, versioned migrations, safety analyzers | DB migration leg only |
| **Buf / BSR** | Proto-first multi-language generation | Tooling only; no domain semantics |
| **Apache AGE** | Graph queries over PostgreSQL (Cypher) | Backing store candidate; not a schema layer |

ELO sits between the platform-engineering tools (runtime orchestrators for ops)
and the SaaS boilerplates (web-app starters). That gap is empty in OSS today.

---

## 13. Functions vs. Actions (from Palantir)

Palantir makes a hard distinction that FAB must also make:

| | Action | Function |
|-|--------|----------|
| Initiated by | Human or AI agent | Code (event, schedule, pipeline) |
| Transaction | Single, atomic | Multi-step, can be long-running |
| Preconditions | Yes — enforced before commit | No |
| Audit trail | Always, captures human intent | Optional |
| Side effects | Declared in schema | Arbitrary |
| Typical use | "Approve this order" | "Recalculate all risk scores nightly" |

Actions are the human/agent write interface to the ontology.
Functions are the computation layer that keeps derived state current.

```yaml
# schema/functions/recalculate_risk.yaml
apiVersion: fab/v1
kind: Function
metadata:
  name: recalculate_risk
spec:
  trigger:
    - on_object_updated: [Customer, Order]   # event-driven
    - on_schedule: "0 2 * * *"              # or nightly
  reads:  [Customer, Order, Payment]
  writes: [Customer.risk_score]
  runtime: python
  timeout: 300s
```

Functions are declared in schema, versioned with the ontology, and
executed by the function runtime (a separate concern from the action engine).
They are not ad-hoc scripts — they are first-class schema citizens.

---

## 14. Object Edit History (from Palantir)

When a user modifies an object via an action, that edit is stored separately
from the source data. When a pipeline re-syncs from the source (e.g., nightly
CRM sync), the user's correction survives — it is not overwritten.

```
customer row in object store:
  id:    abc123              ← pipeline (source of truth for this property)
  email: corrected@corp.com  ← user edit via action (wins over pipeline value)
  tier:  pro                 ← pipeline
```

The edit layer sits above the pipeline layer. Conflict resolution is per-property:

```yaml
# schema/objects/customer.yaml
spec:
  properties:
    - name: email
      type: string
      editPolicy: user_wins     # user edit survives pipeline re-sync
    - name: tier
      type: enum
      editPolicy: pipeline_wins # pipeline always authoritative for this property
    - name: notes
      type: string
      editPolicy: user_only     # pipeline never writes this — user-managed only
```

Without edit history, every pipeline re-run undoes human corrections.
Teams stop trusting the ontology as an operational layer and treat it
as read-only. The edit layer is what makes the ontology a **digital twin**,
not just a data view.

The DB representation:

```sql
-- Object store (pipeline-managed)
CREATE TABLE customer (
    id    TEXT PRIMARY KEY,
    email TEXT,
    tier  TEXT
);

-- Edit store (action-managed, survives pipeline re-sync)
CREATE TABLE customer_edits (
    object_id   TEXT REFERENCES customer(id),
    property    TEXT,
    value       JSONB,
    edited_by   TEXT,
    edited_at   TIMESTAMPTZ,
    PRIMARY KEY (object_id, property)
);
```

The OSDK merges the two at read time, applying `editPolicy` per property.

**What writes to `customer_edits`:**
- Actions (user-initiated) — always write to the edit store
- Functions with `editPolicy: function_wins` — write to edit store, not the base table
- Direct OSDK writes from app code — treated as user edits, written to edit store
- Pipelines — always write to the base table, never to edit store

**OSDK edit history access:**
```python
# Read current merged state (normal)
customer = await osdk.customer.get(customer_id)

# Read edit history for a specific property
history = await osdk.customer.edit_history(customer_id, property="email")
# → [EditRecord(value="old@corp.com", edited_by="user:alice", edited_at=...),
#    EditRecord(value="corrected@corp.com", edited_by="user:bob", edited_at=...)]

# Revert a user edit (restores pipeline value)
await osdk.customer.revert_edit(customer_id, property="email")
```

**Edit history and schema migrations:**
When a property is renamed (breaking migration), the edit history for the old
property name is migrated to the new name by the generated migration script.
`fab schema migrate diff` detects property renames and generates the edit store migration.

**Retention:** Edit history is retained indefinitely by default. Configurable via
`schema/objects/customer.yaml`:
```yaml
spec:
  edit_history:
    retain: 365d    # keep 1 year of edit history; older entries are archived
```

---

## 15. Transaction Boundaries

**Actions are the atomic unit.** One action = one database transaction.

```
Action: PlaceOrder(customer_id, items)
├── Preconditions evaluated (read-only, no writes)
├── pre-action hooks fire (read-only, can abort)
├── BEGIN TRANSACTION
│   ├── Action writes: create Order, update Customer.last_order_at
│   ├── post-action hooks fire (read/write, same transaction)
│   └── COMMIT
└── on-object-updated hooks fire asynchronously (after commit)
```

**What can happen within one action transaction:**
- Writes to multiple object types: yes — atomic across all
- Calling Functions: no — Functions are async, triggered after commit
- Publishing events: deferred — events are published after commit, not during
- Calling gRPC services: strongly discouraged — network calls inside a transaction risk timeouts and partial commits

**Cross-layer writes within one action:** allowed if both object types are visible
to the action's OSDK scope. The single database transaction covers both writes.

**Function writes are NOT transactional with their trigger:**
```
User calls UpgradeTier(customer_id)          ← transaction 1
    → Customer.tier = "pro" commits
    → recalculate_risk Function triggers      ← transaction 2 (async, separate)
    → Customer.risk_score = 72 commits
```

There is a brief window where `Customer.tier = pro` but `Customer.risk_score` is
stale. This is acceptable — Functions are for derived state, not constraints.
If strong consistency is required between two values, both must be written by the same Action.

---

## 16. Cascade Delete Semantics

Cascade behavior is declared in the link type, not the object type:

```yaml
# schema/links/customer_orders.yaml
apiVersion: fab/v1
kind: LinkType
metadata:
  name: CustomerOrders
spec:
  source: { layer: your-app, type: Customer }
  target: { layer: your-app, type: Order }
  cardinality: one_to_many
  on_source_delete: restrict    # cannot delete Customer if Orders exist
```

| `on_source_delete` value | Behavior |
|--------------------------|----------|
| `restrict` | Deletion fails if linked objects exist (default) |
| `cascade` | Delete all linked target objects |
| `set_null` | Set the foreign key to null on linked objects |
| `detach` | Remove the link but leave target objects intact |

**Cascade and edit history:** when `cascade` deletes a target object, its edit
history rows are also deleted. Set `on_source_delete: cascade` only when the
target has no independent meaning outside the source relationship.

**Cascade and pipelines:** if a pipeline populates the target objects, `cascade`
delete will conflict with the next pipeline run re-creating them. Prefer `restrict`
or `detach` when target objects are pipeline-managed.

`fab schema validate` checks for dangerous cascade patterns:
- `cascade` on a link where the target has `user_wins` properties (user edits will be lost)
- `cascade` chains longer than 2 hops (risk of accidental mass deletion)

---

## 17. Access Control (Phase 5)

Access control is part of the ontology schema — not a separate system layered on top.
Policies are declared alongside type definitions, compiled into the ontology snapshot,
and enforced by the OSDK at the query boundary. Apps never write their own permission
checks; the ontology schema IS the authorization surface.

### Three levels of control

```
Type-level    →  who can see this object type at all
Row-level     →  which instances of that type
Property-level →  which properties on those instances
```

### Schema declarations

```yaml
# schema/objects/vessel.yaml
spec:
  properties:
    - name: name
      type: string
    - name: imo_number
      type: string
    - name: internal_risk_notes   # only fleet managers see this
      type: string
      visibility: restricted      # → requires explicit grant in access policy

  access:
    default: deny                 # deny unless explicitly granted
    policies:
      - ref: schema/policies/vessel_access.yaml
```

```yaml
# schema/policies/vessel_access.yaml
apiVersion: fab/v1
kind: AccessPolicy
metadata:
  name: vessel_access
  layer: meta-marine

spec:
  object_type: Vessel

  rules:
    # Fleet managers: full access to their own fleet only
    - role: fleet_manager
      operations: [read, write]
      row_filter: "object.fleet_id = actor.fleet_id"

    # Analysts: read all vessels, but not restricted properties
    - role: analyst
      operations: [read]
      exclude_properties: [internal_risk_notes]

    # Compliance: read all vessels including restricted, no write
    - role: compliance_officer
      operations: [read]
      # no property exclusions — sees everything

  action_rules:
    # Only fleet managers can reroute their own vessels
    - action: RerouteVessel
      role: fleet_manager
      condition: "object.fleet_id = actor.fleet_id"
```

### Role declaration

Roles are declared in `meta-auth` schema. Every user has a set of roles that the
OSDK reads at query time:

```yaml
# layers/meta-auth/schema/objects/user_role.yaml
kind: ObjectType
spec:
  properties:
    - name: user_id
      type: string
    - name: role
      type: enum
      values: [fleet_manager, analyst, compliance_officer, admin]
    - name: scope_fleet_id      # optional scope — fleet_manager sees only this fleet
      type: string
      nullable: true
```

### How the OSDK enforces access

The OSDK generated for `v1.3.0` includes compiled access checks:

```python
# Generated — not hand-written
async def get_vessel(self, vessel_id: str) -> Vessel:
    actor_roles = self._actor.roles
    row_filter = self._access_policy.row_filter_for("Vessel", actor_roles)
    excluded = self._access_policy.excluded_properties("Vessel", actor_roles)

    row = await self._db.query_one(
        vessel_queries_v1_3_0.find_by_id,
        vessel_id,
        row_filter=row_filter,        # compiled SQL WHERE clause
    )
    if row is None:
        raise NotFound("Vessel", vessel_id)   # same error whether denied or missing
    return Vessel.from_row(row, exclude=excluded)
```

**Not found = access denied.** The OSDK never reveals whether an object exists but
is denied — it returns the same `NotFound` error. This prevents enumeration attacks.

### Multi-tenant isolation

For SaaS deployments where tenants must be strictly isolated, declare an
`isolation_key` on the object type:

```yaml
# schema/objects/customer.yaml
spec:
  isolation:
    key: organization_id    # every query implicitly adds WHERE organization_id = actor.org_id
    enforce: strict         # build error if any query omits this filter
```

`strict` isolation means the OSDK will not compile a query that could cross tenant
boundaries. It is a build-time guarantee, not a runtime check.

### Access policies version with the ontology

Policies are part of the ontology snapshot. `fab schema publish --version 1.3.0`
compiles the type definitions AND the access policies into the same snapshot.
The OSDK for `v1.3.0` enforces `v1.3.0` policies — even if policies are updated in
`v1.4.0`, the `v1.3.0` OSDK continues to enforce the older rules until apps upgrade.

This means policy rollouts are controlled by ontology promotion:
```bash
fab schema tag staging 1.4.0   # staging now enforces new policies
fab schema promote staging prod # atomic: prod enforces new policies from this moment
fab schema rollback prod        # revert: prod back to old policies instantly
```

### Audit trail

Every write through the OSDK is automatically recorded:

```sql
CREATE TABLE ont_audit_log (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type  TEXT NOT NULL,
    object_id    TEXT NOT NULL,
    operation    TEXT NOT NULL,    -- read | write | action | delete
    actor_id     TEXT NOT NULL,
    actor_roles  TEXT[] NOT NULL,
    property     TEXT,             -- null for whole-object operations
    old_value    JSONB,
    new_value    JSONB,
    action_name  TEXT,             -- set for action operations
    timestamp    TIMESTAMPTZ DEFAULT now()
);
```

Accessible via OSDK:
```python
log = await osdk.audit.for_object("Vessel", vessel_id)
# → [AuditEntry(operation="write", property="risk_score", actor="user:alice", ...)]
```

### Design rules

1. **Access is declared in schema** — no permission checks in app or service code
2. **OSDK is the enforcement point** — bypassing it bypasses access control
3. **Not found = access denied** — never distinguish the two in error responses
4. **Strict isolation for SaaS** — `isolation_key` is a build-time guarantee
5. **Policies version with the ontology** — rollout and rollback via `fab schema promote/rollback`
6. **Audit is automatic** — every OSDK write is logged; apps do not write audit entries manually

---

## 18. Storage Backend Abstraction (from Palantir)

Palantir migrated from OSv1 (Phonograph) to OSv2 — a completely different
storage backend — without changing any application code or ontology definitions.
Applications never knew the migration happened. OSv1 deprecation is June 2026,
six years after OSv2 launched. The abstraction made a six-year parallel run possible.

The lesson: **the object store must be behind an abstraction from day one**,
even if there is only one implementation initially.

```
app code
   ↓
OSDK (generated, version-pinned)
   ↓
ObjectSet query engine  ← the abstraction boundary
   ↓
   ├── postgres-atlas     current default
   ├── postgres-age       graph traversal variant
   └── <future>           swap without touching apps or schema
```

If FAB apps ever query PostgreSQL directly — bypassing the query engine —
the storage backend can never change. The abstraction is what makes
future evolution possible without a migration crisis.
