# ELO Hooks

> How layers intercept and extend operations without owning them.
> The hook system is the implementation of P5: extensibility without ownership.
> A layer that cannot modify a type it doesn't own can still react to operations on it.

---

## 1. What Hooks Are

Hooks are the extension mechanism for behavioral modification. When a layer needs to
react to an operation in another layer — validate before it runs, audit after it runs,
update derived state — it declares a hook.

**Hooks vs. Functions:**

| | Trigger | Execution | Can abort? | Purpose |
|---|---|---|---|---|
| **Hook** | Specific operation (action, write) | Synchronous or async depending on type | Yes (`pre-action`) | Intercept, validate, audit |
| **Function** | Object state change | Always async | No | Compute derived properties |

A hook says "when X happens, run Y alongside it."
A function says "whenever object Z's state changes, recompute property P."

---

## 2. Hook Types

### `pre-action`
Fires before an Action executes. Runs in the same transaction. Can abort.

Use for: input validation, precondition checks, permission enforcement.

### `post-action`
Fires after an Action succeeds and commits. Runs immediately after commit.
Cannot abort the action (it has already committed).

Use for: audit logging, triggering downstream notifications, updating cross-layer state.

### `on-object-updated`
Fires after any write to an object type commits to the DB. Always async.

Use for: syncing derived state in another layer, invalidating caches, triggering
external webhooks.

### `on-event`
Fires when a specific domain event is published on the event bus. Always async.

Use for: reacting to events published by other layers.

---

## 3. Schema Declaration

Hooks are declared in the layer's schema directory:

```
layers/meta-marine/
└── schema/
    └── hooks/
        ├── validate_mmsi.yaml          # pre-action hook
        ├── audit_vessel_track.yaml     # post-action hook
        └── sync_port_risk.yaml         # on-object-updated hook
```

### Pre-action hook

```yaml
# layers/meta-marine/schema/hooks/validate_mmsi.yaml
apiVersion: fab/v1
kind: Hook
metadata:
  name: validate_mmsi_format
  layer: meta-marine

spec:
  type: pre-action
  target:
    layer: meta-marine
    action: TrackVessel         # the action to intercept

  implementation:
    language: python
    entrypoint: meta_marine.hooks.vessel:validate_mmsi_format

  on_abort:                     # what the caller sees if the hook aborts
    error_code: INVALID_MMSI
    message: "MMSI must be exactly 9 digits"
```

### Post-action hook

```yaml
# layers/meta-marine/schema/hooks/audit_vessel_track.yaml
apiVersion: fab/v1
kind: Hook
metadata:
  name: audit_vessel_track
  layer: meta-marine

spec:
  type: post-action
  target:
    layer: meta-marine
    action: TrackVessel

  implementation:
    language: python
    entrypoint: meta_marine.hooks.audit:record_vessel_track
```

### On-object-updated hook

```yaml
# layers/meta-marine/schema/hooks/sync_port_risk.yaml
apiVersion: fab/v1
kind: Hook
metadata:
  name: sync_port_risk_on_vessel_update
  layer: meta-marine

spec:
  type: on-object-updated
  target:
    layer: meta-core       # a type owned by a different layer
    object_type: Port
    properties: [sanctions_status]   # only fire if this property changed

  implementation:
    language: python
    entrypoint: meta_marine.hooks.risk:invalidate_vessel_risk_cache
```

### On-event hook

```yaml
# layers/meta-marine/schema/hooks/on_voyage_started.yaml
apiVersion: fab/v1
kind: Hook
metadata:
  name: initialize_voyage_tracking
  layer: meta-marine

spec:
  type: on-event
  target:
    layer: meta-marine
    event: VoyageStarted        # event schema declared in this layer's idl/

  implementation:
    language: python
    entrypoint: meta_marine.hooks.voyage:initialize_tracking
```

---

## 4. Implementation

### Pre-action hook

```python
# layers/meta-marine/packages/python/marine_service/src/meta_marine/hooks/vessel.py
from fab.runtime.hook import PreActionContext, HookAbort

async def validate_mmsi_format(ctx: PreActionContext) -> None:
    """Runs before TrackVessel. Raises HookAbort to cancel the action."""
    mmsi = ctx.action_params.get("mmsi", "")

    if not mmsi.isdigit() or len(mmsi) != 9:
        raise HookAbort(
            error_code="INVALID_MMSI",
            message=f"MMSI '{mmsi}' must be exactly 9 digits",
        )
    # Return normally to allow the action to proceed
```

`PreActionContext` provides:
- `ctx.action_params` — the parameters the caller passed to the action
- `ctx.actor` — the authenticated identity making the call
- `ctx.osdk` — read-only OSDK (pre-action hooks cannot write)

### Post-action hook

```python
from fab.runtime.hook import PostActionContext

async def record_vessel_track(ctx: PostActionContext) -> None:
    """Runs after TrackVessel commits. Cannot abort."""
    # ctx.action_params — what was passed in
    # ctx.action_result — what the action returned
    # ctx.actor — who called the action
    # ctx.osdk — read/write OSDK

    await ctx.osdk.vessel_audit_log.create({
        "vessel_id": ctx.action_params["vessel_id"],
        "action": "track_vessel",
        "actor_id": ctx.actor.id,
        "timestamp": ctx.committed_at,
    })
```

### On-object-updated hook

```python
from fab.runtime.hook import ObjectUpdatedContext

async def invalidate_vessel_risk_cache(ctx: ObjectUpdatedContext) -> None:
    """Fires async after a Port's sanctions_status changes."""
    # ctx.object_id — the Port that was updated
    # ctx.changed_properties — {"sanctions_status": {"old": "clean", "new": "sanctioned"}}
    # ctx.osdk — read/write OSDK

    # Find all vessels whose last port is this port and invalidate their risk scores
    vessels = await ctx.osdk.vessel.filter(last_port_id=ctx.object_id)
    for vessel in vessels:
        await ctx.osdk.vessel.update(vessel.id, {"risk_score": None})
        # This will trigger calculate_risk_score Function to re-run
```

---

## 5. Execution Model

### Synchronous hooks (`pre-action`, `post-action`)

```
Caller invokes TrackVessel(vessel_id, mmsi)
        │
        FAB runtime: collect all pre-action hooks for TrackVessel
        │
        Execute hooks in layer topological order
        │
        ├── meta-core hooks first (if any)
        ├── meta-marine hooks (validate_mmsi_format)
        │       └── raises HookAbort → runtime returns error to caller, no DB write
        │       └── returns normally → continue
        └── your-layer hooks last (if any)
        │
        Execute the action (TrackVessel logic runs, DB write)
        │
        Execute post-action hooks in topological order
        │
        COMMIT
```

Pre-action and post-action hooks run **in the action's transaction**. If a post-action
hook raises an unhandled exception, the entire transaction rolls back — including the
action's writes.

### Asynchronous hooks (`on-object-updated`, `on-event`)

```
DB write commits
        │
        FAB runtime publishes internal update notification
        │
        Hook workers consume asynchronously (out of band)
        │
        Hook runs — failure does NOT roll back the original write
        │
        On failure: retry with exponential backoff, then dead-letter
```

Async hooks are delivered at least once. Hook implementations must be idempotent.

---

## 6. Ordering Across Layers

When multiple layers declare hooks for the same target, they fire in the topological
order of the layer graph — dependencies first:

```
meta-core hooks     (fire first — foundation layer)
meta-auth hooks     (depends on meta-core)
meta-marine hooks   (depends on meta-core, meta-events)
your-app hooks      (depends on everything)
```

Within a single layer, hooks fire in the order they appear in `layer.yaml`'s
provides section (top to bottom).

Order is stable and deterministic. `fab resolve` validates it.

---

## 7. Failure Semantics

| Hook type | Exception in hook | Effect |
|-----------|------------------|--------|
| `pre-action` | `HookAbort` | Action cancelled, error returned to caller |
| `pre-action` | Any other exception | Action cancelled, internal error returned |
| `post-action` | Any exception | Entire action + post-hooks rolled back |
| `on-object-updated` | Any exception | Retried, original write NOT rolled back |
| `on-event` | Any exception | Retried, event NOT un-published |

**The key rule:** synchronous hooks (`pre-action`, `post-action`) are in the causal
chain — they can affect the outcome. Async hooks (`on-object-updated`, `on-event`)
are observers — their failure does not affect what already happened.

---

## 8. Testing

```python
# layers/meta-marine/packages/python/marine_service/tests/test_hooks.py
from fab.testing.hook import HookTestHarness
from meta_marine.hooks.vessel import validate_mmsi_format
import pytest

async def test_valid_mmsi_passes():
    harness = HookTestHarness("pre-action", validate_mmsi_format)
    ctx = harness.build_context(action_params={"mmsi": "123456789", "vessel_id": "v1"})
    await harness.run(ctx)   # should not raise

async def test_invalid_mmsi_aborts():
    harness = HookTestHarness("pre-action", validate_mmsi_format)
    ctx = harness.build_context(action_params={"mmsi": "not-a-number", "vessel_id": "v1"})

    with pytest.raises(HookAbort) as exc_info:
        await harness.run(ctx)

    assert exc_info.value.error_code == "INVALID_MMSI"
```

`HookTestHarness` provides mock context objects matching the hook type.
No action runtime, no DB, no event bus required.

---

## 9. Design Rules

1. **Hooks are declared in schema** — in `schema/hooks/*.yaml`. They are part of the layer's contract, not implementation detail.
2. **Synchronous hooks are in the transaction** — pre-action and post-action hooks share the action's transaction. A post-action exception rolls everything back.
3. **Async hooks are observers** — on-object-updated and on-event hooks cannot affect the operation that triggered them.
4. **Hooks must be idempotent** — async hooks can be delivered more than once.
5. **Pre-action hooks abort via `HookAbort`** — raising any other exception is treated as an internal error (not a validation error).
6. **Topological ordering is guaranteed** — foundation layers' hooks always fire before dependent layers' hooks.
7. **Hooks are not Functions** — use hooks to intercept and validate; use Functions to compute derived properties. A hook that computes state is usually better modeled as a Function.
8. **`fab new hook`** scaffolds the YAML declaration and Python stub.
