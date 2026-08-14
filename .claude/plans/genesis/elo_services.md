# ELO Services

> How custom business logic layers are structured, contracted, and deployed.
> The key invariant: layer code defines WHAT. Assemblies define HOW DEPLOYED.
> These are completely separate concerns.

---

## 1. Three Layer Types

| Type | Contains | Example |
|------|---------|---------|
| **Schema** | YAML only — ontology contributions, no service code | `meta-core` base types |
| **Adapter** | Thin provider wrapper — implements a facade contract | `cognito/`, `stripe/` |
| **Service** | Business logic code + IDL + schema + adapters | `meta-marine`, `meta-billing` |

Service layers are the unit of custom business logic.
They own their entire vertical slice: contract, implementation, schema, data ingestion.

---

## 2. Service Layer Structure

```
layers/meta-marine/
├── layer.yaml                              # manifest: type, deps, provides
│
├── schema/                                 # Ontology contributions (YAML, language-agnostic)
│   ├── objects/
│   │   ├── vessel.yaml
│   │   ├── voyage.yaml
│   │   └── port.yaml
│   ├── links/
│   │   └── vessel_voyages.yaml
│   └── actions/
│       └── track_vessel.yaml
│
├── idl/                                    # Proto contracts (language-agnostic API boundary)
│   └── proto/marine/v1/
│       ├── ais_service.proto               # gRPC service definition
│       ├── objects.proto                   # Vessel, VesselPosition, Voyage wire types
│       └── events.proto                    # VesselPositionUpdated, VoyageStarted
│
├── justfile                                # Task recipes (shell, language-agnostic)
│
├── packages/                               # All code, organized by language
│   ├── python/
│   │   ├── ais_service/                    # Service implementation package
│   │   │   ├── pyproject.toml
│   │   │   └── src/
│   │   │       └── meta_marine/
│   │   │           ├── __init__.py
│   │   │           └── ais_service.py      # implements ais_service.proto
│   │   ├── adapter_marinetraffic/          # Adapter package — MarineTraffic provider
│   │   │   ├── pyproject.toml
│   │   │   └── src/
│   │   │       └── meta_marine_marinetraffic/
│   │   └── adapter_vesseltracker/          # Adapter package — VesselTracker provider
│   │       ├── pyproject.toml
│   │       └── src/
│   │           └── meta_marine_vesseltracker/
│   └── go/                                 # Go packages (if any)
│       └── ais_gateway/
│           └── go.mod
│
├── pipelines/                              # Pipeline definitions (YAML, language-agnostic)
│   └── sync_vessel_positions.yaml
│
└── infra/                                  # Layer-scoped Terraform modules
    ├── main.tf
    └── variables.tf
```

**Five root-level concerns are language-agnostic:** `schema/`, `idl/`, `justfile`, `pipelines/`, `infra/`.
They define *what* the layer is. `packages/` defines *how* it is implemented.
A `justfile` recipe that needs real logic calls `uv run python -m <module>` — the package is opt-in.

---

## 3. IDL is Owned Per Layer

The existing `idl/` at the repo root is a shared concern.
In FAB, each service layer owns its contracts:

```
layers/meta-marine/idl/    ← marine owns its proto contracts
layers/meta-billing/idl/   ← billing owns its proto contracts
layers/meta-auth/idl/      ← auth owns its proto contracts
```

Each layer has its own `buf.yaml` and `buf.gen.yaml`.
Buf generates stubs into the layer's `gen/` directory.
The OSDK wraps those stubs behind the token-scoped interface.

This means a layer's public API is fully self-contained — its proto is its published contract,
versioned alongside its code and schema.

---

## 4. Three Communication Channels Between Layers

Layers communicate through three channels. Each has a distinct role:

```
Channel 1 — Ontology (shared typed state)
  "What is the current state of a business object?"
  Customer.tier, VesselPosition.coordinates, Invoice.status
  Accessed via OSDK (token-scoped, version-pinned)
  Backed by the object store (Atlas-managed PostgreSQL)

Channel 2 — gRPC (synchronous operations)
  "Execute this operation and give me a result now."
  AISService.GetVesselPosition(), BillingService.CreateInvoice()
  Contract: .proto file in the source layer's idl/
  Language-agnostic — any layer can consume via generated stubs

Channel 3 — Events (asynchronous broadcast)
  "Something happened. Who cares?"
  VesselPositionUpdated, InvoiceIssued, OrderPlaced
  Contract: .proto event schemas in the source layer's idl/
  Transport: adapter-backed (SNS, Kafka, RabbitMQ — from foundry.yaml)
  Subscribers declare dependency in layer.yaml — resolver validates producer exists
```

A `layer.yaml` declares all three:

```yaml
# layers/meta-marine/layer.yaml
spec:
  type: service
  dependsOn:
    - name: meta-elo
      version: ">=1.0.0, <2.0.0"
    - name: meta-core
      version: ">=1.0.0"
    - name: meta-events
      version: ">=1.0.0"

  provides:
    schema:
      objects:  [Vessel, Voyage, Port]
      links:    [VesselVoyages]
      actions:  [TrackVessel, RerouteVessel]

    services:
      - name: AISService
        proto: idl/proto/marine/v1/ais_service.proto
        impl: meta_marine.ais_service:AISServiceImpl   # fab build writes this to pyproject.toml entry points
        language: python

    events:
      publishes:
        - name: VesselPositionUpdated
          schema: idl/proto/marine/v1/events.proto#VesselPositionUpdated
        - name: VoyageStarted
          schema: idl/proto/marine/v1/events.proto#VoyageStarted
      subscribes: []

    adapters:
      - facade: ais-source
        implementations: [marinetraffic, vesseltracker]

    tasks:
      justfile: justfile      # path to layer justfile (default: justfile at layer root)
      namespace: marine       # fab task run marine::<recipe>
```

---

## 5. The Assembly — Decoupling Logic from Deployment

This is the central concept of this document.

**The problem with strict microservices:**
Running 10 separate containers for 10 layers in dev and early-stage prod is:
- Expensive — each container has CPU/memory overhead
- Slow to develop — cross-service calls need networking even locally
- Operationally complex — 10 separate deployment pipelines

**The problem with a monolith:**
- Cannot scale services independently
- One slow service blocks all others
- Deployments are all-or-nothing

**The Assembly solves this** — it is a named, configured grouping of layer service
implementations that are packaged into ONE deployable container.
Business logic code does not change between topologies. Only the assembly YAML changes.

```yaml
# assemblies/core-services.yaml
apiVersion: fab/v1
kind: Assembly
metadata:
  name: core-services

spec:
  type: grpc-server           # or: worker | rest-api | hybrid

  services:
    - layer: meta-marine
      service: AISService
    - layer: meta-billing
      service: BillingService
    - layer: meta-auth
      service: AuthService

  compute:
    replicas: { min: 2, max: 10 }
    resources:
      cpu: "2"
      memory: "4Gi"
  port: 50051
```

```yaml
# assemblies/background-workers.yaml
apiVersion: fab/v1
kind: Assembly
metadata:
  name: background-workers

spec:
  type: worker

  handlers:
    - layer: meta-marine
      worker: VesselPositionProcessor
    - layer: meta-billing
      worker: InvoiceGenerator
    - layer: meta-comms
      worker: EmailDispatcher

  compute:
    replicas: { min: 2, max: 8 }
    resources:
      cpu: "1"
      memory: "2Gi"
```

---

## 6. Service Self-Registration

Each service implementation declares how to register itself with gRPC.
The assembly runtime never needs to know the layer name or proto module.

```python
# layers/meta-marine/packages/python/ais_service/src/meta_marine/ais_service.py
from fab.runtime.service import FabService
from marine.v1 import ais_service_pb2_grpc

class AISServiceImpl(AISServiceServicer, FabService):
    @classmethod
    def register(cls, server):
        ais_service_pb2_grpc.add_AISServiceServicer_to_server(cls(), server)

    # ... service method implementations
```

`fab build` writes the entry point declaration into the layer's `pyproject.toml`:

```toml
# layers/meta-marine/packages/python/ais_service/pyproject.toml  — entry points written by fab build
[project.entry-points."fab.services"]
AISService = "meta_marine.ais_service:AISServiceImpl"

[project.entry-points."fab.workers"]
VesselPositionProcessor = "meta_marine.workers:VesselPositionProcessor"
```

---

## 7. The Generated Assembly Entrypoint

`fab build core-services` generates a **generic** entrypoint.
No layer names. No proto imports. Service discovery via entry points.

```python
# gen/assemblies/core_services/main.py  — GENERATED, never hand-edited
from importlib.metadata import entry_points
import grpc
import asyncio

async def serve():
    server = grpc.aio.server()
    for ep in entry_points(group="fab.services"):
        ep.load().register(server)          # each impl registers its own proto binding
    server.add_insecure_port('[::]:50051')
    await server.start()
    await server.wait_for_termination()

asyncio.run(serve())
```

For workers:

```python
# gen/assemblies/background_workers/main.py  — GENERATED
from importlib.metadata import entry_points
from fab.runtime.worker import WorkerApp

app = WorkerApp()
for ep in entry_points(group="fab.workers"):
    app.register(ep.load())                 # each worker declares its own queue in metadata
app.run()
```

**The entrypoint is identical for every assembly** — it is truly generic.
What changes between assemblies is which layer packages are installed (controlled
by the assembly's `pyproject.toml` dependencies). The entry point mechanism
wires them at runtime without the entrypoint knowing their names.

The generated Dockerfile packages all required layer code into one image.
The service implementations never change. Only the installed package set changes.

---

## 8. Deployment Topologies

The same layer code deploys in different topologies by changing assembly YAML only.

### Topology A — Monolith (dev, early stage)

```yaml
# assemblies/monolith.yaml
spec:
  type: hybrid
  services:
    - { layer: meta-marine,   service: AISService }
    - { layer: meta-billing,  service: BillingService }
    - { layer: meta-auth,     service: AuthService }
  handlers:
    - { layer: meta-marine,   worker: VesselPositionProcessor }
    - { layer: meta-billing,  worker: InvoiceGenerator }
    - { layer: meta-comms,    worker: EmailDispatcher }
```

One container. Zero cross-service network calls. Ideal for local dev and low-traffic staging.

### Topology B — Grouped services (mid-scale prod)

```
core-services assembly:     AISService + BillingService + AuthService
background-workers assembly: VesselProcessor + InvoiceGenerator + EmailDispatcher
```

Two containers. Independent scaling of synchronous vs. asynchronous workloads.

### Topology C — Per-layer microservices (high-scale prod)

```
ais-service assembly:           AISService only
billing-service assembly:       BillingService only
auth-service assembly:          AuthService only
marine-workers assembly:        VesselPositionProcessor only
billing-workers assembly:       InvoiceGenerator only
comms-workers assembly:         EmailDispatcher only
```

Six containers. Independent scaling per service. Maximum operational flexibility.

The deployment manifest selects the topology:

```yaml
# deployments/dev.yaml
spec:
  assemblies:
    - name: monolith
      replicas: { min: 1, max: 1 }

# deployments/prod.yaml
spec:
  assemblies:
    - name: core-services
      replicas: { min: 3, max: 20 }
    - name: background-workers
      replicas: { min: 2, max: 10 }
```

---

## 9. In-Process Optimisation

When services are co-located in the same assembly, cross-service calls
within the same process can bypass gRPC entirely:

```yaml
# assemblies/core-services.yaml
spec:
  services:
    - layer: meta-marine
      service: AISService
    - layer: meta-billing
      service: BillingService
  inProcess: true    # AISService calling BillingService uses direct method call,
                     # not a gRPC roundtrip
```

`fab build` generates direct method dispatch for intra-assembly calls
and gRPC dispatch for inter-assembly calls. The service implementation code
never knows the difference — the OSDK handles the routing.

This means you get monolith performance in dev and microservice isolation in prod
from the same service code.

---

## 10. Language Boundaries

Each layer picks its own language. The assembly handles cross-language packaging.

```yaml
# assemblies/core-services.yaml — cross-language assembly
spec:
  type: grpc-server
  services:
    - layer: meta-marine
      service: AISService
      language: python       # Python implementation
    - layer: meta-billing
      service: BillingService
      language: go           # Go implementation
    - layer: meta-auth
      service: AuthService
      language: python       # Python implementation
```

For cross-language assemblies, each service runs as a separate process within the pod,
communicating via localhost gRPC. The container image includes all required runtimes.
The proto contract is the language boundary — it is invisible at the call site.

Single-language assemblies run all services in one process (no inter-process overhead).

---

## 10. Service Discovery

When an app or layer calls `AISService`, it needs to know where it is running.
The OSDK handles this transparently — it reads the assembly routing table
injected by `fab deploy`:

```
# Generated by fab deploy, injected as K8s ConfigMap
ASSEMBLY_CORE_SERVICES_ENDPOINT = core-services.default.svc.cluster.local:50051
ASSEMBLY_BACKGROUND_WORKERS_ENDPOINT = ...
```

Apps and services call:
```python
app.services.ais.get_vessel_position(mmsi="123456789")
# OSDK resolves: AISService is in core-services assembly
# routes to: core-services.default.svc.cluster.local:50051
# (or direct method call if in the same assembly with inProcess: true)
```

Changing the topology (splitting `core-services` into separate assemblies)
updates the routing table. Zero app code changes.

---

## 11. The SBOM — System Bill of Materials

`fab release` generates a signed Software Bill of Materials:

```yaml
# dist/sbom-v1.3.0.yaml  — generated, signed, immutable
apiVersion: fab/v1
kind: SBOM
metadata:
  version: 1.3.0
  git_ref: 179a9f6
  signed_at: 2026-03-15T14:00:00Z

layers:
  - name: meta-elo
    version: 1.0.3
    digest: sha256:abc...
  - name: meta-marine
    version: 1.0.0
    digest: sha256:def...

assemblies:
  - name: core-services
    image: ecr.aws/acme/core-services:1.3.0
    image_digest: sha256:ghi...
    contains:
      - { layer: meta-marine,  service: AISService }
      - { layer: meta-billing, service: BillingService }
  - name: background-workers
    image: ecr.aws/acme/background-workers:1.3.0
    image_digest: sha256:jkl...

ontology:
  name: acme-corp
  version: 1.3.0
  digest: sha256:mno...
```

The SBOM is the "Yocto image manifest" equivalent — the complete, auditable
record of exactly what is deployed. Ops, security, and compliance teams
use this. It is generated automatically on every release.

---

## 12. Repository Structure

```
foundry/
├── assemblies/                     # deployment packaging (FDE configures)
│   ├── monolith.yaml               # all services in one container (dev)
│   ├── core-services.yaml          # grouped gRPC services
│   └── background-workers.yaml     # grouped workers

├── deployments/                    # environment compositions (FDE configures)
│   ├── dev.yaml                    # monolith assembly
│   ├── staging.yaml                # grouped assemblies
│   └── prod.yaml                   # fully split assemblies

├── layers/                         # business logic (layer authors write here)
│   └── meta-marine/
│       ├── layer.yaml              # manifest: type, deps, provides
│       ├── idl/                    # proto contracts (layer-owned)
│       ├── schema/                 # ontology contributions
│       ├── packages/               # all code, organized by language
│       │   └── python/
│       │       ├── ais_service/            # service implementation package
│       │       ├── adapter_marinetraffic/  # adapter package
│       │       └── adapter_vesseltracker/  # adapter package
│       ├── justfile                # task recipes
│       ├── pipelines/              # data ingestion
│       └── infra/                  # layer-scoped Terraform modules

├── gen/
│   ├── justfile                    # generated mod imports for all layer justfiles
│   └── assemblies/                 # generated entrypoints — never hand-edited
│       ├── core_services/
│       │   ├── pyproject.toml
│       │   ├── main.py
│       │   └── Dockerfile
│       └── background_workers/
│           ├── pyproject.toml
│           ├── main.py
│           └── Dockerfile

└── dist/
    └── sbom-v1.3.0.yaml            # generated SBOM per release
```

---

## 13. Design Rules

1. **`fab new layer` scaffolds a `CLAUDE.md`** — the agent context file for every new layer, updated when the layer contract changes
2. **Layer code never imports from another layer directly** — only via OSDK (ontology) or generated gRPC stubs (services)
2. **Assembly YAML determines topology — layer code is topology-agnostic**
3. **Proto is the contract boundary** — the language of a layer is invisible to its consumers
4. **Event schemas are proto-defined** — no ad-hoc JSON blobs on queues
5. **`inProcess` is an optimisation hint** — the service implementation must work correctly over gRPC too
6. **The SBOM is generated on every release** — never manually authored
7. **Cross-language assemblies** run services as separate processes within the pod; single-language assemblies run in one process
8. **Service discovery is injected** — no hardcoded endpoints anywhere in service or app code
