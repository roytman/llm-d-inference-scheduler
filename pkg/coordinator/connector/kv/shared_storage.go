package kv

import (
	"github.com/llm-d/coordinator/pkg/connector"
	"github.com/llm-d/coordinator/pkg/pipeline"
)

// sharedStorage uses a shared filesystem for KV transfer. No remote_* fields
// are needed because the consumer reads from the same storage the producer
// writes to.
type sharedStorage struct{}

func (sharedStorage) Name() string { return connector.NameSharedStorage }

func (sharedStorage) PreparePrefillKVParams(_ *pipeline.RequestContext) map[string]any {
	return map[string]any{"do_remote_decode": true}
}

func (sharedStorage) PrepareDecodeKVParams(_ *pipeline.RequestContext) map[string]any {
	return map[string]any{"do_remote_prefill": true}
}
