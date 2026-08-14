# ELO OSDK

> The OSDK (Ontology Software Development Kit) is the only way apps interact with the ontology.
> Apps never query the database directly. They import the OSDK and call typed methods.
> The OSDK is generated from the ontology snapshot — it changes when the ontology changes.

---

## 1. What the OSDK Is

The OSDK is a generated, versioned, type-safe client library that exposes the ontology to app code. Every object type, link, action, and query defined in the ontology has a corresponding generated class or function in the OSDK.

```
app.py
  import from: customer_osdk  ← generated package, installed via uv
        │
        OSDK runtime → resolves queries → PostgreSQL / object store
```

Apps import the OSDK. They never import SQLAlchemy, psycopg, or any database client. The OSDK is the only database.

**What the OSDK provides:**

- Typed ObjectSet query builders — `osdk.customer.filter(tier="gold").sort_by("created_at")`
- Action invocations — `await osdk.actions.upgrade_customer(customer_id="c1")`
- Link traversal — `await osdk.order.get(id).via("placed_by")`
- Pre-compiled query modules — `osdk.queries.churn_risk_customers(threshold=0.8)`
- Edit history access — `await osdk.customer.history(id).since("2025-01-01")`

---

## 2. Generation Pipeline

```
foundry.yaml (layer selection)
        │
        fab schema compile
        │
        Ontology snapshot (JSON, versioned)
        │
        fab osdk generate --lang python --app my-app
        │
        Generated OSDK package
        │
        apps/my-app/pyproject.toml references it as a workspace dependency
```

### Step 1 — Ontology snapshot

`fab schema compile` reads all schema YAML from active layers, resolves types, validates constraints (no cycles, cascade rules, access policies), and produces a versioned JSON snapshot:

```json
{
  "version": "1.3.0",
  "sha": "a8f3c21...",
  "object_types": [...],
  "link_types": [...],
  "action_types": [...],
  "access_policies": [...]
}
```

The snapshot is the source of truth. OSDK generation always starts from a snapshot — never directly from YAML.

### Step 2 — OSDK generation

```bash
fab osdk generate \
  --snapshot .fab/snapshots/1.3.0.json \
  --app apps/my-app \
  --lang python

# Output:
#   apps/my-app/osdk/
#     pyproject.toml       (package: my-app-osdk, version: 0.1.3+ontology.1.3.0)
#     src/
#       my_app_osdk/
#         __init__.py
#         objects/
#           customer.py    (Customer ObjectSet class)
#           order.py
#         actions/
#           upgrade_customer.py
#         queries/
#           __init__.py    (pre-compiled query modules)
#         _runtime.py      (connection + auth plumbing)
```

The generated package name embeds the app name to prevent cross-app imports.

### Step 3 — Install

The generated package is a workspace member. Apps declare it as a dependency:

```toml
# apps/my-app/pyproject.toml
[project]
dependencies = [
    "my-app-osdk",   # workspace package, generated
]
```

`uv sync` installs it. The package is regenerated whenever the ontology snapshot changes.

---

## 3. Token-Scoped Packages

Apps declare what they bind to in `app.yaml`. The generator uses those binds to scope what the OSDK exposes — the app cannot see types it hasn't declared a need for.

```yaml
# apps/my-app/app.yaml
spec:
  binds:
    - object_type: Customer
      properties: [id, name, email, tier, risk_score]
      actions: [UpgradeCustomer, DowngradeCustomer]
    - object_type: Order
      properties: [id, customer_id, amount, status]
      # no actions declared — app can read but not write orders
    - queries:
      - churn_risk_customers
      - high_value_segment
```

The generated OSDK for this app exposes:
- `Customer` with only the 5 declared properties
- `Order` with only the 4 declared properties — no action methods
- The 2 declared query modules
- Nothing else

An attempt to access `osdk.invoice` (not declared in binds) raises `AttributeError` at import time — the class does not exist in the generated package.

**Why token scoping matters:**

- Security — the app cannot read `Customer.password_hash` unless it declares it
- Clarity — the OSDK surfaces exactly what the app uses; nothing more
- Audit — access policy checks are applied at the OSDK layer, and the binds constrain what those checks need to evaluate

Access policies from `elo_ontology.md §17` are enforced at OSDK method call time using the runtime token. The generated code includes policy evaluation hooks; it does not bypass them.

---

## 4. Pre-Compiled Query Modules

Complex queries — multi-join traversals, aggregations, ML-model scoring — are declared in schema and pre-compiled into typed Python modules. The app calls a function; the OSDK runs the query.

```yaml
# layers/meta-core/schema/queries/churn_risk_customers.yaml
apiVersion: fab/v1
kind: Query
metadata:
  name: churn_risk_customers
  layer: meta-core

spec:
  description: "Customers whose 90-day order value has dropped more than 30%"

  parameters:
    - name: threshold
      type: float
      default: 0.3
    - name: lookback_days
      type: int
      default: 90

  returns:
    object_type: Customer
    properties: [id, name, tier, risk_score]

  implementation:
    sql: |
      SELECT c.id, c.name, c.tier, c.risk_score
      FROM customers c
      JOIN (
        SELECT customer_id,
               SUM(CASE WHEN created_at > NOW() - INTERVAL '{{lookback_days}} days'
                        THEN amount ELSE 0 END) AS recent_value,
               SUM(CASE WHEN created_at > NOW() - INTERVAL '{{lookback_days*2}} days'
                        AND created_at <= NOW() - INTERVAL '{{lookback_days}} days'
                        THEN amount ELSE 0 END) AS prior_value
        FROM orders
        GROUP BY customer_id
      ) v ON c.id = v.customer_id
      WHERE prior_value > 0
        AND (prior_value - recent_value) / prior_value > {{threshold}}
      ORDER BY c.risk_score DESC
```

The generated module:

```python
# apps/my-app/osdk/src/my_app_osdk/queries/churn_risk_customers.py
# AUTO-GENERATED — do not edit. Regenerate with: fab osdk generate

from dataclasses import dataclass
from typing import AsyncIterator
from .._runtime import OSDKRuntime

@dataclass
class ChurnRiskCustomer:
    id: str
    name: str
    tier: str
    risk_score: float

async def churn_risk_customers(
    runtime: OSDKRuntime,
    *,
    threshold: float = 0.3,
    lookback_days: int = 90,
) -> AsyncIterator[ChurnRiskCustomer]:
    """Customers whose 90-day order value has dropped more than 30%"""
    async for row in runtime.execute_query(
        "churn_risk_customers_v1_3_0",   # compiled query key — versioned
        threshold=threshold,
        lookback_days=lookback_days,
    ):
        yield ChurnRiskCustomer(**row)
```

App usage:

```python
from my_app_osdk.queries import churn_risk_customers

async for customer in churn_risk_customers(osdk.runtime, threshold=0.25):
    await notify_csm(customer.id, customer.risk_score)
```

The compiled query key `churn_risk_customers_v1_3_0` is the ontology-version-pinned identifier. The runtime resolves it to the actual SQL stored in the database.

---

## 5. Runtime Query Resolution

Pre-compiled query modules do not embed SQL in the generated Python. Instead they embed a versioned key. The runtime resolves the key to SQL at startup using a tag/version cache.

```
app starts
    │
    OSDK runtime connects to DB
    │
    SELECT compiled_queries WHERE app = 'my-app' AND ontology_version = '1.3.0'
    │
    loads → { "churn_risk_customers_v1_3_0": "<sql>", ... }
    │
    caches in memory
    │
    app calls churn_risk_customers() → runtime finds cached SQL → executes
```

**Cache invalidation via LISTEN/NOTIFY:**

When a new ontology version is deployed and compiled queries are updated in the database, the runtime receives a PostgreSQL `NOTIFY` on the `fab_query_cache` channel. It reloads the cache without restarting.

```python
# _runtime.py (generated plumbing — simplified)
async def _listen_for_cache_invalidation(self):
    async for notification in self._db.listen("fab_query_cache"):
        if notification.payload == self._app_version:
            await self._reload_query_cache()
```

This means a rolling ontology upgrade does not require app restarts. Running app instances reload their query cache when the new version lands.

---

## 6. Multi-Language Support

The generator produces idiomatic packages per language. Same ontology snapshot, different output.

| Language | Package type | Install mechanism | ObjectSet style |
|----------|-------------|-------------------|-----------------|
| Python | `uv` workspace package | `uv sync` | `async for c in osdk.customer.filter(tier="gold")` |
| TypeScript | `npm` workspace package | `npm install` | `for await (const c of osdk.customer.filter({ tier: "gold" }))` |
| Go | Go module in `apps/` | `go mod tidy` | `osdk.Customer().Filter(customer.Tier.Eq("gold")).All(ctx)` |
| Java | Maven/Gradle module | standard build | `osdk.customer().filter(Customer.tier().eq("gold")).stream()` |
| Rust | Cargo workspace crate | `cargo build` | `osdk.customer().filter(Customer::tier.eq("gold")).all().await` |

All languages generate:
- Typed ObjectSet query builders matching the declared binds
- Action invocation methods with typed parameters
- Pre-compiled query functions with typed return types
- No SQL, no raw HTTP, no schema YAML — only generated types

Language is declared in `app.yaml`:

```yaml
# apps/my-app/app.yaml
spec:
  language: python   # or: typescript, go, java, rust
```

`fab osdk generate` reads this and selects the correct code generator.

---

## 7. Versioning

### App pinning

An app's OSDK is pinned to the ontology version it was generated against. The version is embedded in the package metadata:

```toml
# apps/my-app/osdk/pyproject.toml (generated)
[project]
name = "my-app-osdk"
version = "0.1.3+ontology.1.3.0"   # semver + ontology version label
```

If the ontology is upgraded to `1.4.0`, the app continues running against `1.3.0` until the OSDK is regenerated and the app is redeployed. Both versions are live simultaneously during rolling upgrades.

### Ontology version deprecation

When an ontology version is deprecated (e.g., 1.1.0 → end-of-life after 90 days):

1. `fab osdk deprecate 1.1.0 --in 90d` — marks the version for deprecation
2. Running apps on 1.1.0 OSDK receive a deprecation warning header on all API responses
3. `fab osdk list` shows which apps are on deprecated versions
4. At end-of-life, `fab osdk retire 1.1.0` drops the compiled queries from the database — apps on that version stop working

```bash
fab osdk list
# ONTOLOGY VERSION  APPS                  STATUS
# 1.3.0             my-app, admin-panel   current
# 1.2.0             legacy-importer       current (upgrade recommended)
# 1.1.0             batch-reports         DEPRECATED — retires 2026-06-01
```

---

## 8. Multi-Ontology

An app can bind against at most **one** named ontology. FAB does not support cross-ontology joins in a single OSDK instance.

If a use case genuinely requires data from two separate ontologies, the correct model is:
- A pipeline that syncs relevant objects from ontology B into ontology A (as read-only shadow types)
- Two separate app processes, each with its own OSDK, communicating via the event bus

```yaml
# NOT SUPPORTED:
spec:
  binds:
    - ontology: acme-core
      object_type: Customer
    - ontology: acme-billing   # ← error at fab schema compile
      object_type: Invoice
```

The reason: cross-ontology joins would require the OSDK runtime to coordinate transactions across two databases, which breaks the atomicity guarantees that Actions rely on.

---

## 9. Regenerating the OSDK

```bash
# Regenerate OSDK for a single app after ontology changes
fab osdk generate --app apps/my-app

# Regenerate for all apps in the foundry
fab osdk generate --all

# Check which apps need regeneration (their pinned snapshot is behind)
fab osdk status
# my-app      pinned: 1.2.0   current: 1.3.0   STATUS: stale
# admin-panel pinned: 1.3.0   current: 1.3.0   STATUS: up to date

# CI check — fails if any app OSDK is stale
fab osdk status --assert-current
```

`fab schema compile && fab osdk generate --all` should run in CI on every schema change. Generated OSDK files are committed to the repo — they are not gitignored.

---

## 10. Design Rules

1. **Apps import the OSDK, never the database** — no SQLAlchemy, no psycopg, no raw SQL in app code
2. **OSDK is generated from the snapshot** — not from YAML directly; the snapshot is the intermediate representation
3. **Token scoping is enforced at generation** — the generated package does not contain types the app hasn't declared binds for
4. **Generated files are committed** — OSDK packages are in the repo; `uv sync` installs them as workspace members
5. **Pre-compiled queries are versioned keys** — SQL lives in the database, not in generated Python; the key pins the ontology version
6. **LISTEN/NOTIFY for live cache reload** — running apps don't restart on ontology upgrades
7. **One ontology per app** — cross-ontology binds are a compile error
8. **`fab osdk generate` is idempotent** — running it twice with the same snapshot produces identical output
9. **Deprecation has a grace period** — `fab osdk deprecate` → warning period → `fab osdk retire` → hard stop
