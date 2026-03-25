# ELO Pipelines

> How data gets into the ontology.
> Inspired by Palantir's Funnel pipeline system.
> The ontology is only a digital twin if it reflects real-world state.

---

## 1. Why Pipelines Are a First-Class Concern

An ontology without data ingestion is a schema — not an operational layer.
Object instances must come from somewhere: existing databases, external APIs,
event streams, file imports, webhooks. Without a pipeline primitive, teams
either populate objects manually (unscalable) or write custom sync scripts
that bypass the ontology's type system and edit history.

Pipelines are the bridge between existing data systems and the ontology.

---

## 2. The Pipeline Primitive

```yaml
# pipelines/sync_customers.yaml
apiVersion: fab/v1
kind: Pipeline
metadata:
  name: sync_customers
  layer: app

spec:
  trigger:
    schedule: "*/15 * * * *"      # or: on_event | on_demand | streaming
  source:
    adapter: database              # or: rest-api | s3 | kafka | webhook | grpc
    connection: crm_db             # references secrets adapter
    query: "SELECT id, email, tier, created_at FROM crm.customers"
  target:
    objectType: Customer
    mapping:
      id:         source.id
      email:      source.email
      tier:       source.tier
      created_at: source.created_at
  mode: upsert                     # or: replace | append | delete-missing
  editPolicy: respect              # respect user edits — don't overwrite them
```

The pipeline declares its source, its target object type, and a property mapping.
The runtime handles the sync, edit merging, conflict resolution, and audit trail.
No custom ETL code required for standard sources.

---

## 3. Trigger Modes

| Mode | When it runs | Use case |
|------|-------------|----------|
| `schedule` | Cron expression | Nightly CRM sync, daily report ingestion |
| `on_event` | Object created/updated/deleted | Keep derived objects in sync |
| `on_demand` | `fab pipeline run <name>` | One-time import, manual re-sync |
| `streaming` | Continuous, Kafka/SQS consumer | Real-time event sourcing |

Streaming pipelines are the most operationally complex — they require a persistent
consumer process, exactly-once delivery semantics, and checkpoint management.
They are declared the same way; the runtime selects the appropriate execution mode.

---

## 4. Source Adapters

Source adapters follow the same facade pattern as all FAB adapters.
`foundry.yaml` selects the implementation; the pipeline declares the type.

```yaml
# foundry.yaml
adapters:
  pipeline-sources:
    crm_db:
      type: database
      connection: ssm://prod/crm/db-url
    stripe_events:
      type: kafka
      brokers: ssm://prod/kafka/brokers
      topic: stripe.events
    salesforce:
      type: rest-api
      base_url: ssm://prod/salesforce/url
      auth: ssm://prod/salesforce/oauth
```

Built-in source types:

| Source type | Description |
|------------|-------------|
| `database` | SQL query against any JDBC/ODBC-compatible DB |
| `rest-api` | Paginated REST API with configurable auth |
| `kafka` | Kafka consumer (exactly-once via checkpoints) |
| `sqs` | SQS queue consumer |
| `s3` | S3 file ingestion (CSV, JSON, Parquet) |
| `grpc` | gRPC streaming source |
| `webhook` | HTTP webhook receiver |
| `ontology` | Another object type in the same ontology (derived objects) |
| `ai` | AI model output via `meta-ai` adapter (e.g. Claude, OpenAI, Bedrock) |

---

## 5. Edit Policy — User Edits Survive Re-Syncs

The most critical pipeline design decision. When a pipeline re-runs and the
source has updated data, what happens to properties a user has manually corrected?

```yaml
# schema/objects/customer.yaml
spec:
  properties:
    - name: email
      type: string
      editPolicy: user_wins       # user correction survives pipeline re-sync
    - name: tier
      type: enum
      editPolicy: pipeline_wins   # pipeline is always authoritative
    - name: internal_notes
      type: string
      editPolicy: user_only       # pipeline never writes this property
    - name: risk_score
      type: float
      editPolicy: function_wins   # owned by a Function, not pipeline or user
```

The pipeline runtime merges source data with the edit store per property,
applying the declared `editPolicy`. Without this, every pipeline re-run
destroys human corrections — teams stop trusting the ontology.

---

## 6. Derived Object Pipelines

A pipeline's source can be another object type in the ontology.
This creates derived objects — object types computed from other objects.

```yaml
# pipelines/derive_high_value_customers.yaml
apiVersion: fab/v1
kind: Pipeline
metadata:
  name: derive_high_value_customers
spec:
  trigger:
    on_event:
      - object_type: Order
        event: created
      - object_type: Order
        event: updated
  source:
    adapter: ontology
    query: |
      SELECT c.id, SUM(o.amount) as lifetime_value
      FROM Customer c
      JOIN CustomerOrders co ON co.customer_id = c.id
      JOIN Order o ON o.id = co.order_id
      GROUP BY c.id
      HAVING SUM(o.amount) > 10000
  target:
    objectType: HighValueCustomer
    mapping:
      id:              source.id
      lifetime_value:  source.lifetime_value
  mode: replace
```

Derived pipelines run inside the ontology's permission model —
the pipeline can only read object types it has declared access to.

---

## 7. Streaming Pipelines

Streaming pipelines are long-running consumers. They maintain a checkpoint
so they can resume after restart without reprocessing or losing events.

```yaml
# pipelines/ingest_payment_events.yaml
apiVersion: fab/v1
kind: Pipeline
metadata:
  name: ingest_payment_events
spec:
  trigger:
    streaming:
      source: stripe_events       # references foundry.yaml adapter
      consumer_group: elo-payments
      checkpoint: registry        # store checkpoint in ontology registry DB
      delivery: exactly_once
  source:
    adapter: kafka
    deserializer: json
  target:
    objectType: Payment
    mapping:
      id:         source.id
      amount:     source.amount_cents
      status:     source.status
      created_at: source.created
  mode: upsert
```

The streaming pipeline runtime manages:
- Consumer group offset tracking
- Checkpoint persistence (in the ontology registry DB)
- Exactly-once delivery via two-phase commit
- Backpressure when the object store cannot keep up
- Dead-letter queue for malformed events

---

## 8. Pipeline Observability

Pipelines are fully instrumented via FC1 (OpenTelemetry). Every pipeline run
emits structured events and spans automatically.

```
pipeline: sync_customers
  run_id: 550e8400-e29b-41d4-a716-446655440000
  trigger: schedule (*/15 * * * *)
  started_at: 2026-03-15T14:00:00Z
  duration_ms: 3420
  records:
    fetched:   12450
    upserted:  847
    skipped:   11603   (no change)
    conflicts: 12      (edit policy applied)
    errors:    0
  spans:
    source_query:    1200ms
    edit_merge:       890ms
    object_store:    1330ms
```

Standard dashboard metrics per pipeline:
- Records processed per run
- Edit policy conflict rate (high rate = source and users disagree frequently)
- Run duration trend (degradation detection)
- Error rate and dead-letter queue depth

---

## 9. Pipeline-to-Function Relationship

Pipelines ingest raw data into the ontology.
Functions compute derived state from ontology data.
They compose:

```
External CRM DB
    │
    sync_customers pipeline    (ingests Customer objects)
    │
    ▼
Customer objects in ontology
    │
    recalculate_risk function  (triggered on_object_updated: Customer)
    │
    ▼
Customer.risk_score updated
    │
    derive_high_value_customers pipeline  (triggered on_event: Customer.updated)
    │
    ▼
HighValueCustomer derived objects
```

Each stage is declared in schema, versioned with the ontology, and
observable through the same OTEL pipeline.

---

## 10. Repository Structure

```
pipelines/
├── sync_customers.yaml
├── sync_orders.yaml
├── ingest_payment_events.yaml      # streaming
└── derive_high_value_customers.yaml

schema/
└── functions/
    ├── recalculate_risk.yaml
    └── compute_lifetime_value.yaml
```

Pipelines live at the root alongside `apps/` and `schema/`.
They are first-class citizens — not scripts in `tools/` or buried in `infra/`.

---

## 11. FDE Workflow

```bash
# Scaffold a new pipeline
fab new pipeline sync_customers --source database --target Customer

# Run a pipeline manually (on-demand)
fab pipeline run sync_customers

# Check pipeline status
fab pipeline status
# sync_customers           last: 2m ago   records: 847   errors: 0
# ingest_payment_events    streaming      lag: 120ms     errors: 0
# derive_high_value        last: 2m ago   records: 23    errors: 0

# Inspect a pipeline run
fab pipeline show sync_customers --run-id 550e8400

# Replay a streaming pipeline from a checkpoint
fab pipeline replay ingest_payment_events --from "2026-03-14T00:00:00Z"
```

---

## 12. Design Rules

1. Pipelines declare source, target, and mapping — they do not contain transformation logic
2. Complex transformations belong in Functions, not pipeline mapping expressions
3. Edit policy is declared per-property in the schema, not per-pipeline
4. Streaming pipelines always use the checkpoint mechanism — no stateless consumers
5. Pipeline source adapters follow the facade pattern — no direct connection strings in pipeline YAML
6. Every pipeline run is observable via OTEL — no silent failures
7. Dead-letter queues are mandatory for streaming pipelines — malformed events never block the stream
