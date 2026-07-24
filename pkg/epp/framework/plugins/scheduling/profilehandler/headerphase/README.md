# HeaderPhaseProfileHandler

**Type:** `header-phase-profile-handler`

Runs one or more scheduling profiles per request, named by the value of a request
header. This lets a single EPP instance serve several phases of a disaggregated pipeline
(e.g. `encode`, `prefill`, `decode`) whose caller already knows, out of band, which
phase(s) each request is for, instead of needing one EPP instance per phase.

## What it does

Reads the configured header from the incoming request and looks up the
`schedulingProfiles` entry (or entries) it names:

- With exactly one profile configured, that profile always runs, regardless of the
  header (or its absence). There is nothing else to disaggregate to, so a deployment
  scaled down to a single stage -- or one that never disaggregates at all -- works
  without swapping to a different profile handler.
- With more than one profile configured and the header naming a single profile, it runs
  that profile alone, as primary.
- With more than one profile configured and the header naming several profiles separated
  by a comma (e.g. `encode,decode`), every named profile runs in this one scheduling
  cycle instead of one call per phase -- non-deferred scheduling. The first named profile
  is primary: the destination the gateway routes the connection to. Each other named
  profile is secondary: [`PreRequest`](#requestcontrolprerequest) stamps its selected
  endpoints onto a response header instead, so the caller can still reach it. A secondary
  profile that isn't configured is skipped rather than treated as an error; only the
  primary profile must resolve to a configured profile.
- With more than one profile configured and the header missing or blank, `defaultProfile`
  runs instead of failing. This covers calls that never carry the header at all, such as
  pass-through requests (e.g. `/models`) that don't go through phase-tagged scheduling.
- With more than one profile configured and the header's primary profile naming a profile
  that isn't configured, no profile runs -- an unrecognized value is a real error, not
  treated the same as an absent header. The scheduler reports that no profile could be
  run at all, without a reason, which the EPP maps to a 429 response to the client --
  misleading, since the scheduler doesn't distinguish a malformed request from exhausted
  capacity. The EPP logs the specific reason (missing header vs. unconfigured value) for
  operators.

### `requestcontrol.PreRequest`

Implemented: for every profile result other than the primary, stamps its selected
endpoints onto a response header named after that profile, e.g. `prefill` becomes
`x-prefill-host-port`; multiple endpoints for one profile are comma-joined. This only
does anything for non-deferred (comma-separated header) requests -- a single-profile
request has no secondary results to stamp. It mirrors how
[`disagg-profile-handler`](../disagg/README.md)'s `PreRequest` stamps the fixed
`x-prefiller-host-port` / `x-encoder-hosts-ports` headers for its sidecar model, but the
header name here is derived from the profile name rather than fixed, since this handler
doesn't know its profiles' roles ahead of time.

## How this differs from disagg-profile-handler

Both are `scheduling.ProfileHandler` implementations, but they answer a different
question. [`disagg-profile-handler`](../disagg/README.md) answers "which stages does
*this* request need?" for a caller that makes one scheduling call per request and needs
every needed pod picked up front. `header-phase-profile-handler` answers "which single
stage is *this specific call* for?" for a caller (the coordinator) that already knows the
answer and makes one separate scheduling call per phase.

| | `header-phase-profile-handler` | `disagg-profile-handler` |
|---|---|---|
| Selection signal | A request header naming one or more profiles | Decider plugins, evaluated per optional stage |
| Profiles per request | One (deferred) or several named by a comma-separated header (non-deferred) | Decode always, plus encode/prefill when their decider approves -- up to three |
| Scheduling calls per request | One per phase in the deferred case (caller drives the cascade); one cycle for a comma-separated header | One cycle picks every stage the request needs |
| Primary profile | The single named profile, or the first name in a comma-separated header | Always decode |
| `requestcontrol.PreRequest` | Implemented: stamps `x-<profile>-host-port` for each non-primary profile in a non-deferred request | Implemented: stamps `x-prefiller-host-port` / `x-encoder-hosts-ports` for the decode sidecar |
| Fits | The coordinator model, which tracks cross-phase state itself | The sidecar model (llm-d-router), where the decode sidecar orchestrates the remaining hops |

## Configuration

### Parameters

| Name | Type | Default | Description |
|---|---|---|---|
| `headerName` | string | `EPP-Phase` | Request header whose value names the scheduling profile(s) to run, comma-separated for non-deferred scheduling. Matched case-insensitively: the EPP lowercases every incoming header name, so this is normalized to lowercase regardless of how it's written here. |
| `defaultProfile` | string | `decode` | Scheduling profile to run when the header is missing or blank and more than one profile is configured. Matched case-sensitively against `schedulingProfiles` names, like the header value itself. Ignored when only one profile is configured, since that profile always runs. |

### Example

```yaml
plugins:
- type: encode-filter
- type: prefill-filter
- type: decode-filter
- type: header-phase-profile-handler
schedulingProfiles:
- name: encode
  plugins:
  - pluginRef: encode-filter
- name: prefill
  plugins:
  - pluginRef: prefill-filter
- name: decode
  plugins:
  - pluginRef: decode-filter
```

A request with `EPP-Phase: prefill` runs only the `prefill` profile. A request with no
`EPP-Phase` header at all -- e.g. `GET /models` -- runs `decode`, the default.

A request with `EPP-Phase: encode,decode` runs both profiles in one scheduling cycle:
`encode` is primary, and the `decode` profile's selected endpoint is stamped onto an
`x-decode-host-port` response header instead of being routed to directly.

To use a different fallback than `decode`:

```yaml
- type: header-phase-profile-handler
  parameters:
    defaultProfile: prefill
```
