# Session Affinity Filter (`session-affinity-filter`)

**Type:** `session-affinity-filter`

## When to use this filter

Enable this filter when you need **hard session affinity** — guaranteeing that requests with a session identifier are routed exclusively to the endpoint running that session. This is useful for:

- Stateful workloads where session state is maintained on specific endpoints
- Applications requiring strict session stickiness for correctness
- Scenarios where routing to a different endpoint would cause session loss or errors

If you only need **soft affinity** (preference but not requirement), use the `session-affinity-scorer` instead.

## Difference from `session-affinity-scorer`

| Feature | `session-affinity-filter` (this plugin) | `session-affinity-scorer` |
|---------|----------------------------------------|---------------------------|
| **Affinity type** | Hard (enforced) | Soft (weighted preference) |
| **Behavior with session** | Returns ONLY the session endpoint | Gives high score to session endpoint |
| **Behavior without session** | Returns all endpoints | Scores all endpoints equally (0.0) |
| **Fallback if session endpoint unavailable** | Returns all endpoints for scoring | Other scorers determine selection |
| **Use case** | Stateful apps requiring strict stickiness | Load balancing with session preference |

Both plugins work together with the `session-id-producer` to read session identifiers from requests.

## Overview

This filter enforces hard session affinity by narrowing the candidate endpoint list to only the endpoint identified by the session. When a session identifier is present in the request (extracted by `session-id-producer`), the filter:

1. Decodes the session ID to get the target endpoint name
2. If the target endpoint is in the candidate list, returns only that endpoint
3. If the target endpoint is not in the candidate list (e.g., pod scaled down), returns all endpoints
4. If no session ID is present, returns all endpoints (no-op)

This ensures that requests with sessions are routed exclusively to their session endpoint, while new requests (without sessions) can be routed to any endpoint.

## Behavior

- **With session ID matching a candidate**: Returns only that endpoint
- **With session ID not matching any candidate**: Returns all endpoints (allows scoring/selection)
- **Without session ID**: Returns all endpoints (no-op)
- **Single endpoint in list**: Always returns that endpoint (no filtering needed)
- **Invalid/malformed session ID**: Returns all endpoints (treats as no session)

## Config

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `sessionIDProducerName` | `string` | No | `""` (uses default producer) | Name of the session-id-producer to consume session data from |

## Dependencies

- **Requires**: `session-id-producer` to extract and publish session identifiers
- **Reads**: `SessionID` attribute from the request attribute store
- **Works with**: `session-affinity-scorer` for cookie management (scorer sets cookies, filter reads them via producer)

## Configuration Example

### Basic Configuration

```yaml
plugins:
  # Session ID producer extracts session from cookie
  - type: session-id-producer
    name: my-session-producer
    parameters:
      cookieName: llm-d-session

  # Filter enforces hard affinity
  - type: session-affinity-filter
    name: session-filter
    parameters:
      sessionIDProducerName: my-session-producer

  # Scorer sets cookies and provides soft affinity fallback
  - type: session-affinity-scorer
    name: session-scorer
    parameters:
      sessionIDProducerName: my-session-producer
      maxAge: 3600

schedulingProfiles:
  - name: default
    plugins:
      filters:
        - pluginRef: session-filter
      scorers:
        - pluginRef: session-scorer
          weight: 1.0
```

### With Header-Based Sessions

```yaml
plugins:
  - type: session-id-producer
    name: header-session-producer
    parameters:
      headerName: x-session-id

  - type: session-affinity-filter
    name: session-filter
    parameters:
      sessionIDProducerName: header-session-producer

schedulingProfiles:
  - name: default
    plugins:
      filters:
        - pluginRef: session-filter
```

## How It Works

1. **Session Establishment** (first request):
   - Request has no session ID
   - Filter passes all endpoints through
   - Scorers and picker select an endpoint
   - `session-affinity-scorer` sets a cookie with the selected endpoint's identity

2. **Subsequent Requests** (with session):
   - `session-id-producer` extracts session ID from cookie/header
   - Filter decodes session ID to get endpoint name
   - Filter returns only that endpoint (hard affinity)
   - Request is routed to the session endpoint

3. **Session Endpoint Unavailable**:
   - Filter cannot find session endpoint in candidate list
   - Filter returns all endpoints
   - Scorers and picker select a new endpoint
   - `session-affinity-scorer` updates the cookie

## Related Documentation

- [Session Attributes](../../../datalayer/attribute/session/README.md)
- [Session ID Producer](../../../requestcontrol/dataproducer/sessionid/README.md)
- [Session Affinity Scorer](../../scorer/sessionaffinity/README.md)