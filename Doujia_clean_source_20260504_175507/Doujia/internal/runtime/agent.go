package runtime

import (
	"context"
	"devflow/internal/artifact"
	"devflow/internal/core"
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
	RunConfig     core.RunConfig
	WorkspacePath string
}

type AgentDeps struct {
	ArtifactStore   artifact.Store
	OutputCommitter AgentOutputCommitter
	Logger          logging.RunLogger
}

type ArtifactStoreFactory func(init AgentInit) (artifact.Store, error)
type OutputCommitterFactory func(init AgentInit, store artifact.Store) (AgentOutputCommitter, error)

type AgentRuntimeConfig struct {
	ArtifactStoreFactory   ArtifactStoreFactory
	OutputCommitterFactory OutputCommitterFactory
	Logger                 logging.RunLogger
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
	if config.OutputCommitterFactory != nil {
		committer, err := config.OutputCommitterFactory(init, deps.ArtifactStore)
		if err != nil {
			return AgentDeps{}, err
		}
		deps.OutputCommitter = committer
	}
	deps.Logger = config.Logger
	return deps, nil
}
