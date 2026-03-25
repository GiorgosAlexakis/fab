# ELO Tasks

> How operational commands are defined, discovered, and run across layers.
> Tasks are shell recipes. Each layer contributes a justfile. FAB merges them.
> For complex logic, a justfile recipe calls into a proper package — but that's opt-in.

---

## 1. Mental Model

```
each layer contributes             fab sync merges              FDE runs
        │                                │                          │
layers/meta-marine/              gen/justfile                fab task
  justfile                       (generated mod imports)     marine::sync-vessels
```

Tasks are **justfile recipes** — not Python packages, not bash scripts buried in `tools/`.
`just` is language-agnostic: a recipe can call a Python module, a Go binary, a shell
one-liner, or a `fab pipeline run` command. The runner is irrelevant to the recipe author.

---

## 2. Layer justfile

Each layer that provides tasks has a `justfile` at its root:

```
layers/meta-marine/
├── layer.yaml
├── schema/
├── idl/
├── justfile                ← task definitions (sits alongside schema/, idl/)
├── packages/
│   └── python/
│       ├── ais_service/
│       ├── adapter_marinetraffic/
│       └── adapter_vesseltracker/
├── pipelines/
└── infra/
```

```just
# layers/meta-marine/justfile

# Sync vessel positions from the active AIS adapter
sync-vessels limit="":
    uv run fab pipeline run sync_vessel_positions {{ if limit != "" { "--limit " + limit } else { "" } }}

# Begin tracking a specific vessel
track-vessel mmsi:
    uv run python -m meta_marine.cli track --mmsi {{mmsi}}

# Replay historical voyage data
replay-voyage voyage_id from="":
    uv run python -m meta_marine.cli replay --voyage-id {{voyage_id}} \
        {{ if from != "" { "--from " + from } else { "" } }}

# Seed test data (dev only)
seed-test-data:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Seeding marine test data..."
    uv run python -m meta_marine.dev.seed
```

Recipes are plain shell by default. `#!/usr/bin/env bash` makes them multi-line scripts.
No FAB-specific API. No decorator magic. Just `just`.

---

## 3. Generated Merged justfile

`fab sync` generates `gen/justfile` using `just`'s `mod` keyword.
Each active layer with a `justfile` becomes a named module:

```just
# gen/justfile  — GENERATED, never hand-edited

mod marine  '../layers/meta-marine'
mod billing '../layers/meta-billing'
mod auth    '../layers/meta-auth'
mod comms   '../layers/meta-comms'
```

The root `justfile` (at the repo root, FDE-owned) imports from `gen/justfile`:

```just
# justfile  (repo root — FDE-owned, committed)
import 'gen/justfile'

# FDE can add project-level recipes here
bootstrap: (marine::sync-vessels) (billing::seed-test-data)
    @echo "Bootstrap complete"
```

---

## 4. CLI Interface

```bash
# List all tasks across all layers
fab task list
# or: fab task list --json   (structured output for agents)
# or directly: just --list

# Available recipes:
#   marine::sync-vessels   limit=""  Sync vessel positions from the active AIS adapter
#   marine::track-vessel   mmsi      Begin tracking a specific vessel
#   marine::replay-voyage  voyage_id from=""  Replay historical voyage data
#   billing::generate-invoices       Generate pending invoices
#   auth::rotate-keys                Rotate auth provider signing keys

# Run a task
fab task run marine::sync-vessels
fab task run marine::sync-vessels --limit 500
fab task run marine::track-vessel --mmsi 123456789

# Or call just directly — same thing
just marine::sync-vessels
just marine::track-vessel mmsi=123456789
```

`fab task run` is a thin wrapper over `just` — it resolves to the generated justfile,
passes arguments through, and streams output. FDEs can use `just` directly if they prefer.

---

## 5. When a Task Needs Real Code

For tasks with logic too complex for a shell recipe, the justfile calls into a
proper Python package. The package is under `packages/python/` like any other —
it's just not required for every layer that has tasks.

```just
# layers/meta-marine/justfile — recipe delegates to Python package
analyse-voyage voyage_id:
    uv run python -m meta_marine_analysis.voyage_analyser --voyage-id {{voyage_id}}
```

```
layers/meta-marine/
└── packages/
    └── python/
        ├── ais_service/
        └── voyage_analysis/            ← optional: only when logic warrants it
            ├── pyproject.toml
            └── src/
                └── meta_marine_analysis/
                    └── voyage_analyser.py
```

The justfile is always the entry point. The Python package is an implementation detail.
Not every layer needs one. Most tasks are a few shell lines.

---

## 6. layer.yaml Task Registration

```yaml
# layers/meta-marine/layer.yaml
spec:
  provides:
    tasks:
      justfile: justfile        # path relative to layer root (default: justfile)
      namespace: marine         # module name in gen/justfile (default: layer name)
```

If a layer has no `justfile`, the `tasks` key is omitted. Registration is minimal —
`fab sync` handles discovery and merging.

---

## 7. The `meta-devops` Layer

`meta-devops` provides the task runner facade. The default is `just`.

```
facade: task-runner
    ├── just     → justfile recipes (default — language-agnostic)
    ├── invoke   → Python @task functions (Python-heavy projects)
    └── make     → legacy Makefile support
```

```yaml
# foundry.yaml
adapters:
  task-runner: just    # default
```

Switching to `invoke` changes how layer task packages are structured
(Python modules with `@task` decorators instead of justfiles) but the
`fab task run` interface and `layer.yaml` registration are unchanged.

---

## 8. Tasks vs Pipelines vs Actions

| Mechanism | Initiated by | Use case |
|-----------|-------------|----------|
| **Pipeline** | Schedule / event / stream | Continuous data ingestion into ontology |
| **Action** | User / app via OSDK | Transactional user operations with preconditions |
| **Task** | FDE / CI / operator | Operational commands: sync, seed, rotate, replay |

A pipeline syncs data on a schedule. A task is what the FDE runs manually
to force a re-sync, seed test data, or trigger a one-off operation.
Tasks are not called from app code — that's what gRPC services and Actions are for.

---

## 9. Repository Structure

```
foundry/
├── justfile                        # FDE-owned root justfile — imports gen/justfile
│
├── gen/
│   └── justfile                    # GENERATED — mod imports for all active layers
│
└── layers/
    ├── meta-devops/                # task runner facade
    │   ├── layer.yaml
    │   └── packages/
    │       └── python/
    │           ├── adapter_just/
    │           └── adapter_invoke/
    ├── meta-marine/
    │   ├── justfile                # layer tasks (shell recipes)
    │   └── packages/python/...
    └── meta-billing/
        ├── justfile
        └── packages/python/...
```

---

## 10. Design Rules

1. **Tasks are justfile recipes by default** — no Python package required for a task to exist.
2. **`justfile` sits at the layer root** — alongside `schema/`, `idl/`, `pipelines/`. It is a first-class layer file, not a package.
3. **`gen/justfile` is generated** — never hand-edited. Regenerated by `fab sync`.
4. **The root `justfile` is FDE-owned** — it imports `gen/justfile` and can add project-level recipes.
5. **Complex task logic lives in a package** — the justfile recipe calls into it. The package is opt-in, not required.
6. **Tasks are not callable from app code** — they are CLI-only. Cross-service calls use gRPC or Actions.
7. **`just` is the default runner** — swap to `invoke` for Python-heavy projects via `foundry.yaml`.
