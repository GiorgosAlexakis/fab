# ELO Apps

> How FAB apps declare what they need and get it wired automatically.
> Apps are thin. Layers and adapters do the heavy lifting.

---

## 1. Mental Model

An app in FAB is a deployable service that:
- Binds against a versioned ontology snapshot (typed access to business objects)
- Declares infrastructure resources by semantic type, not provider
- Receives generated clients for both — zero provider imports in app code

```
app.yaml  (what the app needs)
    +
foundry.yaml  (which adapters are active)
        │
        fab build <app>
        │
   ┌────┴──────────────────────┐
   ▼                           ▼
OSDK (ontology + infra)    Terraform + K8s manifests
   │                           │
   app code                    fab deploy <app> --env <env>
```

The app author writes business logic only.
Storage, queues, auth, billing, and deployment plumbing come from the layer system.

---

## 2. Repository Structure

```
apps/
├── customer-portal/
│   ├── app.yaml            # binding declaration — the app's contract
│   ├── src/                # business logic only
│   └── Dockerfile
├── admin-api/
│   ├── app.yaml
│   └── src/
└── data-pipeline/
    ├── app.yaml
    └── src/
```

Apps live at the root, not buried in `packages/`. They are first-class citizens.

---

## 3. `app.yaml` — The App's Contract

```yaml
apiVersion: fab/v1
kind: App
metadata:
  name: customer-portal

spec:
  # Ontology binding — what business objects this app reads/writes
  ontology:
    name: acme-corp
    tag: prod             # resolves to a pinned version at build time

  binds:
    reads:   [Customer, Order, Product]
    writes:  [Customer]
    actions: [PlaceOrder, CancelOrder, UpgradeTier]

  # Infrastructure resources — semantic declarations, no provider specifics
  resources:
    order-events:
      type: queue
      config:
        visibility_timeout: 30
    session-store:
      type: cache
      config:
        ttl: 3600
    uploads:
      type: storage
      config:
        public: false
    app-db:
      type: database        # app-local DB, separate from the ontology object store

  # Compute declaration
  compute:
    replicas: { min: 2, max: 10 }
    autoscale:
      metric: cpu
      target: 70
    resources:
      cpu: "500m"
      memory: "512Mi"
    ports:
      - { name: grpc, port: 50051 }
      - { name: http, port: 8080  }
```

---

## 4. Adapter Selection

`foundry.yaml` holds the provider selection once, for the entire company stack.
Apps never specify providers — they inherit from `foundry.yaml`.

```yaml
# foundry.yaml

adapters:
  # Business domain
  auth:    cognito
  billing: stripe
  email:   ses

  # Infrastructure primitives
  queue:    sqs             # or: rabbitmq | kafka | pubsub
  cache:    elasticache     # or: redis | memcached
  storage:  s3              # or: gcs | azure-blob | minio
  secrets:  ssm             # or: vault | gcp-secrets
  compute:  kubernetes      # or: ecs | cloud-run
  registry: ecr             # or: gcr | acr | dockerhub
  database: rds-postgres    # or: aurora | supabase | cloudsql

environments:
  dev:
    adapters:               # environment-level overrides
      queue:   localstack-sqs
      cache:   local-redis
      storage: minio
```

Swapping `sqs` → `kafka` is a one-line change in `foundry.yaml`.
Zero app code changes required.

---

## 5. What `fab build` Generates

### 5a. Terraform (infrastructure provisioning)

```
infra/apps/customer-portal/
├── queue.tf        # aws_sqs_queue       (because adapters.queue: sqs)
├── cache.tf        # aws_elasticache
├── storage.tf      # aws_s3_bucket
├── database.tf     # aws_db_instance
└── iam.tf          # least-privilege role scoped to this app
```

Example generated `queue.tf`:
```hcl
resource "aws_sqs_queue" "customer_portal_order_events" {
  name                       = "${var.company}-customer-portal-order-events-${var.env}"
  visibility_timeout_seconds = 30
  tags = {
    app     = "customer-portal"
    env     = var.environment
    managed = "fab"
  }
}
```

### 5b. Kubernetes manifests

```
infra/k8s/apps/customer-portal/
├── deployment.yaml   # generated from compute spec
├── service.yaml
├── hpa.yaml          # horizontal pod autoscaler
├── secrets.yaml      # populated from SSM/Vault at deploy time
└── ingress.yaml
```

### 5c. Runtime injection

Connection strings injected as Kubernetes secrets at deploy time.
The app reads standard env vars — it never knows the underlying provider:

```
QUEUE_ORDER_EVENTS_URL     = https://sqs.us-east-1.amazonaws.com/...
CACHE_SESSION_STORE_URL    = redis://acme-corp-session.cache.amazonaws.com:6379
STORAGE_UPLOADS_BUCKET     = acme-corp-customer-portal-uploads
STORAGE_UPLOADS_REGION     = us-east-1
DATABASE_APP_DB_URL        = postgresql://...
```

### 5d. OSDK — the app's only import

The generated SDK wraps both ontology access and infrastructure clients
under one typed interface. The app never imports a provider SDK directly:

```python
# Generated: fab.apps.customer_portal
from fab.apps.customer_portal import App

app = App()

# Ontology — typed, version-pinned to the resolved ontology snapshot
customers = await app.ontology.customer.filter(tier="pro")
order     = await app.ontology.order.get(order_id)
await app.ontology.actions.place_order(customer_id=..., items=[...])

# Infrastructure — typed, provider-agnostic
await app.queues.order_events.publish(OrderCreated(order_id="123"))
cached = await app.cache.session_store.get(session_id)
url    = await app.storage.uploads.put("receipt.pdf", content)
row    = await app.db.app_db.execute("SELECT ...")
```

No `boto3.client('sqs')`. No `redis.Redis()`. No raw SQL connections.
The OSDK is the only interface. Swapping providers touches zero app code.

---

## 6. Layer Requirements

Each resource type requires its backing layer to be active in `foundry.yaml`.
The resolver catches missing layer dependencies at build time:

| Resource type | Required layer   | Adapter facades                               |
|--------------|-----------------|-----------------------------------------------|
| `queue`      | `meta-events`   | sqs, rabbitmq, kafka, pubsub, eventbridge     |
| `cache`      | `meta-data`     | redis, elasticache, memcached                 |
| `storage`    | `meta-storage`  | s3, gcs, azure-blob, minio                    |
| `database`   | `meta-data`     | rds-postgres, aurora, supabase, cloudsql      |
| `secrets`    | `meta-core`     | ssm, vault, gcp-secrets                       |
| `compute`    | `meta-compute`  | kubernetes, ecs, cloud-run                    |
| `registry`   | `meta-compute`  | ecr, gcr, acr, dockerhub                      |
| `cdn`        | `meta-delivery` | cloudfront, fastly, cloudflare                |

If `app.yaml` declares `type: queue` but `meta-events` is not active,
`fab build` fails with a clear error — not a runtime failure.

---

## 7. The Full Wiring Picture

```
foundry.yaml  (adapters.queue: sqs)
app.yaml      (resources.order-events.type: queue)
        │
        fab build customer-portal
        │
   ┌────┴──────────────────────────────┐
   ▼                                   ▼
infra/apps/customer-portal/        infra/k8s/apps/customer-portal/
  queue.tf   → aws_sqs_queue         deployment.yaml
  cache.tf   → aws_elasticache       hpa.yaml
  storage.tf → aws_s3_bucket         secrets.yaml
  iam.tf     → scoped role
        │
        fab deploy customer-portal --env prod
        │
   ┌────┴──────────────────────────────┐
   ▼                                   ▼
terraform apply                    kubectl apply
(provisions SQS, cache, S3, RDS)   (deploys pods, injects env vars)
        │
        ▼
App running in Kubernetes
  QUEUE_ORDER_EVENTS_URL = https://sqs...
  CACHE_SESSION_STORE_URL = redis://...
        │
        ▼
Generated OSDK reads env vars at startup
  app.queues.order_events.publish(...)  →  SQS
  app.cache.session_store.get(...)      →  ElastiCache
  app.storage.uploads.put(...)          →  S3
```

---

## 8. FDE Workflow

```bash
# Scaffold a new app — agent generates idiomatic scaffold from description
fab new app customer-portal \
  --binds "Customer,Order" \
  --describe "Self-service portal for B2B customers to manage orders and billing"
# → creates apps/customer-portal/app.yaml + src/ + CLAUDE.md (agent context file)
# → agent infers resource types from description (queue, cache) and adds them to app.yaml

# Add a resource to an existing app
fab app add-resource customer-portal queue order-events
# → adds resource block to app.yaml

# Build (generate OSDK + Terraform + K8s manifests)
fab build customer-portal

# Deploy to an environment
fab deploy customer-portal --env dev
fab deploy customer-portal --env staging

# Promote between environments
fab promote customer-portal staging prod

# Inspect what an app has access to
fab app show customer-portal
# Ontology:  acme-corp:1.3.0 (prod)
# Reads:     Customer, Order, Product
# Actions:   PlaceOrder, CancelOrder, UpgradeTier
# Resources: order-events (queue→sqs), session-store (cache→elasticache)
#            uploads (storage→s3), app-db (database→rds-postgres)
# Compute:   kubernetes  2-10 replicas  cpu:500m  mem:512Mi
```

---

## 9. Design Rules

**Apps declare needs. Layers own implementations. `foundry.yaml` selects adapters.**

1. Apps never import provider SDKs directly — only the generated OSDK
2. Apps never write Terraform — infra is generated from `app.yaml` + `foundry.yaml`
3. Apps never write Kubernetes manifests — generated from the compute spec
4. Resource type declarations are semantic (`type: queue`) — never provider-specific
5. Missing layer dependencies are caught at `fab build`, not at runtime
6. Swapping any adapter requires zero app code changes

---

## 10. Deployment Manifest — The Image Recipe (from Yocto)

In Yocto, an **image recipe** (`core-image-minimal`) is not just "include these layers".
It composes specific packages from those layers into a named, deployable artifact.

FAB has the equivalent: a `deployment.yaml` that assembles the final deployed state
from layer outputs. This is what `fab deploy` consumes.

```yaml
# deployments/prod.yaml
apiVersion: fab/v1
kind: Deployment
metadata:
  name: prod
spec:
  bsp: aws
  region: us-east-1
  ontology:
    name: acme-corp
    tag: prod
  apps:
    - name: customer-portal
      replicas: { min: 3, max: 20 }
    - name: admin-api
      replicas: { min: 2, max: 5 }
    - name: data-pipeline
      replicas: { min: 1, max: 3 }
  excludes:
    - layer: meta-auth
      adapter: okta       # only ship the cognito adapter in prod
```

```bash
fab deploy --deployment deployments/prod.yaml
# equivalent to Yocto's: bitbake core-image-minimal
```

Multiple deployment manifests exist for different purposes:

```
deployments/
├── dev.yaml        # all apps, local BSP, minimal replicas
├── staging.yaml    # all apps, aws BSP, staging ontology tag
└── prod.yaml       # production apps only, aws BSP, multi-AZ
```

---

## 11. Token-Scoped OSDK (from Palantir)

Palantir generates OSDK packages scoped to exactly what the app declared.
The generated package surface is precisely `app.yaml` `binds` — nothing more.

```python
# customer-portal OSDK — scoped to Customer, Order, PlaceOrder, CancelOrder
from fab.apps.customer_portal import App
app = App()

app.ontology.customer.filter(tier="pro")    # works — declared in binds.reads
app.ontology.invoice.list()                 # AttributeError — not in app.yaml
```

`Invoice` does not exist in the generated package. This is not access control
enforced at runtime — it is **capability scoping at the type system level**.
The app physically cannot reach undeclared types, even with elevated credentials.

**What this enables:**
- Audit what data any app can touch from `app.yaml` alone — no source code reading required
- Accidental over-reach is impossible, not just unauthorised
- The OSDK package size is proportional to what the app needs, not the full ontology

The token scoping applies to actions too. If `app.yaml` does not declare
`actions: [CancelOrder]`, the OSDK has no `cancel_order` method. The app
cannot invoke that action through any code path.
