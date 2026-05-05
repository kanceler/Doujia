package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/app"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/orchestrator"
	"devflow/internal/runtime"
)

const defaultRequirementText = "帮我写一个单机版本的贪吃蛇游戏吧"

type commonOptions struct {
	DBPath               string
	ProjectsRoot         string
	RunID                string
	PipelineRegistryPath string
	PipelineID           string
	AgentMode            string
	AgentPluginRoots     string
	Requirement          string
	FeedbackPath         string
	TaskID               string
	Timeout              time.Duration
	Reset                bool
	MaxCoderAgents       int
	MaxTesterAgents      int
	AllowParallel        bool
}

type stepSummary struct {
	RunID         core.RunID     `json:"run_id"`
	RunStatus     core.RunStatus `json:"run_status"`
	AppliedTaskID core.TaskID    `json:"applied_task_id,omitempty"`
	NextTask      *stepTask      `json:"next_task,omitempty"`
	Tasks         []stepTask     `json:"tasks"`
}

type agentStepSummary struct {
	RunID        core.RunID        `json:"run_id"`
	TaskID       core.TaskID       `json:"task_id"`
	FeedbackPath string            `json:"feedback_path"`
	Feedback     core.TaskMetaData `json:"feedback"`
}

type stepTask struct {
	TaskID       core.TaskID          `json:"task_id"`
	StageID      core.StageID         `json:"stage_id,omitempty"`
	AgentRole    core.AgentRole       `json:"agent_role,omitempty"`
	AgentID      core.AgentID         `json:"agent_id,omitempty"`
	Op           string               `json:"op,omitempty"`
	Status       core.TaskStatus      `json:"status"`
	Result       core.TaskResultCode  `json:"result,omitempty"`
	DependsOnIDs []core.TaskID        `json:"depends_on_ids,omitempty"`
	ArtifactURIs []string             `json:"artifact_uris,omitempty"`
	InputBagIDs  []string             `json:"input_bag_ids,omitempty"`
	InputBags    []core.BagBindingRef `json:"input_bags,omitempty"`
}

func main() {
	if err := runWithArgs(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "doujiagit-step-runner: %v\n", err)
		os.Exit(1)
	}
}

func runWithArgs(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand is required: init, next, agent, apply")
	}
	command := args[0]
	opts, err := parseOptions(command, args[1:])
	if err != nil {
		return err
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	switch command {
	case "init":
		return runInit(ctx, opts, out)
	case "next":
		return runNext(ctx, opts, out)
	case "agent":
		return runAgent(ctx, opts, out)
	case "apply":
		return runApply(ctx, opts, out)
	default:
		return fmt.Errorf("unknown subcommand %q: use init, next, agent, apply", command)
	}
}

func parseOptions(command string, args []string) (commonOptions, error) {
	opts := commonOptions{
		PipelineID:      "pipeline_full_delivery",
		AgentMode:       "protocolmock",
		Requirement:     defaultRequirementText,
		Timeout:         5 * time.Minute,
		MaxCoderAgents:  1,
		MaxTesterAgents: 1,
		AllowParallel:   false,
	}
	fs := flag.NewFlagSet("doujiagit-step-runner "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "SQLite state database path")
	fs.StringVar(&opts.ProjectsRoot, "projects", "", "projects/workspaces root")
	fs.StringVar(&opts.RunID, "run", "", "run id")
	fs.StringVar(&opts.PipelineRegistryPath, "pipeline-registry", "", "PipelineSpec JSON registry path")
	fs.StringVar(&opts.PipelineID, "pipeline", opts.PipelineID, "pipeline id")
	fs.StringVar(&opts.AgentMode, "agent-mode", opts.AgentMode, "agent mode: protocolmock or real")
	fs.StringVar(&opts.AgentPluginRoots, "agent-plugin-roots", "", "optional plugin roots separated by OS path-list separator")
	fs.StringVar(&opts.Requirement, "requirement", opts.Requirement, "requirement text used by init")
	fs.StringVar(&opts.TaskID, "task", "", "task id for agent")
	fs.StringVar(&opts.FeedbackPath, "feedback", "", "feedback JSON path for agent/apply")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "command timeout")
	fs.BoolVar(&opts.Reset, "reset", false, "remove db and run workspace before init")
	fs.IntVar(&opts.MaxCoderAgents, "max-coder-agents", opts.MaxCoderAgents, "maximum coder agents")
	fs.IntVar(&opts.MaxTesterAgents, "max-tester-agents", opts.MaxTesterAgents, "maximum tester agents")
	fs.BoolVar(&opts.AllowParallel, "parallel", opts.AllowParallel, "allow parallel work")
	if err := fs.Parse(args); err != nil {
		return commonOptions{}, err
	}
	if strings.TrimSpace(opts.DBPath) == "" {
		return commonOptions{}, fmt.Errorf("-db is required")
	}
	if strings.TrimSpace(opts.ProjectsRoot) == "" {
		return commonOptions{}, fmt.Errorf("-projects is required")
	}
	if strings.TrimSpace(opts.RunID) == "" {
		return commonOptions{}, fmt.Errorf("-run is required")
	}
	opts.DBPath = filepath.Clean(opts.DBPath)
	opts.ProjectsRoot = filepath.Clean(opts.ProjectsRoot)
	opts.PipelineRegistryPath = strings.TrimSpace(opts.PipelineRegistryPath)
	return opts, nil
}

func runInit(ctx context.Context, opts commonOptions, out io.Writer) error {
	if err := validateRunID(opts.RunID); err != nil {
		return err
	}
	runDir := filepath.Join(opts.ProjectsRoot, opts.RunID)
	if opts.Reset {
		if err := removeSQLiteDBFiles(opts.DBPath); err != nil {
			return err
		}
		if err := os.RemoveAll(runDir); err != nil {
			return err
		}
	}
	bootstrap, db, err := openBootstrap(ctx, opts)
	if err != nil {
		return err
	}
	defer db.Close()

	pipelineID := core.PipelineID(strings.TrimSpace(opts.PipelineID))
	config, err := stepRunConfig(opts)
	if err != nil {
		return err
	}
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, core.RunID(opts.RunID), pipelineID, config); err != nil {
		return err
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, core.RunID(opts.RunID)); err != nil {
		return err
	}
	feedback, err := seedRequirementFeedback(ctx, bootstrap, core.RunID(opts.RunID), runDir, opts.Requirement)
	if err != nil {
		return err
	}
	if err := manualOrchestrator(bootstrap).OnFeedback(ctx, feedback); err != nil {
		return err
	}
	return writeSummary(ctx, bootstrap, core.RunID(opts.RunID), "", out)
}

func runNext(ctx context.Context, opts commonOptions, out io.Writer) error {
	bootstrap, db, err := openBootstrap(ctx, opts)
	if err != nil {
		return err
	}
	defer db.Close()
	return writeSummary(ctx, bootstrap, core.RunID(opts.RunID), "", out)
}

func runApply(ctx context.Context, opts commonOptions, out io.Writer) error {
	if strings.TrimSpace(opts.FeedbackPath) == "" {
		return fmt.Errorf("-feedback is required for apply")
	}
	bootstrap, db, err := openBootstrap(ctx, opts)
	if err != nil {
		return err
	}
	defer db.Close()
	feedback, err := readFeedback(opts.FeedbackPath)
	if err != nil {
		return err
	}
	if feedback.RunID != core.RunID(opts.RunID) {
		return fmt.Errorf("feedback run_id %q does not match -run %q", feedback.RunID, opts.RunID)
	}
	if err := manualOrchestrator(bootstrap).OnFeedback(ctx, feedback); err != nil {
		return err
	}
	return writeSummary(ctx, bootstrap, core.RunID(opts.RunID), feedback.TaskID, out)
}

func runAgent(ctx context.Context, opts commonOptions, out io.Writer) error {
	if strings.EqualFold(strings.TrimSpace(opts.AgentMode), "real") || strings.EqualFold(strings.TrimSpace(opts.AgentMode), "llm") {
		if err := validateRealAgentEnv(os.Getenv); err != nil {
			return err
		}
	}
	if strings.TrimSpace(opts.TaskID) == "" {
		return fmt.Errorf("-task is required for agent")
	}
	bootstrap, db, err := openBootstrap(ctx, opts)
	if err != nil {
		return err
	}
	defer db.Close()
	run, err := bootstrap.Internals.RunRepository.Get(ctx, core.RunID(opts.RunID))
	if err != nil {
		return err
	}
	task, err := bootstrap.Internals.TaskRepository.Get(ctx, core.RunID(opts.RunID), core.TaskID(opts.TaskID))
	if err != nil {
		return err
	}
	if task.Status != core.TaskStatusDispatched {
		return fmt.Errorf("task %s status is %s, want dispatched", task.ID, task.Status)
	}

	sink := newCaptureSink()
	bootstrap.Modules.TaskRuntime.SetFeedbackSink(sink)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(interface{ SetFeedbackSink(runtime.FeedbackSink) }); ok {
		sessionRuntime.SetFeedbackSink(sink)
	}
	dispatch := taskDispatchMeta(run.ID, task)
	if task.AgentRole == core.AgentRoleCEO {
		if _, err := bootstrap.Modules.SessionRuntime.CreateSession(ctx, run); err != nil {
			return err
		}
		if err := bootstrap.Modules.SessionRuntime.DispatchToSession(ctx, dispatch); err != nil {
			return err
		}
		feedback, err := sink.Wait(ctx)
		if err != nil {
			return err
		}
		return writeAgentFeedback(run.ID, task.ID, opts.FeedbackPath, run.ProjectDir, feedback, out)
	}

	if _, err := bootstrap.Modules.TaskRuntime.EnsureAgent(ctx, runtime.EnsureAgentRequest{
		RunID:       run.ID,
		Role:        task.AgentRole,
		AgentID:     task.AgentID,
		ProjectRoot: run.ProjectDir,
		RunConfig:   run.Config,
	}); err != nil {
		return err
	}
	if err := bootstrap.Modules.MessageGateway.Dispatch(ctx, dispatch); err != nil {
		return err
	}
	feedback, err := sink.Wait(ctx)
	if err != nil {
		return err
	}
	return writeAgentFeedback(run.ID, task.ID, opts.FeedbackPath, run.ProjectDir, feedback, out)
}

func writeAgentFeedback(runID core.RunID, taskID core.TaskID, requestedPath string, projectDir string, feedback core.TaskMetaData, out io.Writer) error {
	feedbackPath := strings.TrimSpace(requestedPath)
	if feedbackPath == "" {
		feedbackPath = filepath.Join(projectDir, "step_feedback", sanitizePathPart(string(taskID))+".feedback.json")
	}
	if err := writeFeedback(feedbackPath, feedback); err != nil {
		return err
	}
	return writeJSON(out, agentStepSummary{
		RunID:        runID,
		TaskID:       taskID,
		FeedbackPath: feedbackPath,
		Feedback:     feedback,
	})
}

func openBootstrap(ctx context.Context, opts commonOptions) (*app.Bootstrap, interface{ Close() error }, error) {
	bootstrap, db, err := app.NewSQLiteBootstrapWithOptions(ctx, opts.ProjectsRoot, opts.DBPath, app.BootstrapOptions{
		PipelineRegistryPath: opts.PipelineRegistryPath,
		AgentMode:            opts.AgentMode,
		AgentPluginRoots:     filepath.SplitList(opts.AgentPluginRoots),
		TaskWorkerCount:      1,
	})
	if err != nil {
		return nil, nil, err
	}
	return bootstrap, db, nil
}

func manualOrchestrator(bootstrap *app.Bootstrap) *orchestrator.Service {
	service := orchestrator.NewService(
		bootstrap.Internals.PipelineRegistry,
		bootstrap.Internals.RunRepository,
		bootstrap.Internals.TaskRepository,
		noopProvisioner{},
		noopDispatcher{},
		nil,
		nil,
	)
	service.SetPipelineInstanceRepository(bootstrap.Internals.InstanceRepository)
	service.SetPipelineDefinitionRegistry(bootstrap.Internals.PipelineDefinitions)
	service.SetArtifactRepository(bootstrap.Internals.ArtifactRepository)
	service.SetEventRepository(bootstrap.Internals.EventRepository)
	service.SetDoujiaGitRepository(bootstrap.Internals.DoujiaGitRepository)
	return service
}

type noopProvisioner struct{}

func (noopProvisioner) EnsureAgent(context.Context, runtime.EnsureAgentRequest) (runtime.EnsureAgentResult, error) {
	return runtime.EnsureAgentResult{RuntimeID: "manual"}, nil
}

type noopDispatcher struct{}

func (noopDispatcher) Dispatch(context.Context, core.TaskMetaData) error {
	return nil
}

type captureSink struct {
	ch chan core.TaskMetaData
}

func newCaptureSink() *captureSink {
	return &captureSink{ch: make(chan core.TaskMetaData, 1)}
}

func (s *captureSink) OnFeedback(_ context.Context, feedback core.TaskMetaData) error {
	s.ch <- feedback
	return nil
}

func (s *captureSink) Wait(ctx context.Context) (core.TaskMetaData, error) {
	select {
	case feedback := <-s.ch:
		return feedback, nil
	case <-ctx.Done():
		return core.TaskMetaData{}, fmt.Errorf("wait for agent feedback: %w", ctx.Err())
	}
}

func seedRequirementFeedback(ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, runDir string, requirementText string) (core.TaskMetaData, error) {
	requirementText = strings.TrimSpace(requirementText)
	if requirementText == "" {
		requirementText = defaultRequirementText
	}
	outputRef := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "ceo", "artifacts", "requirement", "requirement_v1.md"))
	store := &artifact.ScopedLocalStore{
		RunRoot:       runDir,
		WorkspaceRoot: filepath.Join(runDir, "agents", "ceo"),
	}
	if err := store.Write(ctx, outputRef, []byte(requirementText+"\n")); err != nil {
		return core.TaskMetaData{}, err
	}
	committer := runtime.NewDoujiaGitOutputCommitter(bootstrap.Internals.DoujiaGitRepository, store)
	return runtime.CommitAgentFeedbackOutputs(ctx, committer, runID, "ceo", core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	}, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "ceo_write_requirement",
		AgentID:      "ceo",
		Op:           core.TaskOpWritePlan,
		ArtifactURIs: []string{outputRef},
		Result:       core.TaskResultCodeOK,
		Outputs: []core.AgentOutput{
			{LogicalKey: "requirement", ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "requirement",
				Members: []core.ProducedBagMember{
					{LogicalKey: "requirement"},
				},
			},
		},
	})
}

func writeSummary(ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, appliedTaskID core.TaskID, out io.Writer) error {
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return err
	}
	tasks, err := bootstrap.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		return err
	}
	stepTasks := make([]stepTask, 0, len(tasks))
	for _, task := range tasks {
		stepTasks = append(stepTasks, toStepTask(task))
	}
	next := nextTask(stepTasks)
	return writeJSON(out, stepSummary{
		RunID:         run.ID,
		RunStatus:     run.Status,
		AppliedTaskID: appliedTaskID,
		NextTask:      next,
		Tasks:         stepTasks,
	})
}

func nextTask(tasks []stepTask) *stepTask {
	for _, status := range []core.TaskStatus{core.TaskStatusDispatched, core.TaskStatusWaitingExternal, core.TaskStatusPending, core.TaskStatusFailed, core.TaskStatusBlocked} {
		for i := range tasks {
			if tasks[i].Status == status {
				task := tasks[i]
				return &task
			}
		}
	}
	return nil
}

func toStepTask(task core.Task) stepTask {
	return stepTask{
		TaskID:       task.ID,
		StageID:      task.StageID,
		AgentRole:    task.AgentRole,
		AgentID:      task.AgentID,
		Op:           task.Op,
		Status:       task.Status,
		Result:       task.Result,
		DependsOnIDs: append([]core.TaskID(nil), task.DependsOnIDs...),
		ArtifactURIs: artifactRefsToStrings(task.InputArtifactRefs),
		InputBagIDs:  append([]string(nil), task.InputBagIDs...),
		InputBags:    append([]core.BagBindingRef(nil), task.InputBags...),
	}
}

func taskDispatchMeta(runID core.RunID, task core.Task) core.TaskMetaData {
	return core.TaskMetaData{
		Direction:     core.TaskDirectionDispatch,
		RunID:         runID,
		TaskID:        task.ID,
		ParentID:      task.ParentID,
		DependsOnIDs:  append([]core.TaskID(nil), task.DependsOnIDs...),
		AgentID:       task.AgentID,
		Op:            task.Op,
		ArtifactURIs:  artifactRefsToStrings(task.InputArtifactRefs),
		InputBagIDs:   append([]string(nil), task.InputBagIDs...),
		InputBags:     append([]core.BagBindingRef(nil), task.InputBags...),
		ExecutionMode: task.ExecutionMode,
	}
}

func artifactRefsToStrings(refs []core.ArtifactRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(string(ref)) != "" {
			out = append(out, string(ref))
		}
	}
	return out
}

func stepRunConfig(opts commonOptions) (core.RunConfig, error) {
	if opts.MaxCoderAgents <= 0 {
		return core.RunConfig{}, fmt.Errorf("-max-coder-agents must be greater than zero")
	}
	if opts.MaxTesterAgents <= 0 {
		return core.RunConfig{}, fmt.Errorf("-max-tester-agents must be greater than zero")
	}
	return core.RunConfig{
		Delivery: core.DeliveryConfig{
			MaxCoderAgents:         opts.MaxCoderAgents,
			MaxTesterAgents:        opts.MaxTesterAgents,
			RequireTesterPerModule: true,
			AllowParallelWork:      opts.AllowParallel,
			Git: core.GitRunConfig{
				MainBranch: "main",
			},
		},
	}, nil
}

func readFeedback(path string) (core.TaskMetaData, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	var feedback core.TaskMetaData
	if err := json.Unmarshal(body, &feedback); err != nil {
		return core.TaskMetaData{}, err
	}
	if feedback.Direction != core.TaskDirectionFeedback {
		return core.TaskMetaData{}, fmt.Errorf("feedback file %s has direction %q, want feedback", path, feedback.Direction)
	}
	return feedback, nil
}

func writeFeedback(path string, feedback core.TaskMetaData) error {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(feedback)
}

func writeJSON(out io.Writer, payload any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func validateRunID(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("-run is required")
	}
	if runID == "." || runID == ".." || strings.ContainsAny(runID, `/\`) {
		return fmt.Errorf("-run must be a simple run id, got %q", runID)
	}
	return nil
}

func removeSQLiteDBFiles(dbPath string) error {
	info, err := os.Stat(dbPath)
	if err == nil && info.IsDir() {
		return fmt.Errorf("refusing to remove directory passed as -db: %s", dbPath)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func validateRealAgentEnv(getenv func(string) string) error {
	if getenv == nil {
		getenv = os.Getenv
	}
	required := []string{"OPENAI_API_KEY", "OPENAI_MODEL", "OPENAI_BASE_URL"}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("real agent mode requires %s", strings.Join(missing, ", "))
	}
	return nil
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "task"
	}
	return out
}
