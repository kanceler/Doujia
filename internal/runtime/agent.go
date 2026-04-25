package runtime

import (
	"context"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
)

type Agent interface {
	Create(init AgentInit, deps AgentDeps) Agent
	Execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error)
}

type AgentInit struct {
	AgentID       core.AgentID
	RuntimeID     core.RuntimeID
	RunID         core.RunID
	RunRoot       string
	WorkspacePath string
	TaskHistory   []core.TaskMetaData
}

type AgentDeps struct {
	ArtifactStore artifact.Store
	LLMClient     llm.Client
	Logger        logging.RunLogger
}

type ArtifactStoreFactory func(init AgentInit) (artifact.Store, error)
type LLMClientFactory func(init AgentInit) (llm.Client, error)

type AgentRuntimeConfig struct {
	ArtifactStoreFactory ArtifactStoreFactory
	LLMClientFactory     LLMClientFactory
	Logger               logging.RunLogger
}

func cloneTaskHistory(items []core.TaskMetaData) []core.TaskMetaData {
	if len(items) == 0 {
		return nil
	}
	out := make([]core.TaskMetaData, len(items))
	copy(out, items)
	return out
}

func buildAgentDeps(init AgentInit, config AgentRuntimeConfig) (AgentDeps, error) {
	deps := AgentDeps{}
	if config.ArtifactStoreFactory != nil {
		store, err := config.ArtifactStoreFactory(init)
		if err != nil {
			return AgentDeps{}, err
		}
		deps.ArtifactStore = store
	}
	if config.LLMClientFactory != nil {
		client, err := config.LLMClientFactory(init)
		if err != nil {
			return AgentDeps{}, err
		}
		deps.LLMClient = client
	}
	deps.Logger = config.Logger
	return deps, nil
}
