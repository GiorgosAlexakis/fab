# The ontology servers

The ontology has two runtime stores, and one server in front of each:

| Server | Plane | Holds | Port |
|--------|-------|-------|------|
| `ontology-registry` | metadata | versioned ontology snapshots, environment tags | 8081 |
| `ontology-objectstore` | data | object instances, their current property values, links | 8082 |

Both are **internal servers, not fab services**. A fab service is declared in a
layer, generated from proto and packaged into an assembly by `fab build`; these
two are the substrate those services run against. They are therefore plain Go
binaries with a plain JSON API, described by `docker-compose.yml` and
`build/Dockerfile` rather than by a service manifest.

## Running them

```bash
docker compose up -d          # PostgreSQL, the registry, the object store
docker compose logs -f ontology-registry
docker compose down -v        # stop, and discard the data
```

`make up`, `make logs` and `make down` are the same three commands.

Both planes share one PostgreSQL database by default, which is the common
single-instance deployment. They are separable: point
`FAB_OBJECT_STORE_DB_URL` at a different database and nothing else changes.

Each server owns its schema and brings it up to date on startup, so a fresh
`docker compose up` needs no migration step. `--skip-migrations` turns that off
for a deployment that applies migrations out of band.

## Configuration

Every setting is a flag, and every flag has an environment variable so a
container needs no command line. A flag wins over the environment.

`ontology-registry`:

| Flag | Environment | Default |
|------|-------------|---------|
| `--listen` | `FAB_LISTEN_ADDRESS` | `:8081` |
| `--database-url` | `FAB_REGISTRY_DB_URL` | required |
| `--skip-migrations` | | `false` |
| `--log-level` | `FAB_LOG_LEVEL` | `info` |

`ontology-objectstore`:

| Flag | Environment | Default |
|------|-------------|---------|
| `--listen` | `FAB_LISTEN_ADDRESS` | `:8082` |
| `--database-url` | `FAB_OBJECT_STORE_DB_URL` | required |
| `--registry-url` | `FAB_REGISTRY_URL` | `http://localhost:8081` |
| `--ontology-name` | `FAB_ONTOLOGY_NAME` | required |
| `--ontology-tag` | `FAB_ONTOLOGY_TAG` | `dev` |
| `--ontology-refresh-interval` | | `30s` |
| `--skip-migrations` | | `false` |
| `--log-level` | `FAB_LOG_LEVEL` | `info` |

Both serve `GET /healthz` (the process is up) and `GET /readyz` (its database
is reachable).

## The registry API

`fab schema` talks to the registry over this API rather than to its database, so
only the server holds database credentials. Point the CLI at it with
`--registry-url` or `$FAB_REGISTRY_URL`.

```
POST   /v1/ontologies/{name}/versions                       publish
GET    /v1/ontologies/{name}/versions                       list
GET    /v1/ontologies/{name}/versions/{version}             version metadata
GET    /v1/ontologies/{name}/versions/{version}/snapshot     compiled ontology
GET    /v1/ontologies/{name}/versions/{version}/dictionary    stable type and property ids
POST   /v1/ontologies/{name}/versions/{version}/deprecate     deprecate
GET    /v1/ontologies/{name}/tags/{tag}                     resolve a tag
GET    /v1/ontologies/{name}/tags/{tag}/snapshot            resolve and compile
GET    /v1/ontologies/{name}/tags/{tag}/dictionary          resolve and map ids
PUT    /v1/ontologies/{name}/tags/{tag}                     point a tag at a version
POST   /v1/ontologies/{name}/tags/{tag}/promote             move a tag to another tag's version
POST   /v1/ontologies/{name}/tags/{tag}/rollback            undo the last move
```

Errors carry a machine-readable reason, so a client can react to the cause
rather than to a message:

```json
{ "reason": "AlreadyExists", "message": "acme-corp:1.3.0 is already published with different content" }
```

## The object store API

Every request is served against one ontology version: the one it selects with
`?tag=` or `?version=`, or the server's default tag. The version is resolved
from the registry, never taken from the request, so a client cannot write values
the ontology does not allow.

```
GET    /v1/ontology                                          which version is being enforced
POST   /v1/objects/{layer}/{type}                            create or update
GET    /v1/objects/{layer}/{type}                            list, filter, page
GET    /v1/objects/{layer}/{type}/{key}                      read one
DELETE /v1/objects/{layer}/{type}/{key}                      delete, applying link delete policies
GET    /v1/objects/{layer}/{type}/{key}/links/{traversal}    follow a link
PUT    /v1/links/{layer}/{link}                              link two objects
DELETE /v1/links/{layer}/{link}                              unlink two objects
```

Any query parameter that is not `tag`, `version`, `limit`, `offset` or `total` is
read as a property filter, parsed against that property's declared type:

```bash
curl 'http://localhost:8082/v1/objects/app/Customer?tier=pro&limit=10&total'
```

An object's layer and type are path segments everywhere except in a link body,
where both ends are named by their layer-qualified type, the same form the store
reports in an object's `type`:

```bash
curl -X PUT http://localhost:8082/v1/links/app/CustomerOrders \
     -H 'Content-Type: application/json' \
     -d '{"source":{"type":"app/Customer","primaryKey":"CUST-1"},
          "target":{"type":"app/Order","primaryKey":"ORD-1"}}'
```

A link is traversed by name in either direction: `forwardName` from the source,
`reverseName` from the target. Both default from the link type name, so
`CustomerOrders` reads as `customer_orders` from a customer and `customer` from
an order.

### Deleting is asymmetric

`onSourceDelete` is the only delete policy a link type has, so it only governs
deleting the object on the source end. Deleting a `Customer` consults every link
type `Customer` is the source of: `restrict` fails the delete while linked orders
exist, `cascade` deletes them, and `detach` and `set_null` both drop the link and
leave them.

Deleting the object on the target end has no declared policy, so the link rows go
and the objects on the other side survive. Deleting an `Order` unlinks it from its
customer and nothing else happens.

That asymmetry is the schema's, not the store's: model the owning side as the
source, the way `CustomerOrders` does. A link pointing from the dependent object to
the one it depends on can only protect the wrong direction.

A tag is mutable, so the resolved ontology is cached for
`--ontology-refresh-interval` rather than for the lifetime of the process:
promoting a version reaches a running store without a restart. The plan's end
state is `LISTEN/NOTIFY` invalidation on the tag table, which is the same
behaviour with an immediate rather than a bounded delay.

## Example

```bash
docker compose up -d
export FAB_REGISTRY_URL=http://localhost:8081

fab schema validate
fab schema publish --version 0.1.0
fab schema tag dev 0.1.0

curl -X POST http://localhost:8082/v1/objects/app/Customer \
     -H 'Content-Type: application/json' \
     -d '{"set":{"customer_id":"CUST-1","email":"a@corp.com","tier":"pro"}}'

curl 'http://localhost:8082/v1/objects/app/Customer/CUST-1'
curl 'http://localhost:8082/v1/objects/app/Customer/CUST-1/links/customer_orders'
```

The primary key goes in `set` like any other property: it is one of the object's
declared properties, and the store reads the object's identity from it.
