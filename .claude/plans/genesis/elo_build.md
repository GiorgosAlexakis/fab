# ELO Build System

> How layer code is packaged, linked, and shipped.
> The key insight: layers are proper packages first. Assemblies link them. Containers ship them.

---

## 1. Layer Code as Proper Packages

Each layer service is a first-class package in its own language ecosystem.
No special FAB packaging format — use what the language already provides.

```
layers/meta-marine/services/ais_service/
├── pyproject.toml          # Python: uv/pip package
├── src/
│   └── meta_marine/
│       └── ais_service/
│           ├── __init__.py
│           └── main.py

layers/meta-billing/services/billing_service/
├── go.mod                  # Go: module
├── go.sum
└── billing/
    └── service.go

layers/meta-comms/services/email_service/
├── Cargo.toml              # Rust: crate
└── src/
    └── lib.rs
```

The language toolchain is authoritative for that package.
FAB builds on top — it does not replace `uv`, `go`, or `cargo`.

---

## 2. Python: uv Workspace

At the repo root, a single `uv` workspace declares all Python packages as members.
This gives a unified lockfile (`uv.lock`) and allows editable cross-package installs.

```toml
# pyproject.toml  (repo root)
[tool.uv.workspace]
members = [
    # Layer service implementations
    "layers/meta-marine/packages/python/ais_service",
    "layers/meta-billing/packages/python/billing_service",
    "layers/meta-auth/packages/python/auth_service",
    "layers/meta-comms/packages/python/email_service",
    # Layer adapter packages
    "layers/meta-marine/packages/python/adapter_marinetraffic",
    "layers/meta-marine/packages/python/adapter_vesseltracker",
    "layers/meta-auth/packages/python/adapter_cognito",
    "layers/meta-auth/packages/python/adapter_auth0",
    # Generated assembly packages
    "gen/assemblies/core_services",
    "gen/assemblies/background_workers",
    "gen/assemblies/monolith",
]
# Note: tasks are justfile recipes — no Python packages in the workspace for tasks.
# If a task recipe needs a Python package, that package is listed above as a regular member.
```

Each member is an independent package with its own `pyproject.toml`.
The workspace provides a shared virtual environment — one `uv sync` installs everything.

```toml
# layers/meta-marine/packages/python/ais_service/pyproject.toml
[project]
name = "meta-marine-ais-service"
version = "1.0.0"
dependencies = [
    "grpcio>=1.62.0",
    "protobuf>=4.25.0",
    "fab-runtime>=1.0.0",
]

# Written by `fab build` from layer.yaml — never hand-edited
[project.entry-points."fab.services"]
AISService = "meta_marine.ais_service:AISServiceImpl"
```

---

## 3. The Assembly as Generated Package

`fab build <assembly>` generates a package in `gen/assemblies/<name>/`.
The package declares *which layer packages are installed* — that is the only
assembly-specific configuration. The entrypoint itself is generic.

```toml
# gen/assemblies/core_services/pyproject.toml  — GENERATED, never hand-edited
[project]
name = "assembly-core-services"
version = "0.0.0"   # version is the SBOM release version, set at release time
dependencies = [
    "meta-marine-ais-service",      # contributes AISService entry point
    "meta-billing-billing-service", # contributes BillingService entry point
    "meta-auth-auth-service",       # contributes AuthService entry point
]
```

Each listed package declares its services via Python entry points in its own
`pyproject.toml` — written by `fab build` from `layer.yaml`. The assembly
entrypoint discovers them at runtime without knowing layer names.

In dev, all members are editable installs — changes to layer code are immediately
reflected without reinstalling. In prod, wheels are built and copied into the image.

---

## 4. Dev vs Prod Package Resolution

### Dev — editable installs (live changes)

```bash
uv sync
# Installs all workspace members as editable installs (PEP 660 / src layout)
# layers/meta-marine/services/ais_service installed as symlink into venv
# Editing ais_service/main.py is reflected immediately — no reinstall needed
```

The assembly entrypoint is generic — no layer names, no direct imports:
```python
# gen/assemblies/core_services/main.py  (identical for every assembly)
from importlib.metadata import entry_points
import grpc, asyncio

async def serve():
    server = grpc.aio.server()
    for ep in entry_points(group="fab.services"):
        ep.load().register(server)    # discovers AISServiceImpl, BillingServiceImpl, etc.
    server.add_insecure_port('[::]:50051')
    await server.start()
    await server.wait_for_termination()
```

Which services appear is determined entirely by which packages are installed —
the assembly's `pyproject.toml` dependencies, not the entrypoint code.

### Prod — wheel builds

```bash
fab build core-services --mode prod

# For each layer package in the assembly:
uv build layers/meta-marine/packages/python/ais_service      # → dist/meta_marine_ais_service-1.0.0-py3-none-any.whl
uv build layers/meta-billing/packages/python/billing_service # → dist/...
uv build gen/assemblies/core_services                        # → dist/assembly_core_services-...whl

# Dockerfile COPY those wheels and pip install --no-index from local dist/
```

---

## 5. Generated Dockerfile per Assembly

`fab build <assembly>` generates a Dockerfile alongside the entrypoint.

```dockerfile
# gen/assemblies/core_services/Dockerfile  — GENERATED
FROM python:3.12-slim-bookworm

WORKDIR /app

# Copy pre-built wheels (produced by uv build)
COPY dist/meta_marine_ais_service-*.whl .
COPY dist/meta_billing_billing_service-*.whl .
COPY dist/meta_auth_auth_service-*.whl .
COPY dist/assembly_core_services-*.whl .

# Install from local wheels only — no network access at image build time
RUN pip install --no-index --find-links=. \
    meta-marine-ais-service \
    meta-billing-billing-service \
    meta-auth-auth-service \
    assembly-core-services

# Generated entrypoint
CMD ["python", "-m", "assembly_core_services.main"]
```

For cross-language assemblies (Python + Go), the base image includes both runtimes
and each service binary is compiled and copied in separately.

---

## 6. Go and Rust Layer Packages

### Go

Each Go service is a proper Go module. Go modules within the repo reference
each other via `replace` directives in `go.work` (Go workspace):

```
# go.work  (repo root)
go 1.22

use (
    ./layers/meta-billing/packages/go/billing_service
    ./layers/meta-auth/packages/go/auth_service
)
```

```go
// layers/meta-billing/packages/go/billing_service/go.mod
module github.com/acme/meta-billing/billing-service

go 1.22

require (
    google.golang.org/grpc v1.62.0
    github.com/acme/fab-runtime v1.0.0
)
```

Prod build: `go build -o bin/billing-service ./...`
The binary is `COPY`d into the generated Dockerfile.

### Rust

Each Rust service is a crate in a Cargo workspace:

```toml
# Cargo.toml  (repo root)
[workspace]
members = [
    "layers/meta-comms/packages/rust/email_service",
]
```

Prod build: `cargo build --release`
Binary copied into Dockerfile.

---

## 7. Upstream Layers as a Bundle (Phase 1)

All official FAB layers live in a single repo (`fab-oss/foundry`) — not in separate
per-layer repos. This is the Yocto model: `meta-openembedded` bundles 15+ layers
in one repo; `kas` clones it once at a pinned commit.

`fab sync` clones the bundle into `.fab/cache/` (gitignored) and symlinks the
selected layers into `layers/`:

```
foundry/                              # consumer's repo
├── .fab/
│   └── cache/
│       └── foundry-a3f8c21/         # gitignored — cloned by fab sync
│           └── layers/
│               ├── meta-elo/
│               ├── meta-core/
│               └── meta-auth/
├── layers/
│   ├── meta-elo  → ../.fab/cache/foundry-a3f8c21/layers/meta-elo   (symlink)
│   ├── meta-core → ../.fab/cache/foundry-a3f8c21/layers/meta-core  (symlink)
│   ├── meta-auth → ../.fab/cache/foundry-a3f8c21/layers/meta-auth  (symlink)
│   └── meta-marine/                 # local layer — regular directory, in this repo
```

No `.gitmodules`. No submodules. One bundle pin covers all official layers.

---

## 8. foundry.lock Carries the Bundle Pin

`foundry.lock` pins one commit SHA for the entire official bundle.
All official layers move together — no mix-and-match versioning between them.

```yaml
# foundry.lock (generated, committed, never hand-edited)
bundle:
  url:     https://github.com/fab-oss/foundry.git
  ref:     v1.2.0
  git_ref: a3f8c21d9e4b6f0123456789abcdef0123456789   # exact SHA
  digest:  sha256:abc...
  layers:                            # which layers are active from this bundle
    - { name: meta-elo,   version: 1.0.3, digest: sha256:def... }
    - { name: meta-core,  version: 1.0.1, digest: sha256:ghi... }
    - { name: meta-auth,  version: 1.2.0, digest: sha256:jkl... }

external:                            # third-party layers pinned individually
  - name:    meta-payments
    url:     https://github.com/stripe-contrib/meta-payments.git
    git_ref: b9e1f2345abcdef0123456789abcdef0123456789
    digest:  sha256:mno...

local:                               # local layers (content hash for sstate)
  - name:    meta-marine
    path:    layers/meta-marine
    digest:  sha256:pqr...
```

`fab sync` enforces that the cached bundle SHA matches `foundry.lock`.
It will not proceed if they diverge.

---

## 9. Phase 2 — FAB Layer Registry

In Phase 2, the bundle is served as a pre-built archive from the registry.
No git clone required — `fab sync` downloads and unpacks a content-addressed tar.

```yaml
# foundry.lock (Phase 2)
bundle:
  url:    https://registry.fab-oss.io/bundles/foundry/1.2.0.tar.gz
  digest: sha256:abc...
  layers:
    - { name: meta-elo,  version: 1.0.3, digest: sha256:def... }
    - { name: meta-core, version: 1.0.1, digest: sha256:ghi... }
    - { name: meta-auth, version: 1.2.0, digest: sha256:jkl... }
```

The FDE workflow is identical in both phases. `foundry.lock` is the contract.
Phase 1 → Phase 2 is a `fab` CLI change, not a foundry structure change.

---

## 10. The Three Setup Commands

```bash
# Fresh clone or after pulling changes
fab sync
# 1. Clone fab-oss/foundry bundle at foundry.lock git_ref → .fab/cache/  (Phase 1)
#    or download bundle archive from registry                              (Phase 2)
# 2. Sparse checkout — only the layers declared in foundry.yaml
# 3. Symlink layers/meta-*/  →  .fab/cache/.../layers/meta-*/
# 4. uv sync                   (Python: installs editable workspace)
# 5. go work sync              (Go: sync go.work)
# 6. fab resolve --check       (validate layer graph + lock consistency)

# Activate an official layer (already in the bundle — no new download)
fab layer add meta-comms
# 1. Adds meta-comms to foundry.yaml layers list
# 2. fab resolve  (resolves deps, updates foundry.lock layers list)
# 3. fab sync     (adds symlink for meta-comms from existing cache)
# 4. uv sync      (installs meta-comms packages into workspace)

# Upgrade to a new bundle release
fab upgrade
# 1. Checks fab-oss/foundry for latest release compatible with foundry.yaml version ranges
# 2. Updates foundry.lock bundle git_ref to new SHA
# 3. fab sync  (re-clones bundle at new SHA, updates symlinks)
# 4. Runs schema compat check: no breaking changes introduced
```

---

## 11. Build Pipeline

```
fab build <assembly>
        │
        ├── 1. fab resolve --check     (validate lock is current)
        ├── 2. buf generate                (proto → generated stubs, per layer)
        ├── 3. fab schema compile          (YAML → Atlas HCL + pydantic models)
        ├── 4. uv build <layer packages>   (Python: produce wheels)
        ├── 5. go build                    (Go: produce binaries)
        ├── 6. cargo build --release       (Rust: produce binaries)
        ├── 7. Generate assembly package   (gen/assemblies/<name>/pyproject.toml + main.py)
        ├── 8. uv build assembly package   (produce assembly wheel)
        └── 9. Generate Dockerfile         (gen/assemblies/<name>/Dockerfile)

fab build <assembly> --push
        │
        └── 10. docker buildx build --push  (build + push to registry, tagged with SBOM version)
```

sstate cache applies at steps 2–6: if a layer's content hash matches the cache,
its outputs are reused without rerunning. Only changed layers are rebuilt.

---

## 12. Repository Structure

```
foundry/
├── pyproject.toml              # uv workspace root — lists all Python member packages
├── uv.lock                     # uv universal lockfile (committed)
├── go.work                     # Go workspace (if any Go layers active)
├── go.work.sum
├── Cargo.toml                  # Cargo workspace (if any Rust layers active)
│
├── foundry.yaml                # layer selection + adapter config
├── foundry.lock                # bundle pin + local layer digests (committed)
│
├── .fab/
│   └── cache/                  # gitignored — managed by fab sync
│       └── foundry-a3f8c21/    # cloned bundle at pinned SHA
│           └── layers/
│               ├── meta-elo/
│               ├── meta-core/
│               └── meta-auth/
│
├── layers/
│   ├── meta-elo  →  ../.fab/cache/foundry-a3f8c21/layers/meta-elo   (symlink)
│   ├── meta-core →  ../.fab/cache/foundry-a3f8c21/layers/meta-core  (symlink)
│   ├── meta-auth →  ../.fab/cache/foundry-a3f8c21/layers/meta-auth  (symlink)
│   └── meta-marine/            # local layer — regular directory, in this repo
│       ├── layer.yaml
│       ├── schema/
│       ├── idl/
│       ├── packages/
│       │   └── python/
│       │       ├── ais_service/
│       │       │   └── pyproject.toml
│       │       ├── adapter_marinetraffic/
│       │       └── adapter_vesseltracker/
│       ├── justfile            # task recipes
│       ├── pipelines/
│       └── infra/
│
├── justfile                    # FDE-owned root justfile (imports gen/justfile)
│
├── gen/                        # GENERATED — never hand-edited
│   ├── justfile                # generated mod imports for all layer justfiles
│   └── assemblies/
│       ├── core_services/
│       │   ├── pyproject.toml  # generated assembly package
│       │   ├── main.py         # generated entrypoint
│       │   └── Dockerfile      # generated container definition
│       └── background_workers/
│           ├── pyproject.toml
│           ├── main.py
│           └── Dockerfile
│
└── dist/                       # built wheels and binaries (gitignored)
    ├── meta_marine_ais_service-1.0.0-py3-none-any.whl
    └── assembly_core_services-1.3.0-py3-none-any.whl
```

---

## 13. Design Rules

1. **`fab new` scaffolds a `CLAUDE.md` alongside every generated component** — layer, app, assembly. Never generated without it.
2. **Layer packages are standard language packages** — `uv`, `go`, and `cargo` are authoritative. FAB adds no proprietary packaging format.
3. **The workspace root is the dev environment** — `fab sync` sets up everything. No manual steps.
4. **`gen/` is never hand-edited** — all assembly packages and Dockerfiles are generated outputs.
5. **`foundry.lock` is the reproducibility contract** — bundle SHA, layer digests, external layer pins. Always committed.
6. **`.fab/cache/` is gitignored** — `fab sync` populates it; the bundle SHA in `foundry.lock` is the source of truth.
7. **`dist/` is gitignored** — built artifacts are never committed. Produced by the build pipeline and pushed to the container registry.
8. **sstate cache prevents redundant rebuilds** — layers are rebuilt only when their content hash changes. CI restores the sstate directory between runs.
9. **Phase 1 (bundle clone) and Phase 2 (registry archive) are transparent to FDE** — `fab layer add` and `fab sync` work identically in both phases.
