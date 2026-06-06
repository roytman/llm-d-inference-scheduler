# Session Attributes

Per-request session identity used by affinity-aware scorers and filters.

## `SessionID`

Holds the session identifier extracted from a request. Stored on the
`InferenceRequest` attribute store (one entry per request, not per endpoint).

- **Key**: `SessionIDDataKey` (default producer: `session-id-producer`)
- **Type**: `SessionID` (string alias)
- **Reader helper**: `session.ReadSessionID(request)` returns the value and a
  presence boolean. Consumers should prefer this over reading the attribute
  directly so the storage choice stays encapsulated.

## `BoundEndpoint`

Identifies the endpoint that a session has been pinned to by a previous
request. The same `session-id-producer` that publishes `SessionID` also
maintains the binding cache and writes `BoundEndpoint` on subsequent
requests for a known session.

- **Key**: `BoundEndpointDataKey` (default producer: `session-id-producer`)
- **Type**: `BoundEndpoint` (alias of `types.NamespacedName`)

## Producers

- **`session-id-producer`** (Request Control): extracts the session
  identifier from a configured request header or named cookie, tracks
  selected endpoints in a TTL-expiring LRU, and publishes both attributes.
