package sessionaffinity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	logutil "github.com/llm-d/llm-d-router/pkg/common/observability/logging"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/requestcontrol"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

const (
	// SessionAffinityType is the type of the SessionAffinity scorer.
	SessionAffinityType = "session-affinity-scorer"

	// DefaultSessionTokenHeader is the default request/response header carrying
	// the session token. It is shared by the session-affinity scorer and filter.
	DefaultSessionTokenHeader = "x-session-token"
)

// parameters configures the SessionAffinity scorer.
type parameters struct {
	// HeaderName overrides the default x-session-token header used to read and
	// write the session token. When empty the default is used.
	HeaderName string `json:"headerName"`
}

// NormalizeHeader lowercases and trims the configured session header name,
// falling back to DefaultSessionTokenHeader when empty. It is shared by the
// session-affinity scorer and filter.
func NormalizeHeader(name string) string {
	header := strings.ToLower(strings.TrimSpace(name))
	if header == "" {
		return DefaultSessionTokenHeader
	}
	return header
}

// DecodePodName decodes a base64-encoded session token into a pod
// NamespacedName string. It returns "" when the token is empty or cannot be
// decoded. It is shared by the session-affinity scorer and filter.
func DecodePodName(ctx context.Context, token string) string {
	if token == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error decoding session header")
		return ""
	}
	return string(decoded)
}

// WriteSessionResponseHeader encodes targetPod into sessionHeader on the
// response sent to the client. It is shared by the session-affinity scorer and
// filter; pluginType labels the originating plugin in logs.
// TODO: this should be using a cookie and ensure not overriding any other
// cookie values if present.
// Tracked in https://github.com/llm-d/llm-d-router/issues/28
func WriteSessionResponseHeader(ctx context.Context, pluginType, sessionHeader string, response *requestcontrol.Response, targetPod *datalayer.EndpointMetadata) {
	if response == nil || targetPod == nil {
		reqID := "undefined"
		if response != nil {
			reqID = response.RequestID
		}
		log.FromContext(ctx).V(logutil.DEBUG).Info("Session affinity - skip response header because response or targetPod is nil", "plugin", pluginType, "req id", reqID)
		return
	}

	if response.Headers == nil { // TODO should always be populated?
		response.Headers = make(map[string]string)
	}

	response.Headers[sessionHeader] = base64.StdEncoding.EncodeToString([]byte(targetPod.NamespacedName.String()))
}

// compile-time type assertion
var _ scheduling.Scorer = &SessionAffinity{}
var _ requestcontrol.ResponseHeaderProcessor = &SessionAffinity{}

// Factory defines the factory function for SessionAffinity scorer.
func Factory(name string, rawParameters *json.Decoder, _ plugin.Handle) (plugin.Plugin, error) {
	params := parameters{}
	if rawParameters != nil {
		if err := rawParameters.Decode(&params); err != nil {
			return nil, fmt.Errorf("failed to parse the parameters of the '%s' scorer - %w", SessionAffinityType, err)
		}
	}

	return NewSessionAffinity(params.HeaderName).WithName(name), nil
}

// NewSessionAffinity returns a scorer. When sessionHeader is empty the default
// x-session-token header is used.
func NewSessionAffinity(sessionHeader string) *SessionAffinity {
	return &SessionAffinity{
		typedName:     plugin.TypedName{Type: SessionAffinityType},
		sessionHeader: NormalizeHeader(sessionHeader),
	}
}

// SessionAffinity is a routing scorer that routes subsequent
// requests in a session to the same pod as the first request in the
// session was sent to, by giving that pod the specified weight and assigning
// zero score to the rest of the targets
type SessionAffinity struct {
	typedName plugin.TypedName
	// sessionHeader is the request/response header carrying the session token.
	sessionHeader string
}

// TypedName returns the typed name of the plugin.
func (s *SessionAffinity) TypedName() plugin.TypedName {
	return s.typedName
}

// WithName sets the name of the plugin.
func (s *SessionAffinity) WithName(name string) *SessionAffinity {
	s.typedName.Name = name
	return s
}

// Category returns the preference the scorer applies when scoring candidate endpoints.
func (s *SessionAffinity) Category() scheduling.ScorerCategory {
	return scheduling.Affinity
}

// Score assign a high score to the pod used in previous requests and zero to others
func (s *SessionAffinity) Score(ctx context.Context, request *scheduling.InferenceRequest, endpoints []scheduling.Endpoint) map[scheduling.Endpoint]float64 {
	scoredEndpoints := make(map[scheduling.Endpoint]float64)
	podName := DecodePodName(ctx, request.Headers[s.sessionHeader])

	for _, endpoint := range endpoints {
		scoredEndpoints[endpoint] = 0.0 // initial value
		if endpoint.GetMetadata().NamespacedName.String() == podName {
			scoredEndpoints[endpoint] = 1.0
		}
	}

	return scoredEndpoints
}

// ResponseHeader sets the session header on the response sent to the client.
func (s *SessionAffinity) ResponseHeader(ctx context.Context, _ *scheduling.InferenceRequest, response *requestcontrol.Response, targetPod *datalayer.EndpointMetadata) {
	WriteSessionResponseHeader(ctx, SessionAffinityType, s.sessionHeader, response, targetPod)
}
