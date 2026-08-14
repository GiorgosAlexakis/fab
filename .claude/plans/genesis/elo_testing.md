# ELO Testing Strategy

> How to verify that layers, services, adapters, functions, and assemblies work correctly.
> Testing in FAB is layered — each primitive has its own harness, and integration tests
> compose them. No single test touches everything; each layer of the stack has its own suite.

---

## 1. Testing Layers

```
┌─────────────────────────────────────────────────────────────────┐
│ Assembly integration tests  (full stack, LocalStack, real DB)   │
├─────────────────────────────────────────────────────────────────┤
│ Schema compatibility tests  (CI — breaking change detection)    │
├─────────────────────────────────────────────────────────────────┤
│ Adapter integration tests   (real provider or LocalStack)       │
├─────────────────────────────────────────────────────────────────┤
│ Service unit tests          (mock OSDK, mock adapters)          │
│ Function unit tests         (FunctionTestHarness)               │
│ Hook unit tests             (HookTestHarness)                   │
├─────────────────────────────────────────────────────────────────┤
│ Schema validation           (fab schema validate — always)      │
└─────────────────────────────────────────────────────────────────┘
```

Each layer above can run independently. CI runs them bottom-up — a schema validation failure blocks everything else.

---

## 2. Schema Validation

`fab schema validate` catches structural errors before any code runs:

```bash
fab schema validate

# Checks:
# - No undefined object type references
# - No link cycles
# - Every function_wins property has exactly one owning function
# - No function chain cycles (fab resolve)
# - Cascade delete rules are consistent
# - Access policy role references exist in meta-auth schema
# - Hook targets resolve to existing layers/actions/object types
```

This is not optional. `fab schema validate` runs as a pre-commit hook and as the first CI step. A schema error is treated like a compilation error — it blocks everything.

---

## 3. Service Unit Tests

Services are the business logic layer. They receive the OSDK as a constructor argument, which makes them straightforward to unit test with a mock OSDK.

```python
# layers/meta-marine/packages/python/marine_service/tests/test_vessel_service.py
import pytest
from fab.testing import MockOSDK
from meta_marine.service import VesselService

@pytest.fixture
def osdk():
    mock = MockOSDK()
    mock.vessel.seed([
        {"id": "v1", "mmsi": "123456789", "flag_state": "NO", "risk_score": 12.0},
        {"id": "v2", "mmsi": "987654321", "flag_state": "IR", "risk_score": 87.5},
    ])
    return mock

async def test_high_risk_vessels_returns_only_above_threshold(osdk):
    service = VesselService(osdk)
    results = await service.get_high_risk_vessels(threshold=80.0)

    assert len(results) == 1
    assert results[0].id == "v2"

async def test_track_vessel_raises_on_invalid_mmsi(osdk):
    service = VesselService(osdk)

    with pytest.raises(ValueError, match="MMSI"):
        await service.track_vessel(mmsi="not-valid", vessel_id="v1")
```

`MockOSDK` provides:
- `mock.vessel.seed(rows)` — populate the mock store with test data
- `mock.vessel.get(id)` — returns seeded data
- `mock.vessel.filter(...)` — filters over seeded data in memory
- `mock.vessel.written` — list of all writes made during the test
- `mock.actions.called` — list of all action invocations

No database, no network, no adapters. Service tests run in milliseconds.

---

## 4. Function Unit Tests

Functions have declared I/O — `FunctionTestHarness` enforces it. See `elo_functions.md §9` for the full pattern. Key points:

```python
from fab.testing.function import FunctionTestHarness
from meta_marine_functions.risk import CalculateRiskScore

async def test_sanctioned_port_produces_high_risk():
    harness = FunctionTestHarness(CalculateRiskScore)
    vessel = harness.mock_object("Vessel", {"flag_state": "IR", ...})
    port = harness.mock_object("Port", {"sanctions_status": "sanctioned"})
    harness.mock_link(vessel, "vessel_last_port", port)

    result = await harness.run(object_id=vessel.id)

    assert result["risk_score"] > 80
    harness.assert_output_complete()   # verifies all declared output properties are present
```

`harness.assert_output_complete()` checks that the returned dict contains every property declared in `output.writes`. Missing outputs are a runtime bug caught in tests.

---

## 5. Hook Unit Tests

Hooks have typed contexts — `HookTestHarness` provides them. See `elo_hooks.md §8` for the full pattern. Key points:

```python
from fab.testing.hook import HookTestHarness
from meta_marine.hooks.vessel import validate_mmsi_format

async def test_non_digit_mmsi_produces_correct_error_code():
    harness = HookTestHarness("pre-action", validate_mmsi_format)
    ctx = harness.build_context(action_params={"mmsi": "ABC123456", "vessel_id": "v1"})

    with pytest.raises(HookAbort) as exc:
        await harness.run(ctx)

    assert exc.value.error_code == "INVALID_MMSI"
```

No action runtime, no database, no event bus. Hook tests are pure function tests.

---

## 6. Adapter Tests

Each adapter has two test modes. See `elo_adapters.md §9` for the full pattern.

### Unit tests (no external services)

```python
# adapter_sqs/tests/test_sqs_unit.py
from unittest.mock import MagicMock, patch
from meta_events_sqs.adapter import SQSAdapter

def test_publish_calls_send_message_with_correct_queue_url():
    with patch("boto3.client") as mock_boto:
        mock_client = MagicMock()
        mock_boto.return_value = mock_client
        mock_client.send_message.return_value = {"MessageId": "msg-1"}

        adapter = SQSAdapter(AdapterConfig({
            "region": "us-east-1",
            "account_id": "123456789012",
            "queue_prefix": "acme-",
        }))
        await adapter.publish("orders", b"payload")

        call_args = mock_client.send_message.call_args
        assert "acme-orders" in call_args.kwargs["QueueUrl"]
```

### Integration tests (LocalStack or real service)

```bash
# Start LocalStack for integration tests
docker compose up localstack -d

# Run integration tests
uv run pytest layers/meta-events/packages/python/adapter_sqs/tests/ -m integration
```

```python
# adapter_sqs/tests/test_sqs_integration.py
import pytest
from fab.testing.adapter import AdapterIntegrationHarness

@pytest.mark.integration
async def test_publish_subscribe_roundtrip():
    harness = AdapterIntegrationHarness("queue", "localstack-sqs", config={
        "endpoint_url": "http://localhost:4566",
        "region": "us-east-1",
        "account_id": "000000000000",
    })
    async with harness as adapter:
        received = []
        await adapter.create_queue("test-q", QueueConfig())
        await adapter.subscribe("test-q", lambda msg, _: received.append(msg))
        await adapter.publish("test-q", b"hello")
        await asyncio.sleep(0.3)
        assert received == [b"hello"]
```

Integration tests are marked `@pytest.mark.integration` and only run in CI environments with LocalStack available. They never run as part of the default `uv run pytest` invocation.

---

## 7. Schema Compatibility Tests

Schema changes can be breaking — removing a property, changing a type, making a nullable field required. FAB detects these automatically in CI.

```bash
# Run as part of CI after a schema change
fab schema compat --base main --head HEAD

# Output:
# BREAKING: Customer.tier — type changed from enum to string
#   affected apps: my-app (pinned to 1.3.0), admin-panel (pinned to 1.3.0)
# SAFE: Customer.risk_label — new optional property (additive)
# SAFE: Vessel.imo_number — new required property with default (safe migration)
```

`fab schema compat` compares two ontology snapshots and classifies each change:

| Change | Classification |
|--------|---------------|
| Add optional property | Safe |
| Add required property with default | Safe |
| Add new object type | Safe |
| Add new action type | Safe |
| Remove property | Breaking |
| Change property type | Breaking |
| Make optional property required | Breaking |
| Remove object type | Breaking |
| Change link cardinality | Breaking |

A breaking change blocks the PR merge unless:
- All affected apps have been updated and regenerated (`fab osdk generate --all`)
- A migration path is documented in the schema YAML (`migration:` key)
- The PR author has explicitly tagged it `breaking-change` and a schema reviewer has approved

---

## 8. Assembly Integration Tests

Assembly integration tests verify that a group of services and workers behave correctly together. They run against a real database and real adapters (or LocalStack equivalents).

```yaml
# assemblies/test-marine.yaml
spec:
  type: test
  target: assemblies/marine-api.yaml   # the assembly under test

  services:
    - layer: meta-marine
      service: VesselService
    - layer: meta-marine
      service: VoyageService

  workers:
    - layer: meta-marine
      function: calculate_risk_score

  fixtures:
    - path: tests/fixtures/vessels.yaml
    - path: tests/fixtures/ports.yaml
```

```python
# assemblies/tests/test_marine_assembly.py
import pytest
from fab.testing.assembly import AssemblyTestHarness

@pytest.mark.assembly
async def test_vessel_risk_score_updates_after_port_sanction_change():
    async with AssemblyTestHarness("assemblies/test-marine.yaml") as harness:
        # Seed test data
        vessel_id = await harness.osdk.vessel.create({
            "mmsi": "123456789",
            "flag_state": "NO",
            "last_port_id": "port-rotterdam",
        })

        # Invoke an action
        await harness.osdk.actions.track_vessel(vessel_id=vessel_id)

        # Wait for async function to run
        await harness.wait_for_function("calculate_risk_score", object_id=vessel_id)

        vessel = await harness.osdk.vessel.get(vessel_id)
        assert vessel.risk_score is not None
        assert vessel.risk_score < 50   # Rotterdam is not sanctioned
```

`AssemblyTestHarness`:
- Starts real services and workers from the assembly YAML
- Connects to a test database (isolated per test run)
- Provides `harness.wait_for_function(name, object_id)` to synchronize on async function completion
- Tears down and rolls back after each test

Assembly tests are slow (seconds, not milliseconds). Run them in CI but not in the local `uv run pytest` fast loop.

---

## 9. Test Data Management

### Fixtures

Static test data lives in `tests/fixtures/*.yaml`. The harness loads them before each test run.

```yaml
# tests/fixtures/vessels.yaml
- kind: Vessel
  id: vessel-maersk-sealand
  mmsi: "636017829"
  flag_state: LR
  current_route: Atlantic Westbound
  last_port_id: port-rotterdam

- kind: Port
  id: port-rotterdam
  country_code: NL
  sanctions_status: clean
```

### Isolation

Each assembly test run gets its own database schema:

```python
@pytest.fixture(scope="function", autouse=True)
async def isolated_db():
    schema_name = f"test_{uuid4().hex[:8]}"
    await create_schema(schema_name)
    yield schema_name
    await drop_schema(schema_name)
```

Tests never share state. There is no "test database cleanup" step — each test owns its schema and drops it when done.

### Seeding for local development

```bash
# Seed the local dev database with realistic test data
fab task seed --env dev --fixture tests/fixtures/

# Reset and reseed
fab task reset --env dev
fab task seed --env dev
```

Seed data is distinct from fixture data: fixtures are minimal and deterministic (used in tests); seed data is larger and realistic (used for local development).

---

## 10. Ontology Upgrade Validation

Before promoting a new ontology version to production, validate that the upgrade is safe:

```bash
# Full upgrade validation workflow
fab schema compile                         # compile current schema → snapshot 1.4.0
fab schema compat --base 1.3.0 --head 1.4.0  # check for breaking changes
fab osdk generate --all --snapshot 1.4.0   # regenerate all app OSDKs
uv run pytest -m "not integration"         # run all unit tests against new OSDK
fab schema migrate --from 1.3.0 --to 1.4.0 --dry-run  # preview DB migrations
fab schema migrate --from 1.3.0 --to 1.4.0            # apply to staging
fab task run meta-core::validate_data_integrity        # post-migration assertions
```

`validate_data_integrity` is an `on_demand` Function that checks invariants:
- No orphaned links (link target IDs exist)
- No null values in required fields
- Enum values are within declared sets
- Cascade delete rules are consistent

If any assertion fails, the migration is rolled back automatically.

---

## 11. CI Pipeline

```yaml
# .github/workflows/ci.yaml (conceptual)
jobs:
  schema:
    - fab schema validate
    - fab schema compat --base ${{ github.base_ref }} --head HEAD

  unit-tests:
    needs: schema
    - uv run pytest layers/ apps/ -m "not integration and not assembly"

  adapter-tests:
    needs: schema
    services: [localstack]
    - uv run pytest layers/ -m integration

  assembly-tests:
    needs: [unit-tests, adapter-tests]
    services: [postgres, localstack]
    - uv run pytest assemblies/tests/ -m assembly

  osdk-check:
    needs: schema
    - fab osdk status --assert-current
```

The pipeline is ordered: schema gates unit tests; unit tests gate assembly tests. A single schema error kills the entire pipeline early.

---

## 12. Running Tests Locally

```bash
# Fast loop — unit tests only (no external services)
uv run pytest layers/ apps/ -m "not integration and not assembly"

# With adapter integration tests (requires Docker)
docker compose up -d localstack
uv run pytest layers/ -m integration

# Full suite (requires Docker + Postgres)
docker compose up -d
uv run pytest

# Single layer
uv run pytest layers/meta-marine/

# Single test file
uv run pytest layers/meta-marine/packages/python/marine_service/tests/test_vessel_service.py -v

# Schema + OSDK check (before pushing)
fab schema validate && fab osdk status --assert-current
```

---

## 13. Design Rules

1. **Schema validation runs always** — it is a pre-commit hook and the first CI step; it is never skipped
2. **Unit tests use mock adapters** — service, function, and hook tests never hit real providers
3. **Integration tests use LocalStack** — adapter integration tests use real SQS/S3/etc. protocols via LocalStack, not the cloud
4. **Assembly tests are isolated** — each test run gets its own DB schema; no shared state
5. **Breaking schema changes block PRs** — `fab schema compat` is a hard gate; intentional breaking changes require explicit approval
6. **Generated OSDK is committed** — stale OSDK is caught by `fab osdk status --assert-current` in CI
7. **Seed data ≠ fixture data** — fixtures are deterministic and minimal; seed data is realistic and for local dev
8. **Upgrade validation before production** — `fab schema migrate --dry-run` + integrity function before applying migrations to prod
