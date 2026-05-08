package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"

	agentbootstrap "devflow/internal/agent/bootstrap"
	"devflow/internal/agent/llm"
	protocolmock "devflow/internal/agent/protocolmock"
	agentreal "devflow/internal/agent/real"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
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
	PipelineRegistry          pipeline.Registry
	PipelineDefinitions       pipeline.DefinitionRegistry
	PipelineCatalog           *agentbootstrap.MutablePipelineCatalog
	PluginRegistry            *agentbootstrap.LiveRegistry
	Orchestrator              *orchestrator.Service
	ProjectRepository         repo.ProjectRepository
	RunRepository             repo.RunRepository
	RunIterationRepository    repo.RunIterationRepository
	SessionMessageRepository  repo.SessionMessageRepository
	SessionArtifactRepository repo.SessionArtifactRepository
	ImprovementItemRepository repo.ImprovementItemRepository
	InstanceRepository        repo.PipelineInstanceRepository
	TaskRepository            repo.TaskRepository
	ArtifactRepository        repo.ArtifactRepository
	EventRepository           repo.EventRepository
	DoujiaGitRepository       doujiagit.Repository
}

type Bootstrap struct {
	Modules   Modules
	Internals Internals
	doujiaLLMFactory func(config core.LLMRunConfig) llm.Adapter
}

type BootstrapOptions struct {
	PipelineRegistryPath string
	AgentMode            string
	AgentPluginRoots     []string
	TaskWorkerCount      int
	DoujiaLLMFactory     func(config core.LLMRunConfig) llm.Adapter
}

func NewBootstrap(projectsRoot string) *Bootstrap {
	bootstrap, err := NewBootstrapWithOptions(projectsRoot, BootstrapOptions{})
	if err != nil {
		panic(err)
	}
	return bootstrap
}

func NewBootstrapWithOptions(projectsRoot string, options BootstrapOptions) (*Bootstrap, error) {
	runRepo := repo.NewMemoryRunRepository()
	projectRepo := repo.NewMemoryProjectRepository()
	iterationRepo := repo.NewMemoryRunIterationRepository()
	sessionMessageRepo := repo.NewMemorySessionMessageRepository()
	sessionArtifactRepo := repo.NewMemorySessionArtifactRepository()
	improvementItemRepo := repo.NewMemoryImprovementItemRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	artifactRepo := repo.NewMemoryArtifactRepository()
	eventRepo := repo.NewMemoryEventRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	return newBootstrap(projectsRoot, projectRepo, runRepo, iterationRepo, sessionMessageRepo, sessionArtifactRepo, improvementItemRepo, instanceRepo, taskRepo, artifactRepo, eventRepo, doujiaGitRepo, options)
}

func NewSQLiteBootstrap(ctx context.Context, projectsRoot string, dbPath string) (*Bootstrap, *sql.DB, error) {
	return NewSQLiteBootstrapWithOptions(ctx, projectsRoot, dbPath, BootstrapOptions{})
}

func NewSQLiteBootstrapWithOptions(ctx context.Context, projectsRoot string, dbPath string, options BootstrapOptions) (*Bootstrap, *sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(dbPath)), 0o755); err != nil {
		return nil, nil, err
	}
	repos, db, err := repo.OpenSQLiteRepositories(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}
	doujiaGitRepo := doujiagit.NewSQLiteRepository(db)
	if err := doujiaGitRepo.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	bootstrap, err := newBootstrap(projectsRoot, repos.Projects, repos.Runs, repos.Iterations, repos.SessionMessages, repos.SessionArtifacts, repos.ImprovementItems, repos.Instances, repos.Tasks, repos.Artifacts, repos.Events, doujiaGitRepo, options)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return bootstrap, db, nil
}

func newBootstrap(
	projectsRoot string,
	projectRepo repo.ProjectRepository,
	runRepo repo.RunRepository,
	runIterationRepo repo.RunIterationRepository,
	sessionMessageRepo repo.SessionMessageRepository,
	sessionArtifactRepo repo.SessionArtifactRepository,
	improvementItemRepo repo.ImprovementItemRepository,
	instanceRepo repo.PipelineInstanceRepository,
	taskRepo repo.TaskRepository,
	artifactRepo repo.ArtifactRepository,
	eventRepo repo.EventRepository,
	doujiaGitRepo doujiagit.Repository,
	options BootstrapOptions,
) (*Bootstrap, error) {
	if absRoot, err := filepath.Abs(filepath.Clean(projectsRoot)); err == nil {
		projectsRoot = absRoot
	}
	pipelineRegistry, pipelineDefinitions, err := buildPipelineRegistries(options)
	if err != nil {
		return nil, err
	}
	pipelineCatalog := agentbootstrap.NewMutablePipelineCatalog(pipelineRegistry, pipelineDefinitions)
	runLogger := logging.NewFileRunLogger(projectsRoot)
	agentConfig := runtime.AgentRuntimeConfig{
		ArtifactStoreFactory: func(init runtime.AgentInit) (artifact.Store, error) {
			return &artifact.ScopedLocalStore{
				RunRoot:       init.RunRoot,
				WorkspaceRoot: init.WorkspacePath,
			}, nil
		},
		OutputCommitterFactory: func(init runtime.AgentInit, store artifact.Store) (runtime.AgentOutputCommitter, error) {
			if doujiaGitRepo == nil || store == nil {
				return nil, nil
			}
			return runtime.NewDoujiaGitOutputCommitter(doujiaGitRepo, store), nil
		},
		Logger: runLogger,
	}

	runtimeOptions := agentbootstrap.RuntimeOptions{PluginRoots: options.AgentPluginRoots}
	agentPlugins, err := agentbootstrap.NewPluginRegistryWithOptions(runtimeOptions)
	if err != nil {
		return nil, err
	}
	pluginRegistry, err := agentbootstrap.NewLiveRegistry(
		agentPlugins,
		pipelineCatalog,
		agentbootstrap.NewDefaultDriverResolver(),
		filepath.Join(projectsRoot, ".plugins"),
	)
	if err != nil {
		return nil, err
	}
	var runtimePlan agentRuntimePlan
	switch mode := strings.ToLower(strings.TrimSpace(options.AgentMode)); mode {
	case "", "default", "protocolmock", "test":
		runtimePlan, err = buildAgentRuntimePlan(agentPlugins.RoleSpecs(), func(role core.AgentRole) runtime.Agent {
			return protocolmock.NewFactory()
		})
		if err != nil {
			return nil, err
		}
	case "real", "llm":
		runtimePlan, err = buildAgentRuntimePlan(agentPlugins.RoleSpecs(), func(role core.AgentRole) runtime.Agent {
			return agentreal.NewFactoryWithRuntimeOptions(role, runtimeOptions)
		})
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported agent mode %q; supported modes are protocolmock and real", options.AgentMode)
	}

	sessionRuntime := runtime.NewInMemorySessionRuntime(runtimePlan.sessionTemplate, runLogger, agentConfig)

	taskWorkerCount := options.TaskWorkerCount
	if taskWorkerCount <= 0 {
		taskWorkerCount = 4
	}
	taskRuntime := runtime.NewTaskRuntime(taskWorkerCount, nil, runLogger, agentConfig)
	messageGateway := runtime.NewMessageGateway(taskRuntime, runLogger)
	inputResolver := runtime.NewDoujiaGitInputResolver(doujiaGitRepo)
	messageGateway.SetInputBundleResolver(inputResolver)
	sessionRuntime.SetInputBundleResolver(inputResolver)
	taskRuntime.SetBinder(messageGateway)
	orch := orchestrator.NewService(pipelineCatalog, runRepo, taskRepo, taskRuntime, messageGateway, sessionRuntime, runLogger)
	orch.SetSessionRoles(runtimePlan.sessionRoles)
	orch.SetRunIterationRepository(runIterationRepo)
	orch.SetPipelineInstanceRepository(instanceRepo)
	orch.SetPipelineDefinitionRegistry(pipelineCatalog)
	orch.SetArtifactRepository(artifactRepo)
	orch.SetEventRepository(eventRepo)
	orch.SetDoujiaGitRepository(doujiaGitRepo)
	taskRuntime.SetFeedbackSink(orch)
	sessionRuntime.SetFeedbackSink(orch)

	for role, factory := range runtimePlan.taskFactories {
		taskRuntime.RegisterTemplate(role, factory)
	}

	runManager := orchestrator.NewRunManager(pipelineCatalog, runRepo, taskRepo, runIterationRepo, sessionMessageRepo, sessionArtifactRepo, improvementItemRepo, sessionRuntime, orch, projectsRoot, runLogger)
	doujiaLLMFactory := options.DoujiaLLMFactory
	if doujiaLLMFactory == nil {
		doujiaLLMFactory = func(config core.LLMRunConfig) llm.Adapter {
			adapter := &llm.OpenAIAdapter{}
			adapter.ApplyConfig(llm.OpenAIAdapterConfig{
				BaseURL:  config.BaseURL,
				APIKey:   config.APIKey,
				Model:    config.Model,
				APIStyle: config.APIStyle,
			})
			return adapter
		}
	}

	return &Bootstrap{
		Modules: Modules{
			RunManager:     runManager,
			SessionRuntime: sessionRuntime,
			MessageGateway: messageGateway,
			TaskRuntime:    taskRuntime,
		},
		Internals: Internals{
			PipelineRegistry:          pipelineCatalog,
			PipelineDefinitions:       pipelineCatalog,
			PipelineCatalog:           pipelineCatalog,
			PluginRegistry:            pluginRegistry,
			Orchestrator:              orch,
			ProjectRepository:         projectRepo,
			RunRepository:             runRepo,
			RunIterationRepository:    runIterationRepo,
			SessionMessageRepository:  sessionMessageRepo,
			SessionArtifactRepository: sessionArtifactRepo,
			ImprovementItemRepository: improvementItemRepo,
			InstanceRepository:        instanceRepo,
			TaskRepository:            taskRepo,
			ArtifactRepository:        artifactRepo,
			EventRepository:           eventRepo,
			DoujiaGitRepository:       doujiaGitRepo,
		},
		doujiaLLMFactory: doujiaLLMFactory,
	}, nil
}

func buildPipelineRegistries(options BootstrapOptions) (pipeline.Registry, pipeline.DefinitionRegistry, error) {
	registryPath := strings.TrimSpace(options.PipelineRegistryPath)
	if registryPath == "" {
		registry, definitions, err := buildBundledPipelineRegistries()
		if err == nil {
			return registry, definitions, nil
		}
		return pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne(), pipeline.BuiltinPhaseTwo()), nil, nil
	}
	registryPath = filepath.Clean(registryPath)
	spec, err := pipeline.LoadRegistrySpec(registryPath)
	if err != nil {
		return nil, nil, err
	}
	definitions, err := pipeline.NewJSONRegistry(spec)
	if err != nil {
		return nil, nil, err
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("compile JSON pipeline registry %s for current orchestrator: %w", registryPath, err)
	}
	return legacyRegistry, definitions, nil
}

func buildBundledPipelineRegistries() (pipeline.Registry, pipeline.DefinitionRegistry, error) {
	spec, err := pipeline.LoadRegistrySpec(bundledPipelineRegistryPath())
	if err != nil {
		return nil, nil, err
	}
	entryDef, ok := spec.Pipeline(spec.EntryPipelineID)
	if !ok {
		return nil, nil, fmt.Errorf("entry pipeline %q not found in bundled registry", spec.EntryPipelineID)
	}
	phaseTwoAlias := entryDef
	phaseTwoAlias.PipelineID = pipeline.PipelineIDPhaseTwo
	phaseTwoAlias.Name = "Phase Two Delivery Flow"
	spec.PipelineDefs = append(spec.PipelineDefs, phaseTwoAlias)

	definitions, err := pipeline.NewJSONRegistry(spec)
	if err != nil {
		return nil, nil, err
	}
	entryLegacy, err := pipeline.CompileLegacyPipelineDefPrefix(entryDef)
	if err != nil {
		return nil, nil, err
	}
	phaseTwoLegacy, err := pipeline.CompileLegacyPipelineDefPrefix(phaseTwoAlias)
	if err != nil {
		return nil, nil, err
	}
	return pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne(), phaseTwoLegacy, entryLegacy), definitions, nil
}

func bundledPipelineRegistryPath() string {
	_, file, _, ok := stdruntime.Caller(0)
	if !ok {
		return filepath.Join("internal", "orchestrator", "testdata", "full_delivery", "pipeline_full_delivery.spec.json")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "orchestrator", "testdata", "full_delivery", "pipeline_full_delivery.spec.json"))
}
