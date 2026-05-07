package architect

import (
	"context"
	"encoding/json"
	"fmt"

	"devflow/internal/agent/core"
)

func (a *Agent) runCreateContainer(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	createHandler, ok := req.Handlers.Get("container_create")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("container_create handler is required")
	}
	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("artifact_write handler is required")
	}

	args := map[string]any{
		"environment_spec_key": core.LKEnvironmentSpec,
	}
	if bundleHasLogicalKey(req.Bundle, core.LKRuntimeContract) {
		args["runtime_contract_key"] = core.LKRuntimeContract
	}
	createResp, err := createHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args:   args,
	})
	if err != nil {
		return core.AgentResult{}, err
	}

	content, _ := createResp.Data["content"].(string)
	if content == "" {
		if contextData, ok := createResp.Data["container_context"]; ok {
			body, marshalErr := json.MarshalIndent(contextData, "", "  ")
			if marshalErr != nil {
				return core.AgentResult{}, marshalErr
			}
			content = string(body)
		}
	}
	if content == "" {
		return core.AgentResult{}, fmt.Errorf("container_create returned no container_context content")
	}

	writeResp, err := writeHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"logical_key": core.LKContainerContext,
			"content":     content,
		},
	})
	if err != nil {
		return core.AgentResult{}, err
	}

	output := core.AgentOutput{
		LogicalKey:  core.LKContainerContext,
		ObjectType:  stringValue(writeResp.Data["object_type"]),
		ContentType: stringValue(writeResp.Data["content_type"]),
		Encoding:    stringValue(writeResp.Data["encoding"]),
		Status:      stringValue(writeResp.Data["status"]),
		Path:        stringValue(writeResp.Data["path"]),
		ArtifactURI: stringValue(writeResp.Data["artifact_uri"]),
	}
	return core.AgentResult{
		Result:  "kok",
		Message: "architect create_container completed",
		Outputs: []core.AgentOutput{output},
	}, nil
}
