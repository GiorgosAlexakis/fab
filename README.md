# fab

`fab` composes domain layers into a deployable company stack. The ontology is the
centre of that stack: schema documents describe the business domain, and `fab`
compiles them into a versioned ontology that services, generated clients and the
object store all bind against.

This repository currently implements ELO Phase 1 -- `ObjectType`, `Property` and
`LinkType` -- the layer composition the ontology is assembled from, and the two
runtime stores behind it.

## Layout

```
cmd/fab/                    the fab CLI
cmd/ontology-registry/      the metadata plane server
cmd/ontology-objectstore/   the data plane server
pkg/foundry/                foundry.yaml: the layers a foundry activates
pkg/apis/layer/v1/          the fab/v1 Layer manifest, defaulting and validation
pkg/layers/                 layer discovery, dependency resolution and foundry.lock
pkg/apis/ontology/v1/       the fab/v1 schema types, defaulting and validation
pkg/ontology/               the YAML loader, the compiler and the compiled snapshot
pkg/registry/ontology/      the registry: interface, PostgreSQL store, API server, client
pkg/objectstore/            the object store: interface, PostgreSQL store, API server
pkg/cmd/                    one package per command group, kubectl-style
test/integration/           tests that need a live PostgreSQL
```

## Building and testing

```bash
make build              # bin/fab, bin/ontology-registry, bin/ontology-objectstore
make verify             # gofmt, boilerplate, go vet
make test               # unit tests

export FAB_TEST_POSTGRES_URL=postgres://fab:fab@localhost:5432/fab
make test-integration   # tests against a live PostgreSQL
```

## A foundry

A foundry is a directory with a `foundry.yaml`, some layers and a schema
directory. The layers are what the ontology is assembled from.

```
acme-corp/
├── foundry.yaml                    which layers are active  (you edit this)
├── foundry.lock                    the resolved, pinned layer set  (generated, committed)
├── layers/
│   ├── meta-core/
│   │   ├── layer.yaml              the layer manifest: version, dependencies, contributions
│   │   └── schema/objects/user.yaml
│   └── meta-auth/
│       ├── layer.yaml
│       └── schema/
└── schema/                         your own schema, merged last as the app layer
```

```yaml
# foundry.yaml
apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
    - name: meta-auth
      version: ">=1.0.0, <2.0.0"
```

```yaml
# layers/meta-auth/layer.yaml
apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-auth
  version: 1.2.0
  origin: upstream
spec:
  dependsOn:
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
  provides:
    schema:
      objects: [Session]
      links: [UserSessions]
```

`fab resolve` turns that into a build order and pins it:

```bash
fab resolve            # write foundry.lock
fab resolve --check    # validate the graph and the lock, write nothing (CI)
fab layers             # what is active, in build order
```

Layers are merged in dependency order, so `meta-auth` can link to a `meta-core`
type while `meta-core` knows nothing about `meta-auth`. Composition is documented
in [docs/layers.md](docs/layers.md).

## Running the ontology

```bash
docker compose up -d              # PostgreSQL, the registry, the object store
export FAB_REGISTRY_URL=http://localhost:8081

fab schema validate
fab schema publish --version 0.1.0
fab schema tag dev 0.1.0
fab schema list
```

The two servers, their configuration and their APIs are documented in
[docs/ontology-servers.md](docs/ontology-servers.md).

## Commands

```
fab resolve             resolve the layer graph and pin it in foundry.lock
fab layers              list the active layers in build order
fab schema validate     compile the active layers and report what they define
fab schema publish      publish the compiled ontology as a version
fab schema list         list the versions of an ontology
fab schema tag          point an environment tag at a version
fab schema promote      move a tag to whatever version another tag points at
fab schema rollback     return a tag to its previous version
fab version             print version information
```
