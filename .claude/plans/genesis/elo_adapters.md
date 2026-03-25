# ELO Adapters

> How FAB isolates all external dependencies behind swappable facades.
> The adapter system is the implementation of A12: every technology choice must be reversible.
> Swapping a provider is one line in `foundry.yaml`. Zero app or layer code changes.

---

## 1. The Pattern

Every external system in FAB — auth provider, queue, storage, AI model, database — is
accessed through a facade. The facade is an abstract interface. The adapter is the
concrete implementation for a specific provider.

```
app code / service code
        │
        facade interface  (stable contract — never changes)
        │
        ┌───────────────────────────┐
        │   adapter selection       │  ← foundry.yaml: adapters.queue: sqs
        └───────────────────────────┘
        │
        SQSAdapter  /  RabbitMQAdapter  /  KafkaAdapter  /  PubSubAdapter
```

The app never imports `boto3`. The service never imports `pika`. They import the facade.
The runtime injects the active adapter at startup.

---

## 2. Built-in Facade Types

Each facade is owned by the layer that defines the domain:

| Facade | Owner layer | Implementations |
|--------|-------------|-----------------|
| `auth` | `meta-auth` | cognito, auth0, okta |
| `billing` | `meta-billing` | stripe, recurly |
| `email` | `meta-comms` | ses, sendgrid, mailgun |
| `queue` | `meta-events` | sqs, rabbitmq, kafka, pubsub, eventbridge |
| `cache` | `meta-data` | elasticache, redis, memcached |
| `storage` | `meta-storage` | s3, gcs, azure-blob, minio |
| `secrets` | `meta-core` | ssm, vault, gcp-secrets |
| `database` | `meta-data` | rds-postgres, aurora, supabase, cloudsql |
| `compute` | `meta-compute` | kubernetes, ecs, cloud-run |
| `ai` | `meta-ai` | anthropic, openai, bedrock, ollama, local |
| `task-runner` | `meta-devops` | just, invoke, make |
| `pipeline-source` | `meta-elo` | database, rest-api, kafka, sqs, s3, webhook, ai |

---

## 3. Adapter Package Structure

An adapter is a Python package (or Go module / Rust crate) under the layer's `packages/`
directory. Naming convention: `adapter_<provider>`.

```
layers/meta-events/
└── packages/
    └── python/
        ├── events_service/         # the service implementation (not an adapter)
        ├── adapter_sqs/            # Amazon SQS implementation
        │   ├── pyproject.toml
        │   └── src/
        │       └── meta_events_sqs/
        │           ├── __init__.py
        │           └── adapter.py
        ├── adapter_rabbitmq/       # RabbitMQ implementation
        │   └── ...
        └── adapter_kafka/          # Apache Kafka implementation
            └── ...
```

---

## 4. The Facade Interface

Each layer defines its facade as a Python Protocol (structural subtyping):

```python
# layers/meta-events/packages/python/events_service/src/meta_events/facade.py

from typing import Protocol, Callable, Awaitable
from dataclasses import dataclass

@dataclass
class QueueConfig:
    visibility_timeout: int = 30
    max_receive_count: int = 3
    dead_letter_queue: str | None = None

class QueueFacade(Protocol):
    """Facade contract for queue adapters. All implementations must satisfy this."""

    async def publish(
        self,
        queue: str,
        message: bytes,
        attributes: dict[str, str] | None = None,
    ) -> str:
        """Publish a message. Returns the message ID."""
        ...

    async def subscribe(
        self,
        queue: str,
        handler: Callable[[bytes, dict], Awaitable[None]],
        *,
        max_concurrent: int = 10,
    ) -> None:
        """Start consuming messages. Calls handler for each. Runs until cancelled."""
        ...

    async def create_queue(self, name: str, config: QueueConfig) -> None:
        """Provision the queue (called during fab deploy)."""
        ...

    async def delete_queue(self, name: str) -> None:
        """Tear down the queue (called during fab destroy)."""
        ...
```

The Protocol is defined in the owning layer's service package — not in a separate
`interfaces/` package. Any class that implements all methods satisfies the Protocol
(no explicit `implements` declaration needed in Python).

---

## 5. Implementing an Adapter

```python
# layers/meta-events/packages/python/adapter_sqs/src/meta_events_sqs/adapter.py

import json
import boto3
from meta_events.facade import QueueFacade, QueueConfig
from fab.runtime.adapter import AdapterConfig

class SQSAdapter:
    """Amazon SQS implementation of the QueueFacade."""

    def __init__(self, config: AdapterConfig):
        self._client = boto3.client(
            "sqs",
            region_name=config.get("region", "us-east-1"),
            endpoint_url=config.get("endpoint_url"),  # for localstack in dev
        )
        self._account_id = config.require("account_id")
        self._prefix = config.get("queue_prefix", "")

    async def publish(self, queue: str, message: bytes, attributes=None) -> str:
        url = self._queue_url(queue)
        response = self._client.send_message(
            QueueUrl=url,
            MessageBody=message.decode(),
            MessageAttributes={
                k: {"StringValue": v, "DataType": "String"}
                for k, v in (attributes or {}).items()
            },
        )
        return response["MessageId"]

    async def subscribe(self, queue, handler, *, max_concurrent=10) -> None:
        # ... polling loop with concurrent message processing
        pass

    async def create_queue(self, name: str, config: QueueConfig) -> None:
        # ... boto3 create_queue call
        pass

    async def delete_queue(self, name: str) -> None:
        pass

    def _queue_url(self, name: str) -> str:
        return f"https://sqs.us-east-1.amazonaws.com/{self._account_id}/{self._prefix}{name}"
```

The adapter receives an `AdapterConfig` dict populated from `foundry.yaml` at runtime.
Use `config.get(key, default)` for optional config and `config.require(key)` for
required config — `require` raises a clear error at startup if the key is missing.

---

## 6. Registration via Entry Points

Every adapter registers itself via Python entry points. The group name is
`fab.adapters.<facade>`:

```toml
# layers/meta-events/packages/python/adapter_sqs/pyproject.toml
[project]
name = "meta-events-adapter-sqs"
version = "1.0.0"
dependencies = [
    "boto3>=1.34.0",
    "meta-events-service",   # depends on the package that defines QueueFacade
]

[project.entry-points."fab.adapters.queue"]
sqs = "meta_events_sqs.adapter:SQSAdapter"
```

```toml
# layers/meta-events/packages/python/adapter_rabbitmq/pyproject.toml
[project.entry-points."fab.adapters.queue"]
rabbitmq = "meta_events_rabbitmq.adapter:RabbitMQAdapter"
```

`foundry.yaml` selects by entry point name:

```yaml
adapters:
  queue: sqs        # → loads entry point named "sqs" from group "fab.adapters.queue"
```

The runtime does:
```python
from importlib.metadata import entry_points

def load_adapter(facade: str, name: str, config: dict):
    eps = entry_points(group=f"fab.adapters.{facade}")
    cls = eps[name].load()
    return cls(AdapterConfig(config))
```

No layer names, no direct imports. The adapter is discovered at runtime from whatever
packages are installed — same entry point pattern as services.

---

## 7. Configuration

Adapter config comes from `foundry.yaml` and is injected at runtime. Sensitive values
reference the secrets facade — never hardcoded:

```yaml
# foundry.yaml
adapters:
  queue: sqs
  queue_config:
    region: us-east-1
    account_id: "123456789012"
    queue_prefix: "acme-prod-"

  ai: anthropic
  ai_config:
    api_key: ssm://prod/anthropic/api-key   # resolved at startup via secrets adapter
    default_model: claude-sonnet-4-6
    max_tokens: 4096

environments:
  dev:
    adapters:
      queue: localstack-sqs
      queue_config:
        endpoint_url: http://localhost:4566
        region: us-east-1
        account_id: "000000000000"
```

Config keys are adapter-specific. The facade interface is stable; the config schema
is documented per adapter in the adapter package's README.

---

## 8. Writing a New Adapter

Step-by-step for adding a new `queue` adapter for Apache Pulsar:

```bash
# 1. Scaffold the adapter package
fab new adapter meta-events pulsar --facade queue
# → creates layers/meta-events/packages/python/adapter_pulsar/
# → pyproject.toml with correct entry point group
# → src/meta_events_pulsar/adapter.py with QueueFacade stub

# 2. Implement the adapter
# Edit layers/meta-events/packages/python/adapter_pulsar/src/meta_events_pulsar/adapter.py
# Implement all methods defined in QueueFacade

# 3. Register in layer.yaml
# fab new adapter handles this — adds to layer.yaml provides.adapters

# 4. Test
uv run pytest layers/meta-events/packages/python/adapter_pulsar/tests/

# 5. Activate in foundry.yaml
# adapters:
#   queue: pulsar
```

---

## 9. Testing Adapters

Each adapter should have two test modes:

### Unit tests — mock the provider client

```python
# adapter_sqs/tests/test_sqs_adapter.py
from unittest.mock import MagicMock, patch
from meta_events_sqs.adapter import SQSAdapter
from fab.runtime.adapter import AdapterConfig

def test_publish_formats_message_correctly():
    with patch("boto3.client") as mock_boto:
        mock_client = MagicMock()
        mock_boto.return_value = mock_client
        mock_client.send_message.return_value = {"MessageId": "abc-123"}

        adapter = SQSAdapter(AdapterConfig({
            "region": "us-east-1",
            "account_id": "123456789012",
        }))
        msg_id = await adapter.publish("orders", b"hello", {"type": "OrderPlaced"})

        assert msg_id == "abc-123"
        mock_client.send_message.assert_called_once()
```

### Integration tests — real provider (LocalStack or actual service)

```python
# adapter_sqs/tests/test_sqs_integration.py
import pytest
from fab.testing.adapter import AdapterIntegrationHarness

@pytest.mark.integration
async def test_publish_and_subscribe_roundtrip():
    harness = AdapterIntegrationHarness("queue", "localstack-sqs", config={
        "endpoint_url": "http://localhost:4566",
        "region": "us-east-1",
        "account_id": "000000000000",
    })
    async with harness as adapter:
        received = []
        await adapter.create_queue("test-queue", QueueConfig())
        await adapter.subscribe("test-queue", lambda msg, _: received.append(msg))
        await adapter.publish("test-queue", b"test-message")
        await asyncio.sleep(0.5)
        assert received == [b"test-message"]
```

Mark integration tests with `@pytest.mark.integration` — CI runs them against
LocalStack. Unit tests run without any external services.

---

## 10. The Mock Adapter

Every facade has a `mock` adapter for testing:

```yaml
# foundry.yaml (test environment)
environments:
  test:
    adapters:
      queue: mock
      storage: mock
      secrets: mock
```

The mock adapter records all calls and lets tests assert on them:

```python
from fab.testing import get_mock_adapter

queue = get_mock_adapter("queue")
assert queue.published("orders") == [b"expected-message"]
assert queue.subscriptions == ["orders"]
```

App and service tests should always use `mock` adapters — never real providers.
Only adapter integration tests use real providers.

---

## 11. Design Rules

1. **Adapters implement a Protocol** — no base class, no registration decorator. Structural subtyping.
2. **Entry point name = `foundry.yaml` selection key** — `sqs` in entry points → `adapters.queue: sqs` in config
3. **Config is injected via `AdapterConfig`** — no global state, no env var reads in adapter code. The runtime reads env vars and passes them as config.
4. **Secrets are resolved before injection** — `ssm://...` references are resolved by the secrets facade before the adapter sees them. Adapters never call the secrets adapter directly.
5. **Mock adapter always available** — every facade has a mock. Tests never use real providers.
6. **Adapters are layer packages** — they live in `layers/<owner>/packages/python/adapter_<name>/` and are workspace members like any other package.
7. **`fab new adapter`** scaffolds the package, stub implementation, entry point, and test harness.
