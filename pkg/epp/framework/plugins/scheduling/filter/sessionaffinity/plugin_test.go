package sessionaffinity_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	k8stypes "k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
	attrsession "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/datalayer/attribute/session"
	sessionaffinity "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/scheduling/filter/sessionaffinity"
)

func newTestEndpoint(name string) scheduling.Endpoint {
	return scheduling.NewEndpoint(
		&fwkdl.EndpointMetadata{
			NamespacedName: k8stypes.NamespacedName{Name: name, Namespace: "default"},
		},
		&fwkdl.Metrics{},
		nil,
	)
}

func TestSessionAffinityFilter(t *testing.T) {
	ctx := context.Background()

	// Create test filter
	plugin, err := sessionaffinity.Factory("test-filter", nil, nil)
	assert.NoError(t, err)
	filter := plugin.(*sessionaffinity.Plugin)

	// Create mock endpoints
	endpoint1 := newTestEndpoint("pod-1")
	endpoint2 := newTestEndpoint("pod-2")
	endpoint3 := newTestEndpoint("pod-3")
	endpoints := []scheduling.Endpoint{endpoint1, endpoint2, endpoint3}

	tests := []struct {
		name              string
		sessionID         string
		expectedEndpoints []string // endpoint names that should be returned
	}{
		{
			name:              "No session ID - all endpoints pass through",
			sessionID:         "",
			expectedEndpoints: []string{"pod-1", "pod-2", "pod-3"},
		},
		{
			name:              "Session ID for pod-1 - only pod-1 returned",
			sessionID:         base64.StdEncoding.EncodeToString([]byte("default/pod-1")),
			expectedEndpoints: []string{"pod-1"},
		},
		{
			name:              "Session ID for pod-2 - only pod-2 returned",
			sessionID:         base64.StdEncoding.EncodeToString([]byte("default/pod-2")),
			expectedEndpoints: []string{"pod-2"},
		},
		{
			name:              "Session ID for non-existent pod - all endpoints pass through",
			sessionID:         base64.StdEncoding.EncodeToString([]byte("default/pod-99")),
			expectedEndpoints: []string{"pod-1", "pod-2", "pod-3"},
		},
		{
			name:              "Invalid base64 session ID - all endpoints pass through",
			sessionID:         "invalid-base64!!!",
			expectedEndpoints: []string{"pod-1", "pod-2", "pod-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &scheduling.InferenceRequest{}
			if tt.sessionID != "" {
				// Simulate session-id-producer having set the session ID
				request.PutAttribute(attrsession.SessionIDDataKey.WithNonEmptyProducerName("session-id-producer").String(), attrsession.SessionID(tt.sessionID))
			}

			result := filter.Filter(ctx, request, endpoints)

			// Check that the correct number of endpoints are returned
			assert.Equal(t, len(tt.expectedEndpoints), len(result), "unexpected number of endpoints")

			// Check that the correct endpoints are returned
			resultNames := make(map[string]bool)
			for _, ep := range result {
				resultNames[ep.GetMetadata().NamespacedName.Name] = true
			}

			for _, expectedName := range tt.expectedEndpoints {
				assert.True(t, resultNames[expectedName], "expected endpoint %s not found in result", expectedName)
			}
		})
	}
}

func TestSessionAffinityFilterSingleEndpoint(t *testing.T) {
	ctx := context.Background()

	plugin, err := sessionaffinity.Factory("test-filter", nil, nil)
	assert.NoError(t, err)
	filter := plugin.(*sessionaffinity.Plugin)

	// Single endpoint should always pass through
	endpoint1 := newTestEndpoint("pod-1")
	endpoints := []scheduling.Endpoint{endpoint1}

	request := &scheduling.InferenceRequest{}
	request.PutAttribute(attrsession.SessionIDDataKey.WithNonEmptyProducerName("session-id-producer").String(),
		attrsession.SessionID(base64.StdEncoding.EncodeToString([]byte("default/pod-2"))))

	result := filter.Filter(ctx, request, endpoints)

	assert.Equal(t, 1, len(result))
	assert.Equal(t, "pod-1", result[0].GetMetadata().NamespacedName.Name)
}

func TestSessionAffinityFilterNilRequest(t *testing.T) {
	ctx := context.Background()

	plugin, err := sessionaffinity.Factory("test-filter", nil, nil)
	assert.NoError(t, err)
	filter := plugin.(*sessionaffinity.Plugin)

	endpoint1 := newTestEndpoint("pod-1")
	endpoint2 := newTestEndpoint("pod-2")
	endpoints := []scheduling.Endpoint{endpoint1, endpoint2}

	// Nil request should pass all endpoints through
	result := filter.Filter(ctx, nil, endpoints)

	assert.Equal(t, 2, len(result))
}
