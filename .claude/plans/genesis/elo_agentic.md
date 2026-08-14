# ELO Agentic Design

> FAB is designed for human-agent collaboration from day one.
> Agents are a primary user — not an integration added on top.
> The declarative structure that makes FAB human-readable makes it agent-navigable too.

---

## 1. The Core Principle

In a traditional framework, understanding what a system does requires reading implementation code.
In FAB, the declarations are the system. An agent that reads `layer.yaml`, `app.yaml`,
and `foundry.yaml` understands the full contract without reading a single line of Python or Go.

```
Human reads:   layer.yaml  →  understands what the layer provides
Agent reads:   layer.yaml  →  understands what the layer provides

Human runs:    fab build    →  reads human-formatted error
Agent runs:    fab build --json  →  reads structured error, applies fix
```

This is not coincidence — it is a design constraint applied to every interface FAB exposes.

---

## 2. The MCP Server

`meta-elo` ships an MCP server. Every FAB project exposes its full state
to agents via the Model Context Protocol.

```yaml
# foundry.yaml
mcp:
  enabled: true
  port: 7777       # default
```

**What agents can query:**

```
# Ontology queries
mcp: ontology.types()                     → all object types across active layers
mcp: ontology.type("Vessel")              → full Vessel schema, aspects, links, actions
mcp: ontology.providers("Vessel")         → which layers contribute to Vessel
mcp: ontology.breaking_changes("mmsi → vessel_id")  → impact analysis before rename

# Layer graph queries
mcp: layers.graph()                       → full DAG of active layers
mcp: layers.dependents("meta-core")       → what breaks if meta-core is removed
mcp: layers.provides("AISService")        → which layer owns AISService

# Assembly queries
mcp: assemblies.topology()                → which services are in which assembly
mcp: assemblies.contains("AISService")   → which assembly(s) run AISService

# Build state queries
mcp: build.status()                       → what needs rebuilding and why
mcp: build.errors()                       → structured current build errors
```

An agent working on a FAB project can orient itself completely from MCP queries
before touching any file.

---

## 3. Structured CLI Output

Every `fab` command supports `--json` output. Human-readable output is the default;
structured output is always available for agent consumption.

```bash
# Human output (default)
fab build core-services
# ERROR: Missing layer dependency
#   App 'customer-portal' declares resource type 'queue'
#   but layer 'meta-events' is not active in foundry.yaml
#   Fix: fab layer add meta-events

# Structured output (--json)
fab build core-services --json
```

```json
{
  "status": "error",
  "errors": [{
    "code": "MISSING_LAYER_DEPENDENCY",
    "severity": "error",
    "app": "customer-portal",
    "resource_type": "queue",
    "required_layer": "meta-events",
    "fix": {
      "command": "fab layer add meta-events",
      "description": "Add meta-events layer to foundry.yaml"
    }
  }]
}
```

**All structured error objects include a `fix` field** — a concrete command or
file change that resolves the error. Agents act on errors without translation.

```bash
fab schema publish --json
fab resolve --json
fab layer add meta-comms --json
fab task list --json
```

---

## 4. Agent-Driven Scaffolding

`fab new` uses the configured AI adapter to generate idiomatic, context-aware
scaffolds — not static templates filled with `TODO` placeholders.

The agent receives full context before generating:
- Active layer graph (from `foundry.lock`)
- Existing schema types (from registry)
- Naming conventions (derived from existing layers)
- The description provided by the FDE

```bash
fab new layer meta-marine \
  --describe "AIS vessel tracking with MarineTraffic and VesselTracker data providers"

# Agent generates:
# layers/meta-marine/
# ├── CLAUDE.md                           ← contract + extension guide
# ├── layer.yaml                          ← manifest with correct deps inferred
# ├── schema/objects/vessel.yaml          ← Vessel type with common AIS fields
# ├── schema/objects/voyage.yaml
# ├── schema/links/vessel_voyages.yaml
# ├── idl/proto/marine/v1/
# │   ├── ais_service.proto               ← service definition from description
# │   └── events.proto
# ├── packages/python/ais_service/
# │   ├── pyproject.toml
# │   └── src/meta_marine/ais_service.py  ← stub with correct FabService base
# ├── justfile                            ← sync, track, replay recipes
# └── pipelines/sync_vessel_positions.yaml
```

The output is not a generic scaffold — it reflects the specific domain described,
uses the naming conventions of the active project, and wires correctly to the
existing ontology.

```bash
fab new app customer-portal \
  --binds "Customer,Order,Product" \
  --describe "Self-service portal for B2B customers to manage orders and billing"

fab new pipeline sync_customers \
  --source database --target Customer \
  --describe "Nightly sync from Salesforce CRM with user-edit preservation"
```

---

## 5. CLAUDE.md at Every Boundary

Every `fab new` command generates a `CLAUDE.md` alongside the code.
This is the agent's context file — it contains what an agent needs to know
before modifying this component, without reading implementation code.

```markdown
<!-- layers/meta-marine/CLAUDE.md — generated by fab new, maintained by layer author -->

# meta-marine

Provides AIS vessel tracking as a FAB service layer.

## What this layer owns
- Schema: Vessel, Voyage, Port (see schema/objects/)
- Service: AISService (see idl/proto/marine/v1/ais_service.proto)
- Events: VesselPositionUpdated, VoyageStarted
- Adapters: marinetraffic, vesseltracker (facade: ais-source)

## Extension points
- Add a new AIS data provider: implement the ais-source facade in packages/python/adapter_<name>/
- Add vessel properties: attach an aspect in schema/aspects/ — do not modify vessel.yaml directly
- Add a new gRPC method: extend ais_service.proto, implement in packages/python/ais_service/

## Dependencies
- meta-core: uses User (for vessel owner) and Organization (for fleet)
- meta-events: publishes VesselPositionUpdated on the events bus

## Do not
- Import from other layers directly — use the OSDK or gRPC stubs
- Modify Vessel.mmsi — it is the primary key, changing it is a breaking migration
```

The `CLAUDE.md` is updated when the layer's contract changes — it is a living document,
not a one-time generated artifact.

**CLAUDE.md hierarchy:**

```
foundry/CLAUDE.md            ← project-level: what this company's FAB is, active layers
layers/meta-marine/CLAUDE.md ← layer contract + extension guide
apps/customer-portal/CLAUDE.md ← app bindings + what it can and cannot access
assemblies/core-services/CLAUDE.md ← assembly composition and topology rationale
```

---

## 6. `meta-ai` — AI as a First-Class Layer

The AI adapter is not a bolt-on. `meta-ai` is a proper layer with the same
structure as `meta-auth` or `meta-billing`. It is opt-in — not active unless declared.

```yaml
# foundry.yaml
layers:
  - name: meta-ai
    version: ">=1.0.0"
adapters:
  ai: anthropic    # or: openai | bedrock | ollama | local
```

```yaml
# layers/meta-ai/layer.yaml (upstream FAB layer — not modified by FDE)
apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-ai
  origin: upstream
spec:
  dependsOn:
    - name: meta-elo
      version: ">=1.0.0"
  provides:
    adapters:
      - facade: ai
        implementations: [anthropic, openai, bedrock, ollama, local]
    schema:
      functions: []      # FDE defines AI Functions in their own schema/
      pipelines: []      # FDE defines AI pipeline sources in their own pipelines/
```

**AI as a pipeline source:**

```yaml
# pipelines/enrich_vessel_risk.yaml
spec:
  trigger:
    on_event:
      - object_type: Vessel
        event: updated
  source:
    adapter: ai
    model: claude-opus-4-6
    prompt: |
      Vessel: {{vessel.name}}
      Flag: {{vessel.flag_state}}
      Route: {{vessel.current_route}}
      Last port: {{vessel.last_port_of_call}}

      Assess the sanctions and regulatory risk (0-100) with a one-sentence reason.
      Respond as JSON: { "risk_score": int, "risk_reason": string }
  target:
    objectType: Vessel
    mapping:
      risk_score:  source.risk_score
      risk_reason: source.risk_reason
  mode: upsert
  editPolicy: function_wins
```

AI enrichment is observable, retryable, and auditable — same as any other pipeline.
The OTEL pipeline emits spans for every AI call with model, prompt hash, latency, and token count.

**AI as a Function:**

```yaml
# schema/functions/classify_voyage_anomaly.yaml
kind: Function
spec:
  trigger:
    on_object_updated: Voyage
  implementation:
    adapter: ai
    model: claude-haiku-4-5-20251001
    prompt: |
      Voyage {{voyage.id}}: {{voyage.origin}} → {{voyage.destination}}
      Duration: {{voyage.duration_hours}}h  Expected: {{voyage.expected_duration_hours}}h
      AIS gaps: {{voyage.ais_gap_count}}

      Is this voyage anomalous? Respond: { "anomaly": bool, "reason": string, "severity": "low|medium|high" }
  output:
    property: voyage.anomaly_flag
    editPolicy: function_wins
```

**AI as a task:**

```just
# layers/meta-marine/justfile

# Generate a natural-language summary of fleet status
fleet-summary:
    fab task ai-query \
      --model claude-sonnet-4-6 \
      --context "$(fab ontology query 'SELECT * FROM Vessel WHERE status = active')" \
      --prompt "Summarize the current fleet status in 3 bullet points for an ops dashboard"
```

---

## 7. Agentic Development Workflow

The intended workflow for building on FAB with an AI agent:

```
1. Agent reads foundry/CLAUDE.md          → understands the project
2. Agent queries MCP: layers.graph()      → understands active layers
3. Agent queries MCP: ontology.types()    → understands existing schema
4. User describes what they want          → agent generates scaffold
5. Agent runs: fab schema validate --json → catches errors immediately
6. Agent runs: fab resolve --json         → validates layer graph
7. Agent iterates on schema/code          → guided by structured errors
8. Agent runs: fab build --json           → validates full build
9. Human reviews declarations (YAML)      → not implementation code
```

The human's review surface is the declarations — `layer.yaml`, schema YAML,
`app.yaml`. Implementation code is generated and reviewed separately.
The contract is always the primary artifact.

---

## 8. Design Rules

1. **Declarations are the primary interface** — agents navigate `layer.yaml`, not source code.
2. **Every `fab` command supports `--json`** — structured output is not optional.
3. **Every structured error includes a `fix`** — agents act on errors without asking for help.
4. **`CLAUDE.md` is scaffolded at every boundary** — agents always have the right context loaded.
5. **`fab new` uses the AI adapter** — scaffolds are context-aware, not generic templates.
6. **`meta-ai` follows the facade pattern** — swapping `anthropic` → `openai` is one line in `foundry.yaml`. AI pipeline code does not change.
7. **AI enrichment is observable** — every AI call emits OTEL spans. Silent AI calls are a framework bug.
8. **The MCP server is always on in dev** — agents can query project state at any time without special setup.
