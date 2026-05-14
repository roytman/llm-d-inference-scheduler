package kv

import (
	"github.com/llm-d/coordinator/pkg/connector"
	"github.com/llm-d/coordinator/pkg/pipeline"
)

// nixlV2 implements the NIXL v2 P2P KV transfer protocol. The prefill request
// declares the request will be remote-decoded; the decode request forwards
// the prefill response's kv_transfer_params verbatim plus do_remote_prefill
// so the decode pod can pull KV blocks from the prefill pod.
type nixlV2 struct{}

func (nixlV2) Name() string { return connector.NameNIXLv2 }

func (nixlV2) PreparePrefillKVParams(_ *pipeline.RequestContext) map[string]any {
	return map[string]any{
		"do_remote_decode":  true,
		"do_remote_prefill": false,
	}
}

func (nixlV2) PrepareDecodeKVParams(reqCtx *pipeline.RequestContext) map[string]any {
	out := make(map[string]any, len(reqCtx.KVTransferParams)+1)
	for k, v := range reqCtx.KVTransferParams {
		out[k] = v
	}
	out["do_remote_prefill"] = true
	return out
}
