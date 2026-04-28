package app

import (
	architectagent "devflow/internal/agent/architect"
	ceoagent "devflow/internal/agent/ceo"
	coderagent "devflow/internal/agent/coder"
	pmagent "devflow/internal/agent/pm"
	testeragent "devflow/internal/agent/tester"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
	"devflow/internal/orchestrator"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/query"
	"devflow/internal/state/repo"
	"fmt"
	"path/filepath"
)

type Modules struct {
	RunManager     *orchestrator.RunManager
	SessionRuntime runtime.SessionRuntime
	MessageGateway *runtime.MessageGateway
	TaskRuntime    *runtime.TaskRuntime
}

type Internals struct {
	PipelineRegistry pipeline.Registry
	Orchestrator     *orchestrator.Service
	RunRepository    repo.RunRepository
	TaskRepository   repo.TaskRepository
}

type Bootstrap struct {
	Modules   Modules
	Internals Internals
}

func NewBootstrap(projectsRoot string) *Bootstrap {
	if absRoot, err := filepath.Abs(filepath.Clean(projectsRoot)); err == nil {
		projectsRoot = absRoot
	}
	pipelineRegistry := pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne(), pipeline.BuiltinPhaseTwo())
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	runLogger := logging.NewFileRunLogger(projectsRoot)
	taskHistoryQuery := query.NewTaskHistoryQuery(pipelineRegistry, runRepo, taskRepo)
	agentConfig := runtime.AgentRuntimeConfig{
		ArtifactStoreFactory: func(init runtime.AgentInit) (artifact.Store, error) {
			return &artifact.ScopedLocalStore{
				RunRoot:       init.RunRoot,
				WorkspaceRoot: init.WorkspacePath,
			}, nil
		},
		LLMClientFactory: func(init runtime.AgentInit) (llm.Client, error) {
			cfg := init.RunConfig.LLM
			switch llm.ProviderType(cfg.ProviderType) {
			case llm.ProviderTypeNoop:
				return llm.NoopClient{}, nil
			case llm.ProviderTypeOpenAICompatible:
				clientCfg := llm.Config{
					ProviderType:   llm.ProviderTypeOpenAICompatible,
					BaseURL:        cfg.BaseURL,
					APIKey:         cfg.APIKey,
					Model:          cfg.Model,
					RequestTimeout: cfg.RequestTimeout,
				}
				if clientCfg.BaseURL == "" {
					clientCfg.BaseURL = llm.DefaultBaseURL
				}
				if clientCfg.APIKey == "" {
					return nil, fmt.Errorf("run %q is missing llm api key", init.RunID)
				}
				if clientCfg.Model == "" {
					return nil, fmt.Errorf("run %q is missing llm model", init.RunID)
				}
				return llm.BuildClient(clientCfg)
			default:
				return nil, fmt.Errorf("run %q has unsupported llm provider type %q", init.RunID, cfg.ProviderType)
			}
		},
		TaskHistoryProvider: taskHistoryQuery.ListAgentHistory,
		Logger:              runLogger,
	}
	sessionRuntime := runtime.NewInMemorySessionRuntime(ceoagent.NewFactory(), runLogger, agentConfig)

	taskRuntime := runtime.NewTaskRuntime(4, nil, runLogger, agentConfig)
	messageGateway := runtime.NewMessageGateway(taskRuntime, runLogger)
	taskRuntime.SetBinder(messageGateway)
	orch := orchestrator.NewService(pipelineRegistry, runRepo, taskRepo, taskRuntime, messageGateway, sessionRuntime, runLogger)
	taskRuntime.SetFeedbackSink(orch)
	sessionRuntime.SetFeedbackSink(orch)

	pmFactory := pmagent.NewFactory()
	architectFactory := architectagent.NewFactory()
	coderFactory := coderagent.NewFactory()
	testerFactory := testeragent.NewFactory()
	taskRuntime.RegisterTemplate(core.AgentRolePM, pmFactory)
	taskRuntime.RegisterTemplate(core.AgentRoleArchitect, architectFactory)
	taskRuntime.RegisterTemplate(core.AgentRoleCoder, coderFactory)
	taskRuntime.RegisterTemplate(core.AgentRoleTester, testerFactory)

	runManager := orchestrator.NewRunManager(pipelineRegistry, runRepo, sessionRuntime, orch, projectsRoot, runLogger)

	return &Bootstrap{
		Modules: Modules{
			RunManager:     runManager,
			SessionRuntime: sessionRuntime,
			MessageGateway: messageGateway,
			TaskRuntime:    taskRuntime,
		},
		Internals: Internals{
			PipelineRegistry: pipelineRegistry,
			Orchestrator:     orch,
			RunRepository:    runRepo,
			TaskRepository:   taskRepo,
		},
	}
}
