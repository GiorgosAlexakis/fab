# Layers

A layer is a composable domain module: a directory with a `layer.yaml` manifest and
whatever it contributes. Today that is schema. The ontology is the union of the
schema every active layer contributes, merged in dependency order.

This document covers what fab implements. Layer fetching (`fab sync`), adapter
facades, BSP selection and the shared state cache are described in the plans and
are not implemented yet.

## Two repositories

Layers are developed here and consumed there. This repository is the framework: it
holds the `fab` source and, eventually, the official `meta-*` layers as one bundle.
A company's foundry is its own repository, and it contains almost nothing.

```
this repository                          a company foundry
──────────────────────────────────       ──────────────────────────────────
cmd/, pkg/     the fab source            foundry.yaml    layer selection
layers/                                  foundry.lock    the pinned layer set
  meta-elo/    ontology runtime          .fab/cache/     gitignored, fetched
  meta-core/   base entities             layers/
  meta-auth/                               meta-elo  -> symlink into cache
  ...                                      meta-core -> symlink into cache
                                           meta-acme/    a local layer
                                         schema/         the company's own types
```

The official layers ship as one bundle at one commit rather than as N
independently versioned repositories, so a single SHA pins all of them as a
compatible set. `foundry.lock` carries that pin, `fab sync` fetches the bundle into
`.fab/cache/`, and the entries under `layers/` are symlinks into it.

Two consequences show up in this codebase. Discovery treats a symlink and a real
directory identically, which is what lets a foundry mix upstream and local layers
without fab caring which is which. And `fab resolve` preserves the `bundle:` block
when it rewrites the lock, because resolution reads manifests and knows nothing
about where they were fetched from.

A freshly cloned foundry has dangling symlinks, since the cache is gitignored.
That is reported as what it is:

```
$ fab layers
error: 2 problems found:
  layers/meta-core: layer symlink points at a missing directory: ../.fab/cache/foundry-a3f8c21/layers/meta-core does not exist, so the upstream layer cache is missing or out of date
  layers/meta-elo: layer symlink points at a missing directory: ../.fab/cache/foundry-a3f8c21/layers/meta-elo does not exist, so the upstream layer cache is missing or out of date
```

`meta-elo` is the foundation layer and the only mandatory one: it provides the
ontology runtime every other layer builds on, which is the registry, the object
store and the schema compiler implemented here. fab does not yet require it,
because there is no fetcher to supply it and a foundry composed only of local
layers is useful today.

## The two files an FDE edits

`foundry.yaml` says which layers are active. `layer.yaml` says what a layer is.
Everything else fab knows is derived from those two.

```yaml
# foundry.yaml
apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp        # the ontology name every published version is stored under
spec:
  layers:
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
    - name: meta-auth
      version: ">=1.0.0, <2.0.0"
```

A selector may leave `version` out. The official layers are released as a set, so
pinning each of them individually is optional; the lock records the exact versions
either way.

```yaml
# layers/meta-auth/layer.yaml
apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-auth        # must match the directory name
  version: 1.2.0         # an exact version, not a range
  origin: upstream       # upstream (fab-provided) or local (yours). Defaults to local.
spec:
  dependsOn:
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
  provides:
    schema:
      objects: [Session, Permission]
      links: [UserSessions]
```

## Version ranges need an upper bound

`dependsOn[].version` must exclude some future major version. `">=1.0.0"` is
rejected; `">=1.0.0, <2.0.0"` is accepted.

The bound is the whole value of the field. It states the window a layer was tested
against, so that a breaking `meta-core:2.0.0` fails `fab resolve` instead of
failing at some later point where the cause is much harder to see. Layer
maintainers widen the bound when they have tested against the new major release.

## Resolution

`fab resolve` reads `foundry.yaml` and every `layers/*/layer.yaml`, and produces a
build order. It checks, reporting every problem in one pass:

- every layer `foundry.yaml` activates exists under `layers/`
- every layer directory has a `layer.yaml`, and its name matches the directory
- each layer's version satisfies the range `foundry.yaml` pinned it to
- every dependency is active, and its version is inside the declared window
- the dependency graph is acyclic
- each layer ships exactly the object and link types its manifest declares
- every cross-layer type reference resolves against the active layer set

The order is a function of the manifests alone. Layers with no dependency between
them are ordered by name, so the same tree always produces the same order and the
same ontology digest.

A dependency must be satisfied by a layer `foundry.yaml` activates. A layer sitting
in `layers/` that nothing declares is not active, and its types cannot satisfy a
reference: `foundry.yaml` stays the single place that decides what is in the stack.

## foundry.lock

`fab resolve` writes `foundry.lock`. Commit it; do not edit it.

```yaml
# Generated by `fab resolve`. Commit this file; do not edit it.
apiVersion: fab/v1
kind: Lock
bundle:                        # written by the fetcher, preserved by fab resolve
  url: https://github.com/fab-oss/foundry.git
  ref: v1.2.0
  gitRef: a3f8c21d9e4b6f0123456789abcdef0123456789
  layers: [meta-elo, meta-core]
locked:
- digest: sha256:69dcaaf66df25d07...
  name: meta-core
  origin: upstream
  version: 1.0.1
- dependsOn:
  - meta-core
  digest: sha256:31a64b608449c8d0...
  name: meta-auth
  origin: upstream
  version: 1.2.0
```

Entries are in build order. The digest hashes the manifest as it sits on disk, so
`fab resolve --check` notices an upstream layer that was edited without a version
bump -- the drift a pinned version would otherwise hide.

`fab resolve --check` writes nothing and fails when the lock is missing or does not
match the resolved graph. It reports what changed:

```
$ fab resolve --check
error: foundry.lock is out of date; run `fab resolve`:
  ~ meta-core 1.0.1 -> 1.1.0
  + meta-billing 1.0.0
  - meta-comms 1.0.0
```

A changed build order is reported too, even when the layer set is identical: the
merge order decides which layer can reference which types.

## How the ontology uses the resolution

`fab schema validate` and `fab schema publish` resolve the layer graph first, then
load schema in the resolved order:

```
layers/meta-core/schema/     merged first   → types owned by meta-core
layers/meta-auth/schema/     merged next    → may reference meta-core types
schema/                      merged last    → the app layer, may reference anything
```

Your own `schema/` directory is the `app` layer. It is merged last because it may
reference anything and nothing may reference it.

A type is qualified by the layer that owns it, so `meta-core/User` and
`app/User` are different types. A document may not claim to belong to a layer other
than the one shipping it, which is what stops a layer from defining types inside
someone else's namespace.

A cross-layer reference names the owning layer:

```yaml
# layers/meta-auth/schema/links/user_sessions.yaml
apiVersion: fab/v1
kind: LinkType
metadata:
  name: UserSessions
spec:
  source:
    layer: meta-core       # a type from another layer
    type: User
  target:
    type: Session          # no layer means this layer
  cardinality: one_to_many
  onSourceDelete: cascade
```

If `meta-core` is not active, that is an error from `fab resolve` and
`fab schema validate`, not a runtime failure:

```
error: layers/meta-auth/schema/links/user_sessions.yaml: link type meta-auth/UserSessions
references unknown source object type meta-core/User: either the type name is wrong
or layer "meta-core" is not active
```

The published snapshot records the resolved order in its `layers` field, so the
registry and the object store both know how the ontology was assembled.

## Inspecting a foundry

```
$ fab layers
LAYER       VERSION   ORIGIN     DEPENDS ON
meta-core   1.0.1     upstream   <none>
meta-auth   1.2.0     upstream   meta-core
```

Build order, not alphabetical order. `fab layers -o json` emits the same shape as
`foundry.lock`, so a script reading either sees identical fields.

## What is not implemented

- **Fetching layers.** `fab sync` and `fab upgrade` clone the bundle into
  `.fab/cache/` and create the symlinks under `layers/`; external layers
  referenced by URL are pinned individually. Until then, layers are read from
  `layers/` as they are, and the `bundle:` block in the lock is carried forward
  rather than written.
- **The official layers.** `layers/meta-elo/` and `layers/meta-core/` are not in
  this repository yet, so the bundle a company foundry would fetch does not exist.
  What `meta-elo` provides at runtime -- the registry, the object store and the
  schema compiler -- is implemented here as `fab` and the two servers.
- **Adapter facades.** `layer.yaml` does not carry `provides.adapters` yet, and
  `foundry.yaml` has no adapter selection. Those belong with the services work.
- **BSP and MCP selection.** Both are `foundry.yaml` keys the build work will add.
- **Aspects, interfaces and actions.** They are declarable in
  `provides.schema` so a layer written against a later ontology phase still
  resolves, but nothing compiles them yet. Declared kinds fab cannot compile are
  not checked against the tree.
