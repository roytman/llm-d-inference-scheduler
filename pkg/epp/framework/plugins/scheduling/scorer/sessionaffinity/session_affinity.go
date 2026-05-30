package sessionaffinity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	logutil "github.com/llm-d/llm-d-router/pkg/common/observability/logging"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/requestcontrol"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
	attrsession "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/datalayer/attribute/session"
)

const (
	// SessionAffinityType is the type of the SessionAffinity scorer.
	SessionAffinityType = "session-affinity-scorer"

	// SessionCookieName - the name of the session cookie
	SessionCookieName = "llm-d-session"
	// CookieHeaderName - the HTTP Cookie header name
	CookieHeaderName = "cookie"
	// SetCookieHeaderName - the HTTP Set-Cookie header name
	SetCookieHeaderName = "set-cookie"

	// defaultMaxAge is the default cookie Max-Age in seconds.
	// 0 means session cookie (expires when browser closes).
	defaultMaxAge = 0
)

// compile-time type assertion
var _ scheduling.Scorer = &SessionAffinity{}
var _ requestcontrol.ResponseHeaderProcessor = &SessionAffinity{}

type sessionAffinityConfig struct {
	MaxAge                int    `json:"maxAge"`                // Cookie Max-Age in seconds (0 = session cookie)
	SessionIDProducerName string `json:"sessionIDProducerName"` // Name of the session-id-producer to consume from
}

// Factory defines the factory function for SessionAffinity scorer.
func Factory(name string, parameters *json.Decoder, _ plugin.Handle) (plugin.Plugin, error) {
	config := sessionAffinityConfig{
		MaxAge: defaultMaxAge, // Set default
	}

	if parameters != nil {
		if err := parameters.Decode(&config); err != nil {
			return nil, fmt.Errorf("invalid session affinity config: %w", err)
		}
	}

	if config.MaxAge < 0 {
		return nil, fmt.Errorf("invalid session affinity config: maxAge must be >= 0, got %d", config.MaxAge)
	}
	return NewSessionAffinity(config.MaxAge, config.SessionIDProducerName).WithName(name), nil
}

// NewSessionAffinity returns a scorer with the given maxAge configuration.
// maxAge specifies the cookie's Max-Age attribute in seconds.
// If 0 (default), the cookie is a session cookie (expires when browser closes).
// sessionIDProducerName is the name of the session-id-producer to read session data from.
func NewSessionAffinity(maxAge int, sessionIDProducerName string) *SessionAffinity {
	return &SessionAffinity{
		typedName:      plugin.TypedName{Type: SessionAffinityType},
		maxAge:         maxAge,
		sessionDataKey: attrsession.SessionIDDataKey.WithNonEmptyProducerName(sessionIDProducerName),
	}
}

// SessionAffinity is a routing scorer that routes subsequent
// requests in a session to the same pod as the first request in the
// session was sent to, by giving that pod the specified weight and assigning
// zero score to the rest of the targets
type SessionAffinity struct {
	typedName      plugin.TypedName
	maxAge         int
	sessionDataKey plugin.DataKey
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
	target := ""

	// Read session ID from the session-id-producer data
	if request != nil {
		sessionID, ok := s.readSessionID(request)
		if ok && sessionID != "" {
			// Decode the session ID to get the target endpoint name
			decodedBytes, err := base64.StdEncoding.DecodeString(sessionID)
			if err != nil {
				log.FromContext(ctx).V(logutil.DEBUG).Info("Error decoding session ID", "error", err)
			} else {
				target = string(decodedBytes)
			}
		}
	}

	for _, endpoint := range endpoints {
		scoredEndpoints[endpoint] = 0.0 // initial value
		if endpoint.GetMetadata().NamespacedName.String() == target {
			scoredEndpoints[endpoint] = 1.0
		}
	}

	return scoredEndpoints
}

// Consumes declares the SessionID attribute key read by this scorer.
func (s *SessionAffinity) Consumes() map[plugin.DataKey]any {
	return map[plugin.DataKey]any{
		s.sessionDataKey: attrsession.SessionID(""),
	}
}

// readSessionID reads the session ID from the request attributes.
func (s *SessionAffinity) readSessionID(request *scheduling.InferenceRequest) (string, bool) {
	if request == nil {
		return "", false
	}
	key := s.sessionDataKey.String()
	sessionID, ok := scheduling.ReadRequestAttribute[attrsession.SessionID](request, key)
	return string(sessionID), ok
}

// ResponseHeader sets the session cookie on the response sent to the client
// only if the cookie doesn't exist or its value is different
func (s *SessionAffinity) ResponseHeader(ctx context.Context, request *scheduling.InferenceRequest, response *requestcontrol.Response, targetEndpoint *datalayer.EndpointMetadata) {
	logger := log.FromContext(ctx)

	if response == nil || targetEndpoint == nil {
		reqID := "undefined"
		if response != nil {
			reqID = response.RequestID
		}
		logger.V(logutil.DEBUG).Info("Session affinity scorer - skip post response because one of response, targetEndpoint is nil", "req id", reqID)
		return
	}

	// Create session token for the target endpoint
	expectedSessionToken := base64.StdEncoding.EncodeToString([]byte(targetEndpoint.NamespacedName.String()))

	// Check if client already has the correct cookie
	if request != nil && request.Headers != nil {
		cookieHeader := request.Headers[CookieHeaderName]
		if cookieHeader != "" {
			existingSessionToken := extractCookieValue(cookieHeader, SessionCookieName)
			if existingSessionToken == expectedSessionToken {
				// Cookie already exists with correct value, no need to set it again
				return
			}
		}
	}

	// Cookie doesn't exist or has different value, set it
	if response.Headers == nil {
		response.Headers = make(map[string]string)
	}

	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    expectedSessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   s.maxAge, // 0 means session cookie
	}
	cookieStr := cookie.String()

	logger.V(logutil.DEBUG).Info("Setting session cookie",
		"sessionToken", expectedSessionToken,
		"targetEndpoint", targetEndpoint.NamespacedName.String())

	// Append to existing Set-Cookie headers if any
	existingSetCookie := response.Headers[SetCookieHeaderName]
	if existingSetCookie != "" {
		response.Headers[SetCookieHeaderName] = existingSetCookie + "\n" + cookieStr
	} else {
		response.Headers[SetCookieHeaderName] = cookieStr
	}
}

// extractCookieValue extracts the value of a specific cookie from the Cookie header
func extractCookieValue(cookieHeader, cookieName string) string {
	cookies := strings.Split(cookieHeader, ";")
	for _, cookie := range cookies {
		cookie = strings.TrimSpace(cookie)
		parts := strings.SplitN(cookie, "=", 2)
		if len(parts) == 2 && parts[0] == cookieName {
			return parts[1]
		}
	}
	return ""
}
