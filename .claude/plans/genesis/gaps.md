# FAB Documentation Gaps

> Open documentation debts — concepts referenced across the plan files that need their own spec.
> Ordered by implementation priority: gaps at the top block code, gaps at the bottom block polish.
> ✅ = resolved

---

## Missing Documents

### ✅ Consumption model — `elo_init.md`
Written. Covers `fab init`, upstream-vs-local layer distinction, bundle model, never-fork rule, extending upstream layers, contributing back, Phase 1/2 transition, and the one-repo-two-artifacts structure.

### ✅ Functions — `elo_functions.md`
Written. Covers trigger types, schema YAML, implementation contract, AI functions, deployment as workers, async execution model, chaining, cycle detection, testing harness, and design rules.

### ✅ Adapter Implementation Contract — `elo_adapters.md`
Written. Covers facade types, adapter package structure, Protocol interface, entry point registration, config injection, mock adapter, new adapter walkthrough, unit + integration testing.

### ✅ Hooks System — `elo_hooks.md`
Written. Covers four hook types, schema YAML, pre/post-action execution in transaction, async hooks as observers, ordering across layers, failure semantics, `HookAbort`, and testing harness.

---

### ✅ Access Control — `elo_ontology.md §17`
Added as Phase 5 of the ontology (not a separate doc — access is a schema concern).
Covers: type/row/property-level rules, AccessPolicy YAML, role declarations in meta-auth, OSDK enforcement (not-found = denied), multi-tenant isolation_key, policy versioning with ontology snapshots, automatic audit log.

---

### ✅ OSDK Generation — `elo_osdk.md`

Written. Covers code generation pipeline (snapshot → per-language package), token-scoped packages from `app.yaml` binds, pre-compiled query modules with versioned keys, LISTEN/NOTIFY runtime cache invalidation, ontology version deprecation lifecycle, and the single-ontology-per-app constraint.

---

### 3. Adapter Implementation Contract — `elo_adapters.md`

**Referenced in:** `elo_layers.md §6`, `elo_philosophy.md P3`, `elo_apps.md §4`

The facade concept is clear, but the contract an adapter must fulfil is never written down.

Needs to cover:
- Directory structure: where adapter code lives within a layer (`packages/python/adapter_<name>/`)
- The facade interface: what methods/signatures an adapter must implement for each facade type (`auth`, `queue`, `storage`, `ai`, etc.)
- Registration: how the runtime discovers which adapter is active (entry points? config injection?)
- Configuration: how `foundry.yaml` adapter config is passed into the adapter at runtime
- Adapter versioning and compatibility with the facade version
- Testing: how to write and run adapter tests; mock vs. real-provider test modes
- Writing a new adapter: step-by-step guide (implement interface → register in layer.yaml → add to foundry.yaml)

---

### 4. Access Control — `elo_security.md`

**Referenced in:** `elo_philosophy.md FC2`, `elo_ontology.md §5` (Phase 5)

Phase 5 of the ontology is "Access control + Derived properties" — never specified.

Needs to cover:
- Permission model: RBAC? ABAC? Resource-based? Policy language
- Schema-level declarations: where access rules are defined (schema YAML? separate policy files?)
- Row-level security: per-object instance restrictions
- Property-level restrictions: some properties visible only to certain roles
- Action precondition enforcement: who can execute which ActionTypes
- Audit trail: automatic capture of who read/wrote which objects and when
- Cross-tenant isolation: org-scoped object stores for SaaS deployments
- How access policies version with the ontology (v1.2.0 rules applied to v1.3.0 objects)
- Token scoping: relationship between `app.yaml` binds and runtime access checks

---

### 5. Hooks System — `elo_hooks.md`

**Referenced in:** `elo_philosophy.md P5`

Four hook types are named (`pre-action`, `post-action`, `on-object-updated`, `on-event`) but never specified.

Needs to cover:
- Hook YAML declaration: where hooks are defined (schema? layer.yaml? separate hooks/ dir?)
- Hook types and when they fire
- Hook context: what data is available (object state, actor identity, trigger event)
- Cancellation: can `pre-action` hooks abort an action?
- Ordering: if multiple layers register the same hook, what determines order?
- Failure semantics: does a failing hook roll back the triggering operation?
- Relationship to Functions: are hooks implemented as Functions, or a separate primitive?
- Testing: how to test hook interactions across layers

---

## Gaps Within Existing Documents

### ✅ ObjectSet Query Language — `elo_ontology.md §2`
Added filter, sort, limit, link traversal, aggregate, and search-around examples directly to the ObjectSet core concept definition.

### ✅ Transaction Boundaries — `elo_ontology.md §15`
Added new section: Actions are the atomic unit, one action = one transaction, cross-object writes within an action are atomic, Functions are async (separate transaction), events are deferred post-commit.

### ✅ Object Edit History — `elo_ontology.md §14`
Added runtime contract: what writes to the edit store, OSDK history API, edit revert, schema migration handling, and retention configuration.

### ✅ Cascade Delete Semantics — `elo_ontology.md §16`
Added new section: `on_source_delete` declared on link types (restrict/cascade/set_null/detach), interaction with edit history and pipelines, fab schema validate warnings.

### ✅ Spelling — behaviour → behavior
Fixed 7 instances across `elo_philosophy.md` and `elo_layers.md`.

### ✅ ObjectSet capitalization
Standardized to `ObjectSet` across all files.

---

### ✅ Testing Strategy — `elo_testing.md`

Written. Covers the five-layer testing stack (schema validation, unit, adapter integration, schema compat, assembly integration), harnesses for functions/hooks/adapters/services, schema breaking-change CI gate, assembly test isolation via per-run DB schemas, fixture vs. seed data distinction, ontology upgrade validation workflow, and CI pipeline ordering.

---

### `fab` CLI Package Structure (open)

`elo_init.md §14` shows the CLI repo structure but the CLI's own internal design is unspecified:
- Plugin architecture: how do layers extend the CLI (`fab pipeline run` requires `meta-events`)?
- How does `fab` locate `foundry.yaml` (nearest ancestor walk? `FAB_ROOT` env var?)?
- Version compatibility contract between CLI version and layer bundle version

---

## Spelling & Consistency

### "behaviour" → "behavior"

7 remaining instances of British spelling:
- `elo_philosophy.md`: 6 instances
- `elo_layers.md`: 1 instance (§15 "Behaviour difference")

### "Object Set" capitalization

Used as "Object Set", "ObjectSet", and "object set" inconsistently across files.
Pick one and apply it everywhere. Suggestion: **ObjectSet** (matches Palantir's convention).
