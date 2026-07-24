/*
Copyright 2026 The llm-d Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package headerphase

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	logging "github.com/llm-d/llm-d-router/pkg/common/observability/logging"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	fwkrc "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/requestcontrol"
	fwksched "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

const (
	// HeaderPhaseProfileHandlerType is the type of the HeaderPhaseProfileHandler.
	HeaderPhaseProfileHandlerType = "header-phase-profile-handler"

	// defaultHeaderName is the request header read when parameters.HeaderName is empty.
	defaultHeaderName = "EPP-Phase"

	// defaultProfileName is the scheduling profile run when parameters.DefaultProfile is
	// empty.
	defaultProfileName = "decode"

	// phaseSeparator delimits multiple profile names in the phase header, enabling
	// non-deferred scheduling: the caller names every phase it wants co-scheduled in one
	// call instead of one call per phase. Not configurable.
	phaseSeparator = ","

	// secondaryEndpointHeaderPrefix and secondaryEndpointHeaderSuffix bracket the
	// profile name in the response header PreRequest writes for each non-primary
	// profile a non-deferred request selected, e.g. "prefill" becomes
	// "x-prefill-host-port".
	secondaryEndpointHeaderPrefix = "x-"
	secondaryEndpointHeaderSuffix = "-host-port"
)

// compile-time type assertions
var (
	_ fwksched.ProfileHandler = &HeaderPhaseProfileHandler{}
	_ fwkrc.PreRequest        = &HeaderPhaseProfileHandler{}
)

// parameters configures the HeaderPhaseProfileHandler.
type parameters struct {
	// HeaderName is the request header whose value names the scheduling profile to run.
	// Defaults to defaultHeaderName when empty.
	HeaderName string `json:"headerName"`
	// DefaultProfile is the scheduling profile to run when the header is missing or
	// blank. Defaults to defaultProfileName when empty. Useful for requests that never
	// carry the header at all - pass-through calls (e.g. /models) or a deployment that
	// doesn't disaggregate.
	DefaultProfile string `json:"defaultProfile"`
}

// Factory defines the factory function for HeaderPhaseProfileHandler.
func Factory(name string, rawParameters *json.Decoder, _ fwkplugin.Handle) (fwkplugin.Plugin, error) {
	params := parameters{}
	if rawParameters != nil {
		if err := rawParameters.Decode(&params); err != nil {
			return nil, fmt.Errorf("failed to parse the parameters of the '%s' profile handler - %w", HeaderPhaseProfileHandlerType, err)
		}
	}

	return NewHeaderPhaseProfileHandler(params.HeaderName, params.DefaultProfile).WithName(name), nil
}

// NewHeaderPhaseProfileHandler initializes a new HeaderPhaseProfileHandler and returns
// its pointer.
//
// headerName is lowercased and trimmed, falling back to defaultHeaderName when that
// leaves it empty: the EPP's request handler lowercases every incoming header name at
// ingestion (pkg/epp/handlers/request.go), so the configured name must be normalized the
// same way to match, and an empty header key would never match any request.
//
// defaultProfile is trimmed, falling back to defaultProfileName when that leaves it
// empty. Unlike headerName, it is not case-normalized: it names a schedulingProfiles
// entry, which (like the header value itself) is matched case-sensitively.
func NewHeaderPhaseProfileHandler(headerName, defaultProfile string) *HeaderPhaseProfileHandler {
	headerName = strings.ToLower(strings.TrimSpace(headerName))
	if headerName == "" {
		headerName = strings.ToLower(defaultHeaderName)
	}

	defaultProfile = strings.TrimSpace(defaultProfile)
	if defaultProfile == "" {
		defaultProfile = defaultProfileName
	}

	return &HeaderPhaseProfileHandler{
		typedName:      fwkplugin.TypedName{Type: HeaderPhaseProfileHandlerType, Name: HeaderPhaseProfileHandlerType},
		headerName:     headerName,
		defaultProfile: defaultProfile,
	}
}

// HeaderPhaseProfileHandler runs one or more scheduling profiles per request, named by
// the value of a request header, letting a single EPP instance serve several phases of
// a disaggregated pipeline (e.g. encode, prefill, decode) whose caller already knows,
// out of band, which phase(s) each request is for - unlike the disagg profile handler,
// which decides which profiles to run via decider plugins.
//
// The header names one profile in the common (deferred) case, or several separated by
// phaseSeparator for non-deferred scheduling: every named profile runs in this one
// scheduling cycle, the first is primary (the destination the gateway actually routes
// the connection to), and PreRequest stamps the others' selected endpoints onto response
// headers instead, mirroring disagg-profile-handler's PreRequest for the sidecar model.
//
// Two fallbacks keep single-stage and header-less traffic working without a different
// profile handler: with exactly one configured profile there is nothing to disaggregate,
// so that profile always runs regardless of the header (or its absence); with more than
// one configured profile, a request whose header is missing or blank runs defaultProfile
// instead of failing. A header naming a profile that isn't configured is still an error -
// only the header's absence triggers the default, not an unrecognized value.
type HeaderPhaseProfileHandler struct {
	typedName      fwkplugin.TypedName
	headerName     string
	defaultProfile string
}

// TypedName returns the type and name tuple of this plugin instance.
func (h *HeaderPhaseProfileHandler) TypedName() fwkplugin.TypedName {
	return h.typedName
}

// WithName sets the name of the profile handler.
func (h *HeaderPhaseProfileHandler) WithName(name string) *HeaderPhaseProfileHandler {
	h.typedName.Name = name
	return h
}

// phaseHeader returns the trimmed value of the phase header, or "" when request is
// nil or the header is absent or blank. Trimming avoids surprising lookup failures
// when the header carries incidental leading/trailing whitespace.
func (h *HeaderPhaseProfileHandler) phaseHeader(request *fwksched.InferenceRequest) string {
	if request == nil {
		return ""
	}
	return strings.TrimSpace(request.Headers[h.headerName])
}

// phaseList splits the phase header on phaseSeparator into trimmed, non-empty tokens,
// in the order given. A single-value header (the common, deferred case) yields a
// one-element list; a missing, blank, or all-separators header yields an empty list.
func (h *HeaderPhaseProfileHandler) phaseList(request *fwksched.InferenceRequest) []string {
	raw := h.phaseHeader(request)
	if raw == "" {
		return nil
	}
	tokens := strings.Split(raw, phaseSeparator)
	phases := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" {
			phases = append(phases, token)
		}
	}
	return phases
}

// noMatchError explains why no configured scheduling profile matches phase, the
// already-trimmed value of the phase header.
func (h *HeaderPhaseProfileHandler) noMatchError(phase string) error {
	if phase == "" {
		return fmt.Errorf("header-phase profile handler: missing %q header", h.headerName)
	}
	return fmt.Errorf("header-phase profile handler: no scheduling profile configured for %q header value %q", h.headerName, phase)
}

// Pick selects the SchedulingProfiles to run in this cycle: the only configured profile
// when there is just one, otherwise every profile named by the request's phase header
// (comma-separated for non-deferred scheduling), falling back to a single defaultProfile
// when the header is missing or blank. The first named phase is primary; any further
// phases are secondary and only need to be configured to run, not to exist for
// defaultProfile purposes. It returns an empty map once the selected profiles have run,
// or when the primary phase could not be resolved. In the latter case the scheduler's
// run loop (pkg/epp/scheduling.Scheduler.Schedule) stops without ever calling
// ProcessResults, so the specific reason is logged here rather than returned from
// ProcessResults, where it would be unreachable. The client never sees that reason: it
// only gets the scheduler's generic "failed to run any scheduler profile" error, which
// pkg/epp/requestcontrol/director.go maps to a 429 ResourceExhausted response -
// misleading, since a malformed or missing header is a client error, not a capacity
// problem. Surfacing the real reason to the client needs a scheduler/ProfileHandler
// contract change and is out of scope here; the log is a diagnostic aid for operators,
// not an equivalent substitute for what the caller receives.
func (h *HeaderPhaseProfileHandler) Pick(ctx context.Context, request *fwksched.InferenceRequest, profiles map[string]fwksched.SchedulerProfile,
	profileResults map[string]*fwksched.ProfileRunResult) map[string]fwksched.SchedulerProfile {
	if len(profileResults) > 0 { // the selected profiles have already run
		return map[string]fwksched.SchedulerProfile{}
	}

	// With exactly one configured profile there is nothing to disaggregate: always run
	// it, so a deployment scaled down to a single stage works without swapping profile
	// handlers or requiring every caller to send the header.
	if len(profiles) == 1 {
		for name, profile := range profiles {
			return map[string]fwksched.SchedulerProfile{name: profile}
		}
	}

	originalPhases := h.phaseList(request)
	phases := originalPhases
	if len(phases) == 0 {
		phases = []string{h.defaultProfile}
	}

	primaryProfile, ok := profiles[phases[0]]
	if !ok {
		var reportPhase string
		if len(originalPhases) > 0 {
			reportPhase = originalPhases[0]
		}
		log.FromContext(ctx).Error(h.noMatchError(reportPhase), "no scheduling profile selected for request")
		return map[string]fwksched.SchedulerProfile{}
	}

	selected := map[string]fwksched.SchedulerProfile{phases[0]: primaryProfile}
	for _, phase := range phases[1:] {
		profile, ok := profiles[phase]
		if !ok {
			log.FromContext(ctx).V(logging.DEBUG).Info("secondary scheduling profile not configured, skipping", "phase", phase)
			continue
		}
		selected[phase] = profile
	}

	return selected
}

// primaryProfileName returns the key in profileResults that should be primary: the sole
// result when there is only one (covers both the single-configured-profile shortcut and
// an ordinary single-value header), otherwise the first phase in the header's list that
// actually ran (the non-deferred, comma-separated case). len(profileResults) > 1 can only
// happen via that comma-separated path - defaultProfile substitution always yields
// exactly one phase - so re-deriving the phase list here is consistent with what Pick
// used to select these results.
func (h *HeaderPhaseProfileHandler) primaryProfileName(request *fwksched.InferenceRequest, profileResults map[string]*fwksched.ProfileRunResult) string {
	if len(profileResults) == 1 {
		for name := range profileResults {
			return name
		}
	}
	for _, phase := range h.phaseList(request) {
		if _, ran := profileResults[phase]; ran {
			return phase
		}
	}
	return ""
}

// ProcessResults handles the outcome of the profile(s) selected by Pick. It specifies in
// the SchedulingResult the key of the primary profile - the destination the gateway
// routes the connection to - among possibly several profiles run for non-deferred
// scheduling.
func (h *HeaderPhaseProfileHandler) ProcessResults(_ context.Context, request *fwksched.InferenceRequest,
	profileResults map[string]*fwksched.ProfileRunResult) (*fwksched.SchedulingResult, error) {
	if len(profileResults) == 0 {
		return nil, h.noMatchError(h.phaseHeader(request))
	}

	primaryName := h.primaryProfileName(request, profileResults)
	if primaryName == "" {
		return nil, fmt.Errorf("header-phase profile handler: could not determine a primary profile among %d results", len(profileResults))
	}

	if profileResults[primaryName] == nil { // there was an error while running the profile
		return nil, fmt.Errorf("failed to run scheduler profile '%s'", primaryName)
	}

	return &fwksched.SchedulingResult{
		ProfileResults:     profileResults,
		PrimaryProfileName: primaryName,
	}, nil
}

// secondaryEndpointHeader names the response header PreRequest writes with the selected
// endpoints of a non-primary profile, e.g. "prefill" becomes "x-prefill-host-port".
func secondaryEndpointHeader(profileName string) string {
	return secondaryEndpointHeaderPrefix + profileName + secondaryEndpointHeaderSuffix
}

// PreRequest stamps each non-primary profile's selected endpoints onto a response header
// named after that profile, so a caller that named several phases in one non-deferred
// request can still reach the secondary phases' destinations - mirroring how
// disagg-profile-handler's PreRequest stamps the fixed x-prefiller-host-port /
// x-encoder-hosts-ports headers for its sidecar model. The primary profile's endpoint is
// unaffected: it's the destination the gateway already routes the connection to.
func (h *HeaderPhaseProfileHandler) PreRequest(_ context.Context, request *fwksched.InferenceRequest, schedulingResult *fwksched.SchedulingResult) {
	if request == nil || schedulingResult == nil {
		return
	}
	for name, result := range schedulingResult.ProfileResults {
		if name == schedulingResult.PrimaryProfileName || result == nil {
			continue
		}
		headerName := secondaryEndpointHeader(name)
		delete(request.Headers, headerName)

		hostPorts := make([]string, 0, len(result.TargetEndpoints))
		for _, endpoint := range result.TargetEndpoints {
			meta := endpoint.GetMetadata()
			hostPorts = append(hostPorts, net.JoinHostPort(meta.Address, meta.Port))
		}
		if len(hostPorts) == 0 {
			continue
		}
		request.Headers[headerName] = strings.Join(hostPorts, ",")
	}
}
