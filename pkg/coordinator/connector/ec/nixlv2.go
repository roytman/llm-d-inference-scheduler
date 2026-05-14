package ec

import (
	"github.com/llm-d/coordinator/pkg/connector"
	"github.com/llm-d/coordinator/pkg/pipeline"
)

// nixlV2 is the NIXL EC connector: each encoder response carries an
// ec_transfer_params object keyed by the encoded image's mm_hash. The
// coordinator collects them in order and forwards them as a per-modality
// list on the prefill request: {"image": [{"hash1": {...}}, ...]}.
type nixlV2 struct{}

func (nixlV2) Name() string { return connector.NameNIXLv2 }

func (nixlV2) MergeEncodeResponse(reqCtx *pipeline.RequestContext, encResp map[string]any) {
	if len(encResp) == 0 {
		return
	}
	reqCtx.ECTransferParams = append(reqCtx.ECTransferParams, encResp)
}

func (nixlV2) PreparePrefillECParams(reqCtx *pipeline.RequestContext) map[string]any {
	if len(reqCtx.ECTransferParams) == 0 {
		return nil
	}
	return map[string]any{"image": reqCtx.ECTransferParams}
}
