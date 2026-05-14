// Package kv contains KV transfer connector implementations selected at
// config time. Each connector defines the kv_transfer_params shape sent to
// prefill and decode pods. Orchestration variants (shared_storage
// try-decode-first) are not implemented in this package — they require
// pipeline changes outside the per-step wire format.
package kv

import (
	"fmt"

	"github.com/llm-d/coordinator/pkg/connector"
	"github.com/llm-d/coordinator/pkg/pipeline"
)

// Connector controls the kv_transfer_params wire shape on the prefill and
// decode requests. Implementations are stateless and safe to share across
// requests.
type Connector interface {
	Name() string
	// PreparePrefillKVParams returns the kv_transfer_params map written into
	// the prefill request body.
	PreparePrefillKVParams(reqCtx *pipeline.RequestContext) map[string]any
	// PrepareDecodeKVParams returns the kv_transfer_params map written into
	// the decode request body. The prefill response's kv_transfer_params is
	// already populated into reqCtx.KVTransferParams by PrefillStep.
	PrepareDecodeKVParams(reqCtx *pipeline.RequestContext) map[string]any
}

// DefaultName is the connector name selected when an empty string is passed
// to Build.
const DefaultName = connector.NameNIXLv2

// Build returns the KV connector for name. An empty name selects DefaultName.
func Build(name string) (Connector, error) {
	if name == "" {
		name = DefaultName
	}
	switch name {
	case connector.NameNIXLv2:
		return nixlV2{}, nil
	case connector.NameSharedStorage:
		return sharedStorage{}, nil
	default:
		return nil, fmt.Errorf("unknown connector: %q", name)
	}
}
