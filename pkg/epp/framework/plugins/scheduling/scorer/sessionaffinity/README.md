# Session Affinity Scorer

**Type:** `session-affinity-scorer`
**Interfaces:** `scheduling.Scorer`
**Category:** Soft affinity

Gives the endpoint bound to the request's session a maximum score and assigns zero to every other candidate. Other scorers still contribute, so when the bound endpoint is missing or unavailable a different one can win.

## What it does

For each request the scorer reads the `BoundEndpoint` attribute published by a `session-id-producer`:

- **Binding present, endpoint in candidates**: scores that endpoint `1.0`, all others `0.0`.
- **Binding present, endpoint not in candidates**: scores all endpoints `0.0`.
- **Binding absent** (no session, or session not yet bound): scores all endpoints `0.0`.

The scorer never observes responses or sets cookies; persistence of `sessionID -> endpoint` is owned by the `session-id-producer`.

## Inputs consumed

- `BoundEndpoint` attribute, published by `session-id-producer`.

## Configuration

### Parameters

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `sessionIDProducerName` | `string` | No | `""` (uses default producer) | Name of the `session-id-producer` instance whose `BoundEndpoint` this scorer consumes. |

### Example

```yaml
plugins:
  - type: session-id-producer
    parameters:
      headerName: x-session-id

  - type: session-affinity-scorer

schedulingProfiles:
  - name: default
    plugins:
      - pluginRef: session-affinity-scorer
        weight: 1
```

## Difference from session-affinity-filter

Both plugins consume the same `BoundEndpoint` attribute but differ in how strictly they enforce stickiness:

| | `session-affinity-scorer` | `session-affinity-filter` |
|---|---|---|
| **Guarantee** | Soft, bound endpoint gets the highest score, others remain candidates | Hard, only the bound endpoint is returned when present |
| **On endpoint unavailability** | Other scorers determine the winner | Falls back to the full candidate set |

Use the scorer when a best-effort preference is sufficient and other scorers should still get a vote. Use the filter when strict stickiness is required for correctness. Configuring both at once is redundant: the filter dominates the scorer in every case, so the extra scoring work changes no outcomes.

## Related Documentation

- [Session Affinity Filter](../../filter/sessionaffinity/README.md)
- [Session ID Producer](../../../requestcontrol/dataproducer/sessionid/README.md)
- [Session Attributes](../../../datalayer/attribute/session/README.md)
