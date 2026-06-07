# Session ID Producer Plugin

**Type:** `session-id-producer`

Extracts a session identifier from each inference request and tracks which endpoint each session was last routed to. The producer publishes two attributes on the `InferenceRequest` attribute store:

- `SessionID`, when the configured source carries a non-empty value.
- `BoundEndpoint`, when the session is currently bound to an endpoint by a previous request.

The post-schedule `PreRequest` hook records the endpoint chosen by the primary profile. Both writes (`PreRequest`) and reads (`Produce`) refresh the binding's TTL, so an active session keeps its binding alive. Bindings live in an in-memory, size-bounded, time-expiring cache; nothing is written to the response. Affinity-aware scorers and filters consume the attributes via the framework's data dependency mechanism without needing to know how the session was carried on the wire.

The producer is a no-op when the configured source is absent or empty; consumers must treat the missing attributes as "no session preference".

## Parameters

### Source selection (exactly one required)

| Name | Type | Description |
|------|------|-------------|
| `headerName` | `string` | Name of the request header whose value is the session identifier. Comparison is case-insensitive (header names in the request are lowercased). |
| `cookieName` | `string` | Name of the cookie within the standard `Cookie` request header whose value is the session identifier. |

When the producer is auto-instantiated as the default for `SessionIDDataKey` or `BoundEndpointDataKey` (no `parameters` block), `headerName` defaults to `x-session-id`. Configure the producer explicitly to use a different header or a cookie.

### Binding store (optional)

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `lruSize` | `int` | `1024` | Maximum number of concurrent session bindings retained. Must be `> 0` when set. |
| `ttl` | `string` (Go duration) | `30m` | Lifetime of a binding without activity ("30m", "1h"). Both writes (`PreRequest`) and reads (`Produce`) refresh the entry. Must be `> 0` when set. |

## Examples

```yaml
plugins:
  - type: session-id-producer
    parameters:
      headerName: x-session-id
```

```yaml
plugins:
  - type: session-id-producer
    parameters:
      cookieName: llm-d-session
      ttl: 1h
      lruSize: 4096
```

## Related Documentation

- [Session Affinity Filter](../../../scheduling/filter/sessionaffinity/README.md)
- [Session Affinity Scorer](../../../scheduling/scorer/sessionaffinity/README.md)
- [Session Attributes](../../../datalayer/attribute/session/README.md)
