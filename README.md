# fab

`fab` composes domain layers into a deployable company stack. The ontology is the
centre of that stack: schema documents describe the business domain, and `fab`
compiles them into a versioned ontology that services, generated clients and the
object store all bind against.

This repository currently implements ELO Phase 1 -- `ObjectType`, `Property` and
`LinkType` -- with the two runtime stores behind it.

## Layout

```
cmd/fab/                    the fab CLI
cmd/ontology-registry/      the metadata plane server
cmd/ontology-objectstore/   the data plane server
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
fab schema validate     compile the active layers and report what they define
fab schema publish      publish the compiled ontology as a version
fab schema list         list the versions of an ontology
fab schema tag          point an environment tag at a version
fab schema promote      move a tag to whatever version another tag points at
fab schema rollback     return a tag to its previous version
fab version             print version information
```
