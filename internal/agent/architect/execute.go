package architect

import (
	"context"
	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
	"fmt"
	"path"
	"path/filepath"
)

func (a *Agent) executeArchitectureGeneration(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		a.logStep("architecture_generation fallback: artifact store is nil")
		return a.executeArchitectureFallback(task)
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	recipe, err := agentengine.GetRecipe(env.Op)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	if err := agentengine.ValidateTaskInputs(env, recipe); err != nil {
		return core.TaskMetaData{}, err
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("resolve artifacts: %w", err)
	}
	chunks := agentengine.SelectContext(docs, recipe.MaxContextChars)
	prompt := agentengine.BuildPrompt(env, recipe, chunks)
	a.logStep(fmt.Sprintf("architecture_generation llm request start: inputs=%d prompt_chars=%d", len(env.InputArtifacts), len(prompt)))

	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("architecture_generation llm request failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("architecture_generation llm request failed: %w", err)
		}
		return a.executeArchitectureFallback(task)
	}
	a.logStep(fmt.Sprintf("architecture_generation llm request success: chars=%d", len(raw)))
	output, err := agentengine.ParseModelOutput(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("architecture_generation parse model output failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("architecture_generation parse model output failed: %w", err)
		}
		return a.executeArchitectureFallback(task)
	}
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, recipe.OutputKind, output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write artifact outputs: %w", err)
	}
	a.logStep(fmt.Sprintf("architecture_generation artifact write success: outputs=%d", len(uris)))
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) executeArchitectureFallback(task core.TaskMetaData) (core.TaskMetaData, error) {
	a.logStep("architecture_generation fallback output used")
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "architecture", "architecture_v1.md")
	content := "# Architecture\n\nArchitect design draft is ready.\n"
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "architecture", "architecture_v1.md"),
		content,
	)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "ArchitectAgent", message)
	}
}
