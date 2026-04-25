package app

import (
	"devflow/internal/core"
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
	sessionRuntime := runtime.NewInMemorySessionRuntime(runtime.NewCEOAgentFactory(), runLogger)

	taskRuntime := runtime.NewTaskRuntime(4, nil, runLogger)
	messageGateway := runtime.NewMessageGateway(taskRuntime, runLogger)
	taskRuntime.SetBinder(messageGateway)
	orch := orchestrator.NewService(pipelineRegistry, runRepo, taskRepo, taskRuntime, messageGateway, runLogger)
	taskRuntime.SetFeedbackSink(orch)

	pmFactory := runtime.NewPMAgentFactory()
	architectFactory := runtime.NewArchitectAgentFactory()
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
