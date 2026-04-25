package app

import (
	architectagent "devflow/internal/agent/architect"
	ceoagent "devflow/internal/agent/ceo"
	pmagent "devflow/internal/agent/pm"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
	"devflow/internal/orchestrator"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
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
	pipelineRegistry := pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne())
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	runLogger := logging.NewFileRunLogger(projectsRoot)
	agentConfig := runtime.AgentRuntimeConfig{
		ArtifactStoreFactory: func(init runtime.AgentInit) (artifact.Store, error) {
			return &artifact.ScopedLocalStore{
				RunRoot:       init.RunRoot,
				WorkspaceRoot: init.WorkspacePath,
			}, nil
		},
		LLMClientFactory: func(runtime.AgentInit) (llm.Client, error) {
			cfg, ok, err := llm.LoadOptionalConfigFromEnv()
			if err != nil {
				return nil, err
			}
			if !ok {
				return llm.NoopClient{}, nil
			}
			return llm.BuildClient(cfg)
		},
		Logger: runLogger,
	}
	sessionRuntime := runtime.NewInMemorySessionRuntime(ceoagent.NewFactory(), runLogger, agentConfig)

	taskRuntime := runtime.NewTaskRuntime(4, nil, runLogger, agentConfig)
	messageGateway := runtime.NewMessageGateway(taskRuntime, runLogger)
	taskRuntime.SetBinder(messageGateway)
	orch := orchestrator.NewService(pipelineRegistry, runRepo, taskRepo, taskRuntime, messageGateway, runLogger)
	taskRuntime.SetFeedbackSink(orch)

	pmFactory := pmagent.NewFactory()
	architectFactory := architectagent.NewFactory()
	taskRuntime.RegisterTemplate(core.AgentRolePM, pmFactory)
	taskRuntime.RegisterTemplate(core.AgentRoleArchitect, architectFactory)

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
