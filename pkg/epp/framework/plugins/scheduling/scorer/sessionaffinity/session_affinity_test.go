package sessionaffinity_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/requestcontrol"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
	attrsession "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/datalayer/attribute/session"
	sessionaffinity "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/scheduling/scorer/sessionaffinity"
)

func newTestEndpointForSession(name, namespace string) scheduling.Endpoint {
	return scheduling.NewEndpoint(
		&fwkdl.EndpointMetadata{
			NamespacedName: k8stypes.NamespacedName{Name: name, Namespace: namespace},
		},
		&fwkdl.Metrics{},
		nil,
	)
}

func TestSessionAffinityScoreWithSessionID(t *testing.T) {
	s := sessionaffinity.NewSessionAffinity(0, "test-producer")
	ctx := context.Background()

	// Create mock endpoints
	endpoint1 := newTestEndpointForSession("pod-1", "default")
	endpoint2 := newTestEndpointForSession("pod-2", "default")
	endpoints := []scheduling.Endpoint{endpoint1, endpoint2}

	tests := []struct {
		name           string
		sessionID      string
		expectedScores map[string]float64 // endpoint name -> score
	}{
		{
			name:      "No session ID - all endpoints get zero score",
			sessionID: "",
			expectedScores: map[string]float64{
				"pod-1": 0.0,
				"pod-2": 0.0,
			},
		},
		{
			name:      "Session ID for pod-1 - pod-1 gets high score",
			sessionID: base64.StdEncoding.EncodeToString([]byte("default/pod-1")),
			expectedScores: map[string]float64{
				"pod-1": 1.0,
				"pod-2": 0.0,
			},
		},
		{
			name:      "Session ID for pod-2 - pod-2 gets high score",
			sessionID: base64.StdEncoding.EncodeToString([]byte("default/pod-2")),
			expectedScores: map[string]float64{
				"pod-1": 0.0,
				"pod-2": 1.0,
			},
		},
		{
			name:      "Invalid base64 session ID - all endpoints get zero score",
			sessionID: "invalid-base64!!!",
			expectedScores: map[string]float64{
				"pod-1": 0.0,
				"pod-2": 0.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &scheduling.InferenceRequest{}
			if tt.sessionID != "" {
				// Simulate session-id-producer having set the session ID
				request.PutAttribute(attrsession.SessionIDDataKey.WithNonEmptyProducerName("test-producer").String(), attrsession.SessionID(tt.sessionID))
			}

			scores := s.Score(ctx, request, endpoints)

			assert.Equal(t, tt.expectedScores["pod-1"], scores[endpoint1], "pod-1 score mismatch")
			assert.Equal(t, tt.expectedScores["pod-2"], scores[endpoint2], "pod-2 score mismatch")
		})
	}
}

func TestResponseHeaderSetsCookie(t *testing.T) {
	s := sessionaffinity.NewSessionAffinity(0, "test-producer")
	ctx := context.Background()

	tests := []struct {
		name             string
		request          *scheduling.InferenceRequest
		response         *requestcontrol.Response
		targetPod        *fwkdl.EndpointMetadata
		expectCookieSet  bool
		expectedContains []string
		expectEmpty      bool
	}{
		{
			name:    "Sets cookie in response",
			request: nil,
			response: &requestcontrol.Response{
				RequestID: "test-req-1",
				Headers:   make(map[string]string),
			},
			targetPod: &fwkdl.EndpointMetadata{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "pod-1",
				},
			},
			expectCookieSet: true,
			expectedContains: []string{
				sessionaffinity.SessionCookieName + "=",
				"Path=/",
				"HttpOnly",
				"SameSite=Lax",
				base64.StdEncoding.EncodeToString([]byte("default/pod-1")),
			},
		},
		{
			name:    "Appends to existing Set-Cookie header",
			request: nil,
			response: &requestcontrol.Response{
				RequestID: "test-req-2",
				Headers: map[string]string{
					sessionaffinity.SetCookieHeaderName: "existing-cookie=value; Path=/",
				},
			},
			targetPod: &fwkdl.EndpointMetadata{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "pod-2",
				},
			},
			expectCookieSet: true,
			expectedContains: []string{
				"existing-cookie=value",
				sessionaffinity.SessionCookieName + "=",
			},
		},
		{
			name:     "Handles nil response gracefully",
			request:  nil,
			response: nil,
			targetPod: &fwkdl.EndpointMetadata{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "pod-1",
				},
			},
			expectCookieSet: false,
		},
		{
			name:    "Handles nil targetPod gracefully",
			request: nil,
			response: &requestcontrol.Response{
				RequestID: "test-req-3",
				Headers:   make(map[string]string),
			},
			targetPod:       nil,
			expectCookieSet: false,
			expectEmpty:     true,
		},
		{
			name: "Skips setting cookie when request already has correct cookie",
			request: &scheduling.InferenceRequest{
				Headers: map[string]string{
					sessionaffinity.CookieHeaderName: sessionaffinity.SessionCookieName + "=" + base64.StdEncoding.EncodeToString([]byte("default/pod-1")),
				},
			},
			response: &requestcontrol.Response{
				RequestID: "test-req-4",
				Headers:   make(map[string]string),
			},
			targetPod: &fwkdl.EndpointMetadata{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "pod-1",
				},
			},
			expectCookieSet: false,
			expectEmpty:     true,
		},
		{
			name: "Sets cookie when request has different cookie value",
			request: &scheduling.InferenceRequest{
				Headers: map[string]string{
					sessionaffinity.CookieHeaderName: sessionaffinity.SessionCookieName + "=" + base64.StdEncoding.EncodeToString([]byte("default/pod-1")),
				},
			},
			response: &requestcontrol.Response{
				RequestID: "test-req-5",
				Headers:   make(map[string]string),
			},
			targetPod: &fwkdl.EndpointMetadata{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "pod-2",
				},
			},
			expectCookieSet: true,
			expectedContains: []string{
				base64.StdEncoding.EncodeToString([]byte("default/pod-2")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			s.ResponseHeader(ctx, tt.request, tt.response, tt.targetPod)

			if tt.response != nil {
				setCookie := tt.response.Headers[sessionaffinity.SetCookieHeaderName]

				if tt.expectEmpty {
					assert.Empty(t, setCookie)
				} else if tt.expectCookieSet {
					assert.NotEmpty(t, setCookie)
				}

				for _, expected := range tt.expectedContains {
					assert.Contains(t, setCookie, expected)
				}
			}
		})
	}
}

func TestSessionAffinityWithConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                string
		maxAge              int
		expectedContains    []string
		expectedNotContains []string
	}{
		{
			name:   "Cookie with MaxAge set",
			maxAge: 3600,
			expectedContains: []string{
				sessionaffinity.SessionCookieName + "=",
				"Max-Age=3600",
				"HttpOnly",
				"SameSite=Lax",
			},
		},
		{
			name:   "Session cookie (no MaxAge)",
			maxAge: 0,
			expectedContains: []string{
				"HttpOnly",
				"SameSite=Lax",
			},
			expectedNotContains: []string{
				"Max-Age",
				"Secure",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := sessionaffinity.NewSessionAffinity(tt.maxAge, "test-producer")

			response := &requestcontrol.Response{
				RequestID: "test-req-" + tt.name,
				Headers:   make(map[string]string),
			}
			targetPod := &fwkdl.EndpointMetadata{
				NamespacedName: k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "pod-1",
				},
			}

			s.ResponseHeader(ctx, nil, response, targetPod)

			setCookie := response.Headers[sessionaffinity.SetCookieHeaderName]
			require.NotEmpty(t, setCookie)

			for _, expected := range tt.expectedContains {
				assert.Contains(t, setCookie, expected)
			}

			for _, notExpected := range tt.expectedNotContains {
				assert.NotContains(t, setCookie, notExpected)
			}
		})
	}
}
