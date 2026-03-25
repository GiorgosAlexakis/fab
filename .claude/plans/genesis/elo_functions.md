# ELO Functions

> Triggered computations that keep derived state current.
> Functions are the answer to: "this property should always reflect a calculation, never be manually set."
> Pure computation — declared inputs, declared outputs, no side effects.

---

## 1. What a Function Is

A Function is a schema-declared computation that fires automatically when its trigger
condition is met, reads specific ontology data, and writes back to specific properties
it owns. It is the mechanism behind `editPolicy: function_wins`.

```
Trigger fires (object updated / event / schedule)
        │
        Function reads declared inputs via OSDK
        │
        Function computes result
        │
        Function writes declared output properties
        │
        Edit store records: these writes came from a function, not a user
```

**Functions vs. Actions vs. Pipelines:**

| | Initiated by | Source | Writes to |
|---|---|---|---|
| **Pipeline** | Schedule / event / stream | External system | Object store (bulk upsert) |
| **Action** | User / app code | OSDK call | Object store (transactional) |
| **Function** | System (trigger) | Ontology data | Specific declared properties |

A pipeline syncs `Customer.tier` from CRM. A function computes `Customer.risk_score`
from `Customer.tier` and linked `Order` objects. An action lets the user call
`UpgradeTier(customer_id)`. They compose — a pipeline update triggers a function which
may trigger another function.

---

## 2. Schema Declaration

```yaml
# schema/functions/calculate_risk_score.yaml
apiVersion: fab/v1
kind: Function
metadata:
  name: calculate_risk_score
  layer: meta-marine        # layer that owns this function

spec:
  trigger:
    on_object_updated:
      - object_type: Vessel
        # optional: only trigger if these specific properties changed
        properties: [flag_state, current_route, last_port_of_call]
    schedule: "0 */6 * * *"   # also re-run every 6h regardless of updates

  implementation:
    language: python
    entrypoint: meta_marine.functions.risk:calculate_risk_score
    # or: adapter: ai  (see §5 — AI functions)

  input:
    reads:
      - object_type: Vessel
        properties: [name, flag_state, current_route, last_port_of_call, imo_number]
      - object_type: Port         # can read related objects via links
        via_link: vessel_last_port
        properties: [country_code, sanctions_status]

  output:
    writes:
      - object_type: Vessel
        property: risk_score
        type: float
        editPolicy: function_wins    # this function is the only writer
      - object_type: Vessel
        property: risk_reason
        type: string
        editPolicy: function_wins
```

The `input.reads` declaration is enforced at runtime — the OSDK injected into the
function is scoped to exactly these object types and properties. A function cannot
read what it hasn't declared.

The `output.writes` declaration is the source of truth for `editPolicy: function_wins`
on the corresponding schema properties. `fab schema validate` checks that every
`function_wins` property has exactly one function claiming ownership of it.

---

## 3. Trigger Types

| Trigger | When it fires | Notes |
|---------|--------------|-------|
| `on_object_updated` | After any write to the target object type | Optional `properties` filter: only fire if one of those properties changed |
| `on_action_completed` | After a specific ActionType completes successfully | Fires after commit, not during the action transaction |
| `on_event` | When a domain event is published on the event bus | Declare the event schema in `trigger.on_event.schema` |
| `schedule` | Cron expression | Re-computes even if inputs haven't changed; for time-sensitive derivations |
| `on_demand` | `fab task run <layer>::<function>` | Manual trigger for backfills and debugging |

Multiple triggers can be combined — the Function fires on any of them:

```yaml
trigger:
  on_object_updated:
    - object_type: Customer
  on_action_completed:
    - layer: meta-billing
      action: PayInvoice
  schedule: "0 0 * * *"    # nightly catch-all
```

---

## 4. Implementation

```python
# layers/meta-marine/packages/python/marine_functions/src/meta_marine_functions/risk.py

from fab.runtime.function import FabFunction, FunctionContext
from fab.runtime.osdk import ScopedOSDK   # injected, scoped to declared inputs

class CalculateRiskScore(FabFunction):

    @classmethod
    def register(cls, registry):
        registry.register("calculate_risk_score", cls())

    async def execute(self, ctx: FunctionContext, osdk: ScopedOSDK) -> dict:
        vessel = await osdk.vessel.get(ctx.object_id)
        last_port = await osdk.port.get_via_link(vessel, "vessel_last_port")

        score = self._compute_score(vessel, last_port)
        reason = self._compute_reason(vessel, last_port, score)

        # return value maps to declared output properties
        return {
            "risk_score": score,
            "risk_reason": reason,
        }

    def _compute_score(self, vessel, port) -> float:
        # ... domain logic
        pass
```

The function implementation receives:
- `ctx` — trigger context: object ID, trigger type, which properties changed
- `osdk` — a scoped OSDK instance limited to declared `input.reads`

The return dict must match `output.writes` property names. Any undeclared key raises
an error at runtime.

```toml
# layers/meta-marine/packages/python/marine_functions/pyproject.toml
[project.entry-points."fab.functions"]
calculate_risk_score = "meta_marine_functions.risk:CalculateRiskScore"
```

`fab build` writes this entry point from `layer.yaml` — same pattern as services.

---

## 5. AI Functions

When `implementation.adapter: ai`, the function is implemented by the `meta-ai` adapter
rather than hand-written code. The prompt template is the implementation.

```yaml
# schema/functions/classify_voyage_anomaly.yaml
spec:
  trigger:
    on_object_updated:
      - object_type: Voyage
        properties: [duration_hours, ais_gap_count, route_deviation_km]

  implementation:
    adapter: ai
    model: claude-haiku-4-5-20251001
    prompt: |
      Voyage {{voyage.id}}: {{voyage.origin}} → {{voyage.destination}}
      Duration: {{voyage.duration_hours}}h  Expected: {{voyage.expected_duration_hours}}h
      AIS gaps: {{voyage.ais_gap_count}}  Route deviation: {{voyage.route_deviation_km}}km

      Is this voyage anomalous? Respond as JSON only:
      { "anomaly": bool, "reason": string, "severity": "low|medium|high" }

  input:
    reads:
      - object_type: Voyage
        properties: [id, origin, destination, duration_hours, expected_duration_hours,
                     ais_gap_count, route_deviation_km]

  output:
    writes:
      - object_type: Voyage
        property: anomaly_flag
        type: boolean
        editPolicy: function_wins
      - object_type: Voyage
        property: anomaly_reason
        type: string
        editPolicy: function_wins
      - object_type: Voyage
        property: anomaly_severity
        type: enum
        values: [low, medium, high]
        editPolicy: function_wins
```

AI functions are fully observable — every call emits an OTEL span with model, prompt
hash, latency, and token count. Swapping `claude-haiku-4-5-20251001` → `openai/gpt-4o` is a
one-line change; the function schema and output contract do not change.

---

## 6. Deployment

Functions run as workers in a `worker` assembly. They are not standalone processes —
they share a worker process with pipeline handlers and other functions from the same assembly.

```yaml
# assemblies/background-workers.yaml
spec:
  type: worker

  handlers:
    - layer: meta-marine
      worker: VesselPositionProcessor    # pipeline handler
    - layer: meta-marine
      worker: VoyageAnomalyProcessor     # pipeline handler

  functions:
    - layer: meta-marine
      function: calculate_risk_score
    - layer: meta-marine
      function: classify_voyage_anomaly
```

The worker assembly discovers functions via entry points at startup (same mechanism
as services). The function worker subscribes to the object update stream and event bus,
and dispatches to the appropriate function when a trigger fires.

For `schedule` triggers, the assembly runs an internal cron scheduler. For
`on_demand` triggers, `fab task run` posts a message to the worker's control queue.

---

## 7. The Function Worker Runtime

```
Object write commits to DB
        │
        FAB runtime publishes update event to internal message bus
        │
        Function worker(s) consume the event
        │
        Worker checks: does any Function trigger on this object type + changed properties?
        │
        Yes → dispatch to function with scoped OSDK
        │
        Function returns output dict
        │
        Worker writes output properties via OSDK
        │
        Edit store records: property=risk_score, writer=function:calculate_risk_score, ts=now
```

Function writes are NOT in the same transaction as the triggering write — they are
asynchronous. The triggering write commits first; the function result arrives later.
This prevents cascading transaction failures and allows retry on function error.

If a function fails (exception, timeout, AI error), the worker retries with exponential
backoff. After N retries, the failure is recorded in the edit store and an alert is
emitted via OTEL.

---

## 8. Chaining

Functions can trigger other functions. The write from `calculate_risk_score` can trigger
`notify_high_risk_vessel` if that function declares `on_object_updated: Vessel, properties: [risk_score]`.

```
Vessel updated (flag_state changed)
    → calculate_risk_score fires
    → writes risk_score = 87
        → notify_high_risk_vessel fires (risk_score > threshold)
        → writes notification_sent = true
```

**Cycle detection:** `fab resolve` validates that no function chain forms a cycle.
A function that writes property A cannot trigger a function that writes back to an
upstream property in the same chain.

---

## 9. Testing

```python
# layers/meta-marine/packages/python/marine_functions/tests/test_risk.py
from fab.testing.function import FunctionTestHarness
from meta_marine_functions.risk import CalculateRiskScore

async def test_high_risk_vessel():
    harness = FunctionTestHarness(CalculateRiskScore)

    # Provide mock input data matching declared input.reads
    vessel = harness.mock_object("Vessel", {
        "flag_state": "IR",
        "current_route": "Strait of Hormuz",
        "last_port_of_call": "Bandar Abbas",
    })
    port = harness.mock_object("Port", {
        "country_code": "IR",
        "sanctions_status": "sanctioned",
    })
    harness.mock_link(vessel, "vessel_last_port", port)

    result = await harness.run(object_id=vessel.id)

    assert result["risk_score"] > 80
    assert "sanctions" in result["risk_reason"].lower()
```

`FunctionTestHarness`:
- Injects a mock OSDK scoped to the declared inputs
- Validates that the returned dict matches declared outputs
- Does not require a running database or worker assembly
- Can be run with `uv run pytest` like any Python test

---

## 10. Design Rules

1. **Functions declare all I/O** — the OSDK scope is enforced at runtime; undeclared reads raise an error
2. **`function_wins` requires a function** — `fab schema validate` errors if a `function_wins` property has no owning function
3. **One function owns one property** — two functions cannot both claim `function_wins` on the same property
4. **Functions are async** — they do not run in the triggering transaction; writes from functions appear after the triggering commit
5. **Functions are pure** — they read ontology data, compute, write output properties. No emails, no queues, no external API calls. Use Actions or pipeline event handlers for side effects.
6. **AI functions are functions** — the AI adapter is an implementation detail; the schema contract is identical
7. **Cycles are a build error** — `fab resolve` catches them; runtime cycle detection is not the safety net
