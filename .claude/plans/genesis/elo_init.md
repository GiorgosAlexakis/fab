# FAB Consumption Model

> How a company adopts FAB and structures their foundry.
> The key distinction: the FAB framework and a company's foundry are different things.
> Consumers do not fork FAB. They install the `fab` CLI and create their own repo.
> Official upstream layers are a single bundle, not N separate repos.

---

## 1. Two Things That Must Not Be Confused

```
FAB framework (one repo)                Company foundry (their own repo)
─────────────────────────────────────   ─────────────────────────────────────
github.com/fab-oss/foundry              github.com/acme-corp/foundry

fab/                                    foundry.yaml      (layer selection)
  └── CLI source → PyPI `fab` pkg       foundry.lock      (pinned bundle SHA)
layers/                                 .fab/cache/       (gitignored, fab sync)
  ├── meta-elo/                           └── foundry-a3f/layers/meta-*/
  ├── meta-core/                        layers/
  ├── meta-auth/                          ├── meta-elo  → symlink into cache
  └── ...                                 ├── meta-core → symlink into cache
                                          └── meta-marine/  (acme's local layer)
Who touches it:                         schema/  (acme's schema)
  FAB maintainers                       apps/    (acme's apps)

                                        Who touches it:
                                          Acme engineers

Git relationship: none
  acme's foundry has no git connection to fab-oss/foundry.
  It only references fab-oss/foundry as a bundle URL in foundry.lock.
```

---

## 2. The Three Wrong Models

### ❌ Fork the FAB repo

Merging upstream changes is painful. No boundary between your code and the framework.
Engineers edit upstream layer files by accident. The fork model implies you intend to
contribute back — most companies don't.

### ❌ N submodules — one per layer

```
# DO NOT DO THIS
layers/meta-elo/    ← submodule
layers/meta-core/   ← submodule
layers/meta-auth/   ← submodule
layers/meta-billing/ ← submodule
...
```

Yocto does not do this. Related official layers are bundled together in one repo
and cloned once. N submodules means N git operations, N separate SHAs to pin,
N separate upgrade workflows, and no ability to release official layers as a
compatible set.

### ❌ Copy the structure once and diverge

One-time copy with no upgrade path. You are silently accumulating drift from day one.

---

## 3. Installing `fab`

`fab` is a standalone CLI package — not a repo to clone. Consumers never touch the
`fab-oss/foundry` repo directly.

```bash
# Pre-release: install from GitHub source
uv tool install "fab @ git+https://github.com/fab-oss/foundry.git"

# Post-release: install from PyPI
uv tool install fab

# Verify
fab --version
# fab 1.2.0
```

`fab` is versioned independently of the upstream layer bundle. It does not bundle
layer code — that is fetched separately by `fab sync`. Upgrading `fab` is
`uv tool upgrade fab`; it does not touch your foundry or your layers.

---

## 4. The Bundle Model (from Yocto/kas)

Yocto's `meta-openembedded` repo contains 15+ layers in a single repository.
`kas` (the Yocto companion tool) clones a small number of these bundle repos at
pinned commits. `bblayers.conf` then selects which layers within each bundle are active.

FAB follows this model exactly:

```
fab-oss/foundry                     ONE bundle repo — all official FAB layers
├── fab/                            The fab CLI source
└── layers/
    ├── meta-elo/                   Foundation layer
    ├── meta-core/                  Base types
    ├── meta-auth/                  Auth domain
    ├── meta-billing/               Billing domain
    ├── meta-comms/                 Communications
    ├── meta-events/                Event bus
    ├── meta-storage/               Object storage
    ├── meta-data/                  Database
    ├── meta-observability/         OTEL
    ├── meta-ai/                    AI adapter
    └── meta-devops/                Task runner
```

`foundry.lock` pins **one commit SHA** for this entire bundle. All official layers
move together as a compatible set — no mix-and-match versioning between them.

```yaml
# foundry.lock (excerpt)
bundle:
  url: https://github.com/fab-oss/foundry.git
  ref:  v1.2.0
  git_ref: a3f8c21d9e4b6f0123456789abcdef0123456789   # exact SHA
  digest: sha256:abc...
  layers:
    - meta-elo
    - meta-core
    - meta-auth      # only declared layers are checked out
```

---

## 5. How `fab sync` Works

`fab sync` is the FAB equivalent of `kas checkout`. It clones the bundle at the
pinned commit into a local cache and makes the selected layers available:

```bash
fab sync

# 1. Clone fab-oss/foundry at foundry.lock git_ref → .fab/cache/foundry-a3f8c21/
#    (sparse checkout — only the layers declared in foundry.yaml)
# 2. Symlink layers/meta-elo  → .fab/cache/foundry-a3f8c21/layers/meta-elo
#    Symlink layers/meta-core → .fab/cache/foundry-a3f8c21/layers/meta-core
#    Symlink layers/meta-auth → .fab/cache/foundry-a3f8c21/layers/meta-auth
#    (layers/meta-marine/ is a local directory — not touched)
# 3. uv sync   (installs all Python packages including layer packages)
# 4. fab resolve --check  (validates layer graph + lock consistency)
```

The cache directory is gitignored. The symlinks in `layers/` point into the cache.
From `fab build`'s perspective, all layers look identical — local directory or
symlink makes no difference.

---

## 6. The Right Model: `fab init`

```bash
# 1. Install the FAB CLI (one-time, global)
uv tool install fab

# 2. Create your company foundry
fab init acme-corp
cd acme-corp

# 3. Pull the official layer bundle + install packages
fab sync

# 4. Your foundry is ready
```

After `fab sync`, the foundry looks like:

```
acme-corp/
├── .git/
├── .gitignore                          # includes .fab/
│
├── .fab/
│   └── cache/
│       └── foundry-a3f8c21/           # gitignored — managed by fab sync
│           └── layers/
│               ├── meta-elo/
│               ├── meta-core/
│               └── meta-auth/
│
├── foundry.yaml                        # layer selection — edit this
├── foundry.lock                        # single bundle pin — commit this
│
├── layers/
│   ├── meta-elo  -> ../.fab/cache/foundry-a3f8c21/layers/meta-elo    (symlink)
│   ├── meta-core -> ../.fab/cache/foundry-a3f8c21/layers/meta-core   (symlink)
│   ├── meta-auth -> ../.fab/cache/foundry-a3f8c21/layers/meta-auth   (symlink)
│   └── meta-marine/                    # LOCAL — regular directory, in this repo
│
├── schema/
├── apps/
├── assemblies/
├── deployments/
├── justfile
└── CLAUDE.md
```

No submodules. No `.gitmodules`. One lock entry for the entire official bundle.

---

## 7. `foundry.yaml` — Layer Selection

```yaml
apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp

spec:
  bsp: aws

  # Official layers — all come from the fab-oss/foundry bundle
  layers:
    - name: meta-elo
      version: ">=1.0.0, <2.0.0"
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
    - name: meta-auth
      version: ">=1.0.0, <2.0.0"

  # Third-party or community layers — referenced by URL, pinned separately
  external:
    - name: meta-payments
      url: https://github.com/stripe-contrib/meta-payments.git
      version: ">=2.1.0"

  adapters:
    auth:    cognito
    billing: stripe

  mcp:
    enabled: true
    port: 7777
```

**Official layers** are selected by name — they come from the bundle and move together.
**External layers** are referenced by URL — each is pinned separately in `foundry.lock`.

---

## 8. Adding and Upgrading Official Layers

```bash
# Activate an additional official layer (already in the bundle)
fab layer add meta-billing
# → adds meta-billing to foundry.yaml layers list
# → fab resolve  (validates deps)
# → fab sync     (adds symlink for meta-billing)

# No new downloads — meta-billing is already in the cached bundle

# Upgrade the entire official bundle to a new release
fab upgrade
# → checks fab-oss/foundry for latest compatible release
# → updates foundry.lock git_ref
# → fab sync  (re-clones bundle at new SHA, updates symlinks)
# → runs schema compat check: no breaking changes introduced
```

Adding an official layer is fast — the bundle is already cached. Only the symlink and
`foundry.yaml` entry are new. Upgrading re-clones the bundle at the new SHA.

---

## 9. Adding Third-Party Layers

```bash
# Add an external layer by URL
fab layer add https://github.com/stripe-contrib/meta-payments.git

# → adds to foundry.yaml external section
# → clones to .fab/cache/meta-payments-b9e1f23/
# → adds symlink layers/meta-payments → .fab/cache/...
# → adds separate pin to foundry.lock:
#     external:
#       - name: meta-payments
#         url: https://github.com/stripe-contrib/meta-payments.git
#         git_ref: b9e1f2345...

# Upgrade a specific external layer
fab layer upgrade meta-payments
```

External layers are pinned individually in `foundry.lock`, separate from the bundle pin.
Each has its own cache entry and its own upgrade path.

---

## 10. Adding Your Own Layers

```bash
# Create a new local layer
fab new layer meta-marine \
  --describe "AIS vessel tracking with MarineTraffic and VesselTracker data providers"

# → creates layers/meta-marine/ as a regular directory in your repo
# → layer.yaml has origin: local
# → generates schema, idl, packages scaffold + CLAUDE.md
```

```yaml
# layers/meta-marine/layer.yaml
metadata:
  name: meta-marine
  origin: local    # lives in your repo, you own it
```

Local layers are regular directories in your repo. From `fab build`'s perspective,
`layers/meta-marine/` (your code) and `layers/meta-auth/` (a symlink into cache)
are identical — both are layer directories with a `layer.yaml`.

---

## 11. Never Fork an Upstream Layer

If you need to extend an official layer, never fork it.
Three options in order of preference:

### Option A — Schema extension (non-invasive)

```yaml
# layers/meta-marine/schema/aspects/org_risk.yaml
kind: Aspect
spec:
  extends:
    layer: meta-core
    type: Organization
  properties:
    - name: sanctions_risk_score
      type: float
```

### Option B — Behavioral extension via hooks

```yaml
# layers/meta-marine/schema/hooks/post_vessel_update.yaml
kind: Hook
spec:
  trigger: post-action
  target:
    layer: meta-core
    action: UpdateOrganization
  implementation:
    python: meta_marine.hooks:recheck_sanctions
```

### Option C — Wrapping layer

Create your own layer that depends on and extends the upstream one:

```yaml
# layers/meta-auth-acme/layer.yaml
spec:
  dependsOn:
    - name: meta-auth
      version: ">=1.0.0"
  provides:
    adapters:
      - facade: auth
        implementations: [acme-sso]   # your custom SSO provider
```

If none of these work, the upstream layer is missing an extension point — that is
a bug in the upstream layer. Open an issue. Do not fork.

---

## 12. Contributing Back to the Bundle

If a fix belongs in the official bundle (not your company layer):

```bash
# 1. Work on the cached bundle copy on a feature branch
cd .fab/cache/foundry-a3f8c21
git remote add fork https://github.com/your-github/foundry.git
git checkout -b fix/cognito-token-refresh

# 2. Make changes to layers/meta-auth/
git commit -m "fix: handle Cognito token refresh race condition"
git push fork fix/cognito-token-refresh

# 3. Open a PR to fab-oss/foundry
# 4. Once merged, fab upgrade will bring it in on the next bundle release
```

---

## 13. Phase 1 vs Phase 2

**Phase 1 (current) — Bundle clone:**

```yaml
# foundry.lock
bundle:
  url: https://github.com/fab-oss/foundry.git
  git_ref: a3f8c21d9e4b6f0123456789abcdef0123456789
```

`fab sync` clones the bundle git repo at this exact SHA. Sparse checkout fetches
only the selected layers. Cache is local.

**Phase 2 — FAB Layer Registry:**

```yaml
# foundry.lock
bundle:
  url: https://registry.fab-oss.io/bundles/foundry/1.2.0.tar.gz
  digest: sha256:abc...
```

`fab sync` downloads a pre-built tar archive from the registry. No git clone.
Faster, no git history, content-addressed. The registry serves signed archives
that match the bundle's git tag.

**The FDE workflow is identical in both phases.** `foundry.yaml` and `foundry.lock`
look the same. `fab layer add`, `fab upgrade`, `fab sync` work identically.
The Phase 1 → Phase 2 transition is a `fab` CLI change, not a foundry change.

---

## 14. The FAB Repo — One Repo, Two Artifacts

`fab-oss/foundry` is a single repository. It is not split.

```
fab-oss/foundry/
├── fab/                    ← the CLI (Python package, published to PyPI as `fab`)
│   ├── pyproject.toml
│   └── src/fab/
│       ├── cli.py
│       ├── commands/
│       └── ...
│
└── layers/                 ← the official layer bundle
    ├── meta-elo/
    ├── meta-core/
    ├── meta-auth/
    ├── meta-billing/
    ├── meta-comms/
    ├── meta-events/
    ├── meta-storage/
    ├── meta-data/
    ├── meta-observability/
    ├── meta-ai/
    └── meta-devops/
```

Two release artifacts, one source:

| Artifact | How released | Who consumes it |
|----------|-------------|-----------------|
| `fab` CLI | PyPI package from `fab/pyproject.toml` | `uv tool install fab` |
| Layer bundle | Git tag / registry archive from `layers/` | `fab sync` → `.fab/cache/` |

The CLI package on PyPI contains **only** the `fab/` subdirectory — no layer code.
Layer code arrives separately via `fab sync`. A consumer who installs `fab` gets a ~5MB
CLI binary. The 200MB of layer code lands in `.fab/cache/` only when they run `fab sync`.

**Why one repo:**
A new YAML key added to `layer.yaml` and the CLI parser that reads it belong in the
same PR. Splitting would require coordinating releases across two repos. The Yocto
equivalent (`poky`) ships BitBake and the core layers from the same repo for the same reason.

---

## 15. Design Rules

1. **`fab init` creates your foundry** — your repo, not a fork of anything
2. **Official layers are a bundle** — one clone, one SHA, all move together
3. **`foundry.lock` has one bundle pin** — not N layer pins
4. **`.fab/cache/` is gitignored** — `fab sync` populates it; never commit it
5. **Symlinks are the boundary** — `layers/meta-auth/` is a symlink into cache; `layers/meta-marine/` is your code
6. **External layers are pinned separately** — community/third-party layers have their own lock entries
7. **Never fork an official layer** — aspects → hooks → wrapping layer
8. **Contributing back** — PR to `fab-oss/foundry`, picked up by the next `fab upgrade`
9. **Phase 1 and Phase 2 are transparent** — FDE workflow does not change
