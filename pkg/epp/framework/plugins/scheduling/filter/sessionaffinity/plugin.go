/*
Copyright 2026 The Kubernetes Authors.

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

// Package sessionaffinity provides a filter that enforces hard session affinity.
// When a session identifier is present, only the endpoint running that session
// is kept. When no session exists, all endpoints pass through for scoring.
package sessionaffinity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	logutil "github.com/llm-d/llm-d-router/pkg/common/observability/logging"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	fwksched "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
	attrsession "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/datalayer/attribute/session"
)

const (
	PluginType = "session-affinity-filter"
)

var _ fwksched.Filter = &Plugin{}

type Config struct {
	SessionIDProducerName string `json:"sessionIDProducerName,omitempty"`
}

type Plugin struct {
	typedName      fwkplugin.TypedName
	sessionDataKey fwkplugin.DataKey
}

func Factory(name string, rawParameters *json.Decoder, _ fwkplugin.Handle) (fwkplugin.Plugin, error) {
	config := Config{}
	if rawParameters != nil {
		if err := rawParameters.Decode(&config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return &Plugin{
		typedName:      fwkplugin.TypedName{Type: PluginType, Name: name},
		sessionDataKey: attrsession.SessionIDDataKey.WithNonEmptyProducerName(config.SessionIDProducerName),
	}, nil
}

func (p *Plugin) TypedName() fwkplugin.TypedName {
	return p.typedName
}

// Filter enforces hard session affinity. If a session ID is present and matches
// an endpoint, only that endpoint is returned. Otherwise, all endpoints pass through.
func (p *Plugin) Filter(ctx context.Context, request *fwksched.InferenceRequest, endpoints []fwksched.Endpoint) []fwksched.Endpoint {
	logger := log.FromContext(ctx)

	if len(endpoints) <= 1 {
		return endpoints
	}

	// Read session ID from request attributes (populated by session-id-producer)
	sessionID, ok := p.readSessionID(request)
	if !ok || sessionID == "" {
		logger.V(logutil.DEBUG).Info("SessionAffinityFilter: no session ID, keeping all endpoints",
			"total", len(endpoints))
		return endpoints
	}

	// Decode the session ID to get the target endpoint name
	targetEndpoint := p.decodeSessionID(sessionID)
	if targetEndpoint == "" {
		logger.V(logutil.DEBUG).Info("SessionAffinityFilter: invalid session ID, keeping all endpoints",
			"sessionID", sessionID, "total", len(endpoints))
		return endpoints
	}

	// Find the endpoint matching the session
	for _, ep := range endpoints {
		if ep.GetMetadata().NamespacedName.String() == targetEndpoint {
			logger.V(logutil.DEBUG).Info("SessionAffinityFilter: session match found, returning single endpoint",
				"sessionID", sessionID, "targetEndpoint", targetEndpoint)
			return []fwksched.Endpoint{ep}
		}
	}

	// Session endpoint not in candidate list, keep all for scoring
	logger.V(logutil.DEBUG).Info("SessionAffinityFilter: session endpoint not found in candidates, keeping all",
		"sessionID", sessionID, "targetEndpoint", targetEndpoint, "total", len(endpoints))
	return endpoints
}

func (p *Plugin) Consumes() map[fwkplugin.DataKey]any {
	return map[fwkplugin.DataKey]any{
		p.sessionDataKey: attrsession.SessionID(""),
	}
}

func (p *Plugin) readSessionID(request *fwksched.InferenceRequest) (string, bool) {
	if request == nil {
		return "", false
	}
	key := p.sessionDataKey.String()
	sessionID, ok := fwksched.ReadRequestAttribute[attrsession.SessionID](request, key)
	return string(sessionID), ok
}

// decodeSessionID decodes a base64-encoded session ID to get the endpoint name.
// Returns empty string if decoding fails.
func (p *Plugin) decodeSessionID(sessionID string) string {
	decodedBytes, err := base64.StdEncoding.DecodeString(sessionID)
	if err != nil {
		return ""
	}
	return string(decodedBytes)
}
