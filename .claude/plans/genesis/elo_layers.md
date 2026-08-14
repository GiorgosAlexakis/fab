# ELO Layer Architecture

> How FAB composes domain capabilities into a deployable company stack.
> Inspired by Yocto's meta-layer model — pick what you need, own what you change.

---

## 1. Mental Model

FAB layers are composable domain modules. Each layer owns a vertical slice:
schema definitions, infrastructure, service implementations, and adapters.

```
foundry.yaml declares active layers
        │
        fab resolve  →  foundry.lock  (pinned, reproducible)
        │
        topological build order
        │
        ├── meta-elo       (foundation — ontology runtime)
        ├── meta-core      (base entities + interfaces)
        ├── meta-auth      (auth domain, depends on meta-core)
        ├── meta-billing   (billing domain, depends on meta-core)
        └── your-app       (custom schema + apps, depends on selected layers)
```

The merged ontology at runtime is the union of all active layer schemas.
Apps bind against the merged ontology via the OSDK — they never import layer internals directly.

---

## 2. Layer Hierarchy

```
meta-elo                    (foundation — implicit dependency for all layers)
    │
    ├── meta-core           (no domain deps — base types and interfaces)
    │       │
    │       ├── meta-auth           adapters: cognito | auth0 | okta
    │       ├── meta-billing        adapters: stripe | recurly
    │       ├── meta-comms          adapters: ses | sendgrid | mailgun
    │       ├── meta-data           adapters: rds | aurora | supabase
    │       ├── meta-events         adapters: sns-sqs | eventbridge | pubsub
    │       ├── meta-observability  adapters: datadog | grafana | cloudwatch
    │       ├── meta-storage        adapters: s3 | gcs | azure-blob
    │       └── meta-ai             adapters: bedrock | openai | anthropic  ← opt-in
    │
    └── your-app            (depends on selected meta-* layers)
```

`meta-elo` is the only mandatory layer. Every `meta-*` layer (including `meta-ai`) is
opt-in — explicitly declared in `foundry.yaml`. `meta-ai` is not assumed present unless
the project declares it.

`meta-elo` is the only mandatory layer. All others are opt-in.

---

## 3. What a Layer Contains

```
layers/meta-auth/
├── layer.yaml                          # manifest: deps, provides, adapter facades
│
├── schema/                             # Ontology YAML (language-agnostic)
│   ├── objects/                        # Session, Permission, AuthProvider
│   ├── aspects/                        # UserAuthAspect (extends meta-core/User)
│   ├── interfaces/                     # AuthenticatedIdentity
│   └── actions/                        # Login, Logout, GrantPermission
│
├── idl/                                # Proto contracts (language-agnostic)
│   └── proto/auth/v1/
│       ├── auth_service.proto
│       └── events.proto
│
├── justfile                            # Task recipes (language-agnostic shell, optional)
│
├── packages/                           # All code, organized by language
│   └── python/
│       ├── auth_service/               # Service implementation
│       │   └── pyproject.toml
│       ├── adapter_cognito/            # Provider adapter packages
│       │   └── pyproject.toml
│       ├── adapter_auth0/
│       │   └── pyproject.toml
│       └── adapter_okta/
│           └── pyproject.toml
│
└── infra/                              # Terraform modules for this layer
```

**Schema-only layers** (e.g. `meta-core`) have no `packages/` or `idl/` directory —
just `schema/` and `layer.yaml`. The structure is additive: only include what the layer provides.

Every layer is a self-contained vertical slice. Layers communicate only through
the ontology schema — never by importing each other's service code.

---

## 4. `layer.yaml` Manifest

```yaml
apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-auth
  version: 1.2.0
spec:
  dependsOn:
    - name: meta-elo
      version: ">=1.0.0"
    - name: meta-core
      version: ">=1.0.0"

  provides:
    schema:
      objects:    [Session, Permission, AuthProvider]
      aspects:    [UserAuthAspect]        # extends: meta-core/User
      interfaces: [AuthenticatedIdentity]
      actions:    [Login, Logout, GrantPermission]
    adapters:
      - facade: auth
        implementations: [cognito, auth0, okta]
```

---

## 5. Extension Mechanisms

Layers extend each other through three mechanisms, in order of invasiveness:

### A — Implement an interface (non-invasive)

```yaml
# meta-billing/schema/objects/invoice.yaml
spec:
  implements:
    - layer: meta-core
      interface: Auditable    # inherits created_at, created_by
    - layer: meta-core
      interface: Ownable
```

### B — Attach an aspect (extend without owning)

```yaml
# meta-auth/schema/aspects/user_auth.yaml
kind: Aspect
spec:
  extends:
    layer: meta-core
    type: User
  properties:
    - { name: last_login,       type: timestamp }
    - { name: mfa_enabled,      type: boolean }
    - { name: oauth_providers,  type: array, items: string }
```

`meta-auth` adds fields to `User` without owning `User`.
Each aspect evolves independently on its own migration path.

### C — Cross-layer link types

```yaml
# meta-billing/schema/links/org_subscription.yaml
kind: LinkType
spec:
  source: { layer: meta-core,    type: Organization }
  target: { layer: meta-billing, type: Subscription }
  cardinality: one_to_one
```

**The immutable contract rule:**
A layer may add types, implement interfaces, attach aspects, and link to foreign types.
It may not modify a type it does not own.

---

## 6. Adapters as Facades

Every layer that integrates an external service exposes a **facade** — a
provider-agnostic interface. The active implementation is selected in `foundry.yaml`.

```
facade: auth  (interface contract — what your code binds against)
    ├── cognito/    implements auth facade via AWS Cognito
    ├── auth0/      implements auth facade via Auth0
    └── okta/       implements auth facade via Okta
```

```yaml
# foundry.yaml adapter selection
adapters:
  auth:    cognito     # → layers/meta-auth/adapters/cognito/
  billing: stripe
  email:   ses
```

Swapping `cognito` → `auth0` is a one-line change in `foundry.yaml`.
No application code changes. No schema changes.

---

## 7. Schema Merging

At `fab schema publish`, layer schemas are processed in topological order and merged:

```
meta-core:   User { id, name, email }
                ↓
meta-auth:   + UserAuthAspect { last_login, mfa_enabled }
                ↓
meta-billing: + UserBillingAspect { stripe_customer_id, plan }
                ↓
merged User = { id, name, email, last_login, mfa_enabled, stripe_customer_id, plan }
```

Cross-layer references are validated at merge time — not at runtime.
A broken reference (`extends: meta-core/User` when `meta-core` is not active)
is a publish-time error, not a runtime failure.

---

## 8. Dependency Resolution

```yaml
# foundry.yaml
layers:
  - name: meta-core
    version: ">=1.0.0"
  - name: meta-auth
    version: ">=1.2.0"
  - name: meta-billing
    version: ">=1.0.0"
```

Resolution steps:

1. Load `layer.yaml` for each declared layer
2. Discover transitive dependencies
3. Build DAG, topological sort
4. Validate: no missing deps, no circular deps
5. Validate: all cross-layer schema references resolve
6. Write `foundry.lock` — pinned versions, reproducible across environments

```yaml
# foundry.lock (generated, committed, never hand-edited)
locked:
  - name: meta-elo
    version: 1.0.3
    digest: sha256:abc...
  - name: meta-core
    version: 1.0.1
    digest: sha256:def...
  - name: meta-auth
    version: 1.2.0
    digest: sha256:ghi...
  - name: meta-billing
    version: 1.0.0
    digest: sha256:jkl...
```

---

## 9. The FDE Interface

An FDE interacts with layers through `foundry.yaml` and `fab` commands.
Layer internals — Terraform, proto, service code — are never touched directly.

```bash
# Add a layer
fab layer add meta-comms        # updates foundry.yaml + resolves lock

# Switch an adapter
fab adapter set auth auth0      # updates foundry.yaml adapter selection

# Inspect active layers
fab layers
# meta-core      1.0.1   active
# meta-auth      1.2.0   active   adapter: cognito
# meta-billing   1.0.0   active   adapter: stripe

# Validate the layer graph
fab resolve --check             # dry-run: validate deps + references
```

---

## 10. Repository Structure

```
foundry/
├── foundry.yaml                    # layer selection + adapter config (FDE edits this)
├── foundry.lock                    # resolved + pinned (generated, committed)
│
├── layers/                         # FAB-provided domain modules
│   ├── meta-elo/                   # foundation: ontology runtime
│   ├── meta-core/                  # base types: User, Org, Team, Role
│   ├── meta-auth/                  # auth domain
│   ├── meta-billing/               # billing domain
│   ├── meta-comms/                 # communications domain
│   └── meta-*/                     # additional opt-in layers
│
├── schema/                         # company-specific schema (FDE writes here)
│   ├── objects/
│   ├── links/
│   ├── aspects/
│   └── actions/
│
├── apps/                           # deployable services (FDE writes here)
│   └── <name>/
│       ├── app.yaml                # ontology binding + runtime declaration
│       └── src/                    # business logic only
│
├── infra/                          # generated + tuned Terraform (rarely touched)
└── tools/                          # FAB internals (never touched)
```

**The FDE's world is three directories: `foundry.yaml` + `schema/` + `apps/`.**
Everything else is platform.

---

## 11. `meta-elo` — The Foundation Layer

`meta-elo` is the only mandatory layer. It provides the ontology runtime that all
other layers depend on.

```
meta-elo provides:
  runtime:
    - Ontology registry service     (versioned type snapshots)
    - Object store                  (Atlas-managed PostgreSQL tables)
    - ObjectSet query engine       (filter, sort, aggregate, traverse)
    - Action execution engine       (preconditions, writes, side effects, audit)
    - OSDK generator                (type-safe clients from ontology snapshot)

  build-time:
    - Schema compiler               (YAML → proto + SQL + TS + Python)
    - Schema merger                 (aggregates contributions from all layers)
    - Cross-layer reference validator
    - Breaking change detector
```

All other layers contribute schema definitions to `meta-elo`'s registry.
The registry is the merged result.

---

## 12. BSP Layers — Cloud Targets (from Yocto)

Yocto separates **distro** (what kind of system) from **BSP** (Board Support Package —
what hardware target). The BSP provides all the hardware-specific glue without touching
application layers.

FAB applies the same split:

```
Distro  =  company profile  (what kind of company — SaaS, marketplace, internal tool)
BSP     =  cloud target     (which cloud and how it is wired)
```

```
layers/
├── bsp-aws/        # VPC layout, IAM conventions, EKS, ECR, SQS, ElastiCache
├── bsp-gcp/        # VPC, GKE, Artifact Registry, Pub/Sub, Cloud SQL
├── bsp-azure/      # AKS, ACR, Azure networking, Service Bus
└── bsp-local/      # Docker Compose, LocalStack, minikube — local dev
```

```yaml
# foundry.yaml
bsp: aws            # all generated Terraform and K8s targets this BSP

mcp:
  enabled: true     # expose project state to agents via Model Context Protocol
  port: 7777        # default
```

Moving a customer from AWS to GCP changes one line in `foundry.yaml`.
The `meta-auth`, `meta-billing`, schema, and app layers are untouched.
The BSP is what changes, nothing else.

---

## 13. Shared State Cache — sstate (from Yocto)

Yocto's most impactful DX innovation. Every build task is hashed by its inputs.
If nothing changed, the cached output is reused — no recomputation.

FAB equivalent: if `meta-auth:1.2.0` schema hasn't changed since last publish,
skip regenerating its proto stubs, Pydantic models, and Atlas HCL.

```
fab schema publish --version 1.3.0
  → meta-core:1.0.1  content-hash:abc → cache HIT  (skip)
  → meta-auth:1.2.0  content-hash:def → cache HIT  (skip)
  → app:local        content-hash:xyz → cache MISS  (schema changed, regenerate)
  → regenerates app layer only
  → total time: 3s instead of 45s
```

Cache key: `layer_name + layer_version + schema_content_hash`.
Cache location: `.fab/sstate/` — local to the repo, `.gitignore`d.
In CI, the sstate directory is restored from cache between runs.

---

## 14. Layer Compatibility Ranges (from Yocto)

Yocto layers declare `LAYERSERIES_COMPAT` — which Yocto release series they
support. Without an upper bound, a breaking release silently breaks dependent layers.

`layer.yaml` enforces this with version range upper bounds:

```yaml
spec:
  dependsOn:
    - name: meta-elo
      version: ">=1.0.0, <2.0.0"    # tested against meta-elo 1.x only
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
```

The upper bound is the critical addition. Without it, a breaking `meta-elo:2.0.0`
silently breaks `meta-auth:1.2.0`. The resolver enforces compatibility windows
before anything is built or deployed.

Layer maintainers update upper bounds when they test against a new major release.
`fab resolve` warns when a layer's compatibility range does not cover
the currently resolved version of its dependency.

---

## 15. Upstream vs. Downstream Layers (from Yocto)

Yocto distinguishes upstream layers (community-maintained, pulled from OpenEmbedded)
from your own layers (company-specific). Upstream layers are never forked.

`layer.yaml` makes this explicit:

```yaml
metadata:
  name: meta-auth
  origin: upstream      # FAB-provided, pulled from FAB layer registry
  # vs.
  origin: local         # company-owned, lives in this repo
```

Behavior difference:
- `fab resolve --upgrade` updates `upstream` layers to latest compatible version
- `local` layers are never auto-upgraded — they are owned by the company
- Upstream layers must not be forked; extend via aspects, interfaces, and hooks instead
- If an upstream layer does not support a needed extension, open an issue or
  add a hook — do not fork
