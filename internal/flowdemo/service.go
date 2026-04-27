package flowdemo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	architectagent "devflow/internal/agent/architect"
	ceoagent "devflow/internal/agent/ceo"
	coderagent "devflow/internal/agent/coder"
	pmagent "devflow/internal/agent/pm"
	testeragent "devflow/internal/agent/tester"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	runtimepkg "devflow/internal/runtime"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusOK      Status = "ok"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

type Step struct {
	Index      int      `json:"index"`
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Agent      string   `json:"agent"`
	Op         string   `json:"op"`
	Status     Status   `json:"status"`
	Result     string   `json:"result,omitempty"`
	Inputs     []string `json:"inputs,omitempty"`
	Outputs    []string `json:"outputs,omitempty"`
	Error      string   `json:"error,omitempty"`
	StartedAt  string   `json:"started_at,omitempty"`
	FinishedAt string   `json:"finished_at,omitempty"`
}

type Run struct {
	ID          string `json:"id"`
	Status      Status `json:"status"`
	Root        string `json:"root"`
	ProjectRepo string `json:"project_repo,omitempty"`
	GitBranch   string `json:"git_branch,omitempty"`
	GitCommit   string `json:"git_commit,omitempty"`
	Steps       []Step `json:"steps"`
	CreatedAt   string `json:"created_at"`
	FinishedAt  string `json:"finished_at,omitempty"`
}

type ArtifactContent struct {
	URI     string `json:"uri"`
	Content string `json:"content"`
}

type Service struct {
	root      string
	llmConfig llm.Config
	llmClient llm.Client
	mu        sync.RWMutex
	runs      map[string]*Run
}

func NewService(root string) *Service {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join("runtime", "flowdemo")
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	llmConfig, hasLLMConfig, err := llm.LoadOptionalConfigFromEnv()
	llmClient := llm.Client(llm.NoopClient{})
	if err == nil && hasLLMConfig {
		if client, buildErr := llm.BuildClient(llmConfig); buildErr == nil {
			llmClient = client
		}
	}
	return &Service{
		root:      root,
		llmConfig: llmConfig,
		llmClient: llmClient,
		runs:      make(map[string]*Run),
	}
}

func (s *Service) Execute(ctx context.Context, filename string, content []byte) (*Run, error) {
	runID := core.RunID(fmt.Sprintf("flowdemo_%s", time.Now().UTC().Format("20060102_150405_000000000")))
	runRoot := filepath.Join(s.root, string(runID))
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		return nil, err
	}
	run := &Run{
		ID:        string(runID),
		Status:    StatusRunning,
		Root:      filepath.Clean(runRoot),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.mu.Lock()
	s.runs[run.ID] = run
	s.mu.Unlock()

	exec := newExecutor(runID, runRoot, s.llmConfig, s.llmClient)
	if useFakeOpenCode() {
		if err := exec.installFakeOpenCode(); err != nil {
			run.Status = StatusFailed
			return run, err
		}
		defer exec.restoreOpenCodeEnv()
	}

	if err := exec.run(ctx, run, filename, content); err != nil {
		run.Status = StatusFailed
		run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		return run, err
	}
	run.ProjectRepo = filepath.Clean(filepath.Join(runRoot, "project_repo"))
	run.GitBranch = gitValue(ctx, run.ProjectRepo, "rev-parse", "--abbrev-ref", "HEAD")
	run.GitCommit = gitValue(ctx, run.ProjectRepo, "rev-parse", "HEAD")
	run.Status = StatusOK
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return cloneRun(run), nil
}

func (s *Service) GetRun(id string) (*Run, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	return cloneRun(run), true
}

func (s *Service) ListRuns() []*Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Run, 0, len(s.runs))
	for _, run := range s.runs {
		out = append(out, cloneRun(run))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

func (s *Service) ReadArtifact(runID, uri string) (ArtifactContent, error) {
	run, ok := s.GetRun(runID)
	if !ok {
		return ArtifactContent{}, fmt.Errorf("run %q not found", runID)
	}
	store := &artifact.ScopedLocalStore{RunRoot: run.Root, WorkspaceRoot: run.Root}
	content, err := store.Read(context.Background(), uri)
	if err != nil {
		return ArtifactContent{}, err
	}
	return ArtifactContent{URI: uri, Content: string(content)}, nil
}

type executor struct {
	runID        core.RunID
	runRoot      string
	agents       map[core.AgentID]runtimepkg.Agent
	stepCounter  int
	fakeOpenCode string
	oldOpenCode  string
	hadOpenCode  bool
	llmConfig    llm.Config
	llmClient    llm.Client
}

func newExecutor(runID core.RunID, runRoot string, llmConfig llm.Config, llmClient llm.Client) *executor {
	if llmClient == nil {
		llmClient = llm.NoopClient{}
	}
	return &executor{
		runID:     runID,
		runRoot:   runRoot,
		agents:    make(map[core.AgentID]runtimepkg.Agent),
		llmConfig: llmConfig,
		llmClient: llmClient,
	}
}

func (e *executor) run(ctx context.Context, run *Run, filename string, content []byte) error {
	ceoInputURI, err := e.writeUploadedRequirement(ctx, filename, content)
	if err != nil {
		return err
	}

	ceoPlan := e.execute(ctx, run, "CEO imports requirement", core.AgentRoleCEO, "ceo", "write_plan", []string{ceoInputURI})
	pmPlan := e.execute(ctx, run, "PM writes product plan", core.AgentRolePM, "pm01", "write_plan", ceoPlan.Outputs)
	ceoReview := e.execute(ctx, run, "CEO reviews PM plan", core.AgentRoleCEO, "ceo", "review_plan", pmPlan.Outputs)
	architectPlan := e.execute(ctx, run, "Architect writes design", core.AgentRoleArchitect, "architect01", "write_plan", ceoReview.Outputs)
	pmReviewInputs := append([]string{}, pmPlan.Outputs...)
	pmReviewInputs = append(pmReviewInputs, architectPlan.Outputs...)
	e.execute(ctx, run, "PM reviews architecture", core.AgentRolePM, "pm01", "review_plan", pmReviewInputs)
	e.skip(run, "CEO user confirm", "ceo", "user_confirm", "暂时不需要")

	runConfigURI, err := e.writeRunConfig(ctx)
	if err != nil {
		return err
	}
	splitInputs := append([]string{}, architectPlan.Outputs...)
	splitInputs = append(splitInputs, runConfigURI)
	split := e.execute(ctx, run, "Architect splits modules", core.AgentRoleArchitect, "architect01", "split_module", splitInputs)

	coderControl, testerControl := firstCoderTesterControls(split.feedback)
	coder := e.execute(ctx, run, "Coder writes code", core.AgentRoleCoder, core.AgentID(coderControl.AgentName), "write_code", coderControl.ArtifactURIs)
	testData := e.execute(ctx, run, "Tester writes test data", core.AgentRoleTester, core.AgentID(testerControl.AgentName), "test_data", testerControl.ArtifactURIs)
	architectTestDataInputs := append([]string{}, architectPlan.Outputs...)
	architectTestDataInputs = append(architectTestDataInputs, split.Outputs...)
	e.execute(ctx, run, "Architect writes architecture test data", core.AgentRoleArchitect, "architect01", "test_data", architectTestDataInputs)

	moduleURI := firstURIContaining(coderControl.ArtifactURIs, "/artifacts/modules/")
	testerCodeInputs := []string{moduleURI}
	testerCodeInputs = append(testerCodeInputs, coder.Outputs...)
	testerCodeInputs = append(testerCodeInputs, testData.Outputs...)
	e.execute(ctx, run, "Tester tests code", core.AgentRoleTester, core.AgentID(testerControl.AgentName), "test_code", compactURIs(testerCodeInputs))

	mergeInputs := append([]string{}, split.Outputs...)
	mergeInputs = append(mergeInputs, moduleURI)
	mergeInputs = append(mergeInputs, coder.Outputs...)
	e.execute(ctx, run, "Architect merges code", core.AgentRoleArchitect, "architect01", "merge_code", compactURIs(mergeInputs))

	architectTestInputs := append([]string{}, architectPlan.Outputs...)
	architectTestInputs = append(architectTestInputs, split.Outputs...)
	e.execute(ctx, run, "Architect tests final code", core.AgentRoleArchitect, "architect01", "test_code", compactURIs(architectTestInputs))
	return nil
}

type stepResult struct {
	Step
	feedback core.TaskMetaData
}

func (e *executor) execute(ctx context.Context, run *Run, title string, role core.AgentRole, agentID core.AgentID, op string, inputs []string) stepResult {
	e.stepCounter++
	step := Step{
		Index:     e.stepCounter,
		ID:        fmt.Sprintf("task_%02d", e.stepCounter),
		Title:     title,
		Agent:     string(agentID),
		Op:        op,
		Status:    StatusRunning,
		Inputs:    append([]string(nil), inputs...),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	agent := e.agent(role, agentID)
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        e.runID,
		TaskID:       core.TaskID(step.ID),
		AgentID:      agentID,
		Op:           op,
		ArtifactURIs: compactURIs(inputs),
	}
	feedback, err := agent.Execute(ctx, task)
	step.Result = string(feedback.Result)
	step.Outputs = append([]string(nil), feedback.ArtifactURIs...)
	if len(step.Outputs) == 0 {
		step.Outputs = []string{e.writeStepReport(ctx, step, feedback, err)}
	}
	if err != nil {
		step.Status = StatusFailed
		step.Error = err.Error()
	} else if feedback.Result == core.TaskResultCodeFail {
		step.Status = StatusFailed
		if step.Result == "" {
			step.Result = string(core.TaskResultCodeFail)
		}
	} else {
		step.Status = StatusOK
		if step.Result == "" {
			step.Result = string(core.TaskResultCodeOK)
		}
	}
	step.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	run.Steps = append(run.Steps, step)
	return stepResult{Step: step, feedback: feedback}
}

func (e *executor) skip(run *Run, title, agent, op, reason string) {
	e.stepCounter++
	run.Steps = append(run.Steps, Step{
		Index:      e.stepCounter,
		ID:         fmt.Sprintf("task_%02d", e.stepCounter),
		Title:      title,
		Agent:      agent,
		Op:         op,
		Status:     StatusSkipped,
		Result:     "skipped",
		Error:      reason,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (e *executor) agent(role core.AgentRole, agentID core.AgentID) runtimepkg.Agent {
	if agent, ok := e.agents[agentID]; ok {
		return agent
	}
	workspace := filepath.Join(e.runRoot, "agents", string(agentID))
	_ = os.MkdirAll(workspace, 0o755)
	init := runtimepkg.AgentInit{
		AgentID:       agentID,
		RuntimeID:     core.RuntimeID(agentID),
		RunID:         e.runID,
		RunRoot:       e.runRoot,
		WorkspacePath: workspace,
		RunConfig: core.RunConfig{LLM: core.LLMConfig{
			ProviderType:   string(e.llmConfig.ProviderType),
			BaseURL:        e.llmConfig.BaseURL,
			APIKey:         e.llmConfig.APIKey,
			Model:          e.llmConfig.Model,
			RequestTimeout: e.llmConfig.RequestTimeout,
		}},
	}
	deps := runtimepkg.AgentDeps{
		ArtifactStore: &artifact.ScopedLocalStore{RunRoot: e.runRoot, WorkspaceRoot: workspace},
		LLMClient:     e.llmClient,
	}
	var factory runtimepkg.Agent
	switch role {
	case core.AgentRoleCEO:
		factory = ceoagent.NewFactory()
	case core.AgentRolePM:
		factory = pmagent.NewFactory()
	case core.AgentRoleArchitect:
		factory = architectagent.NewFactory()
	case core.AgentRoleCoder:
		factory = coderagent.NewFactory()
	case core.AgentRoleTester:
		factory = testeragent.NewFactory()
	default:
		panic(fmt.Sprintf("unsupported role %s", role))
	}
	agent := factory.Create(init, deps)
	e.agents[agentID] = agent
	return agent
}

func (e *executor) writeUploadedRequirement(ctx context.Context, filename string, content []byte) (string, error) {
	_ = filename
	agentID := core.AgentID("ceo")
	workspace := filepath.Join(e.runRoot, "agents", string(agentID))
	store := &artifact.ScopedLocalStore{RunRoot: e.runRoot, WorkspaceRoot: workspace}
	uri := path.Join("projects", string(e.runID), "agents", string(agentID), "artifacts", "uploads", "ceo_requirement.md")
	if err := store.Write(ctx, uri, content); err != nil {
		return "", err
	}
	return uri, nil
}

func (e *executor) writeRunConfig(ctx context.Context) (string, error) {
	agentID := core.AgentID("architect01")
	workspace := filepath.Join(e.runRoot, "agents", string(agentID))
	store := &artifact.ScopedLocalStore{RunRoot: e.runRoot, WorkspaceRoot: workspace}
	uri := path.Join("projects", string(e.runID), "agents", string(agentID), "artifacts", "config", "run_config.md")
	config := map[string]any{
		"main_branch":         "main",
		"coder_agents":        1,
		"tester_agents":       1,
		"max_modules":         1,
		"assignment_strategy": "one_coder_one_tester_per_module",
		"agent_name_prefix": map[string]string{
			"coder":  "coder",
			"tester": "tester",
		},
	}
	raw, _ := json.MarshalIndent(config, "", "  ")
	content := "```json\n" + string(raw) + "\n```\n"
	if err := store.Write(ctx, uri, []byte(content)); err != nil {
		return "", err
	}
	return uri, nil
}

func (e *executor) writeStepReport(ctx context.Context, step Step, feedback core.TaskMetaData, execErr error) string {
	agentID := core.AgentID(step.Agent)
	workspace := filepath.Join(e.runRoot, "agents", string(agentID))
	store := &artifact.ScopedLocalStore{RunRoot: e.runRoot, WorkspaceRoot: workspace}
	uri := path.Join("projects", string(e.runID), "agents", step.Agent, "artifacts", "flow_reports", step.ID+".md")
	var builder strings.Builder
	builder.WriteString("# Flow Step Report\n\n")
	builder.WriteString("## Step\n\n")
	builder.WriteString("- agent: " + step.Agent + "\n")
	builder.WriteString("- op: " + step.Op + "\n")
	builder.WriteString("- result: " + string(feedback.Result) + "\n")
	if execErr != nil {
		builder.WriteString("- error: " + execErr.Error() + "\n")
	}
	builder.WriteString("\n## Inputs\n\n")
	for _, input := range step.Inputs {
		builder.WriteString("- " + input + "\n")
	}
	builder.WriteString("\n## Note\n\nThe agent returned no artifact outputs, so the flow demo wrote this trace artifact for UI inspection.\n")
	_ = store.Write(ctx, uri, []byte(builder.String()))
	return uri
}

func (e *executor) installFakeOpenCode() error {
	toolDir := filepath.Join(e.runRoot, "tools")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		e.fakeOpenCode = filepath.Join(toolDir, "flowdemo-opencode.cmd")
		if err := os.WriteFile(e.fakeOpenCode, []byte(fakeOpenCodeCMD()), 0o755); err != nil {
			return err
		}
	} else {
		e.fakeOpenCode = filepath.Join(toolDir, "flowdemo-opencode.sh")
		if err := os.WriteFile(e.fakeOpenCode, []byte(fakeOpenCodeSH()), 0o755); err != nil {
			return err
		}
	}
	e.oldOpenCode, e.hadOpenCode = os.LookupEnv("DEVFLOW_OPENCODE_PATH")
	_ = os.Setenv("DEVFLOW_FAKE_OPENCODE", "1")
	return os.Setenv("DEVFLOW_OPENCODE_PATH", e.fakeOpenCode)
}

func (e *executor) restoreOpenCodeEnv() {
	_ = os.Unsetenv("DEVFLOW_FAKE_OPENCODE")
	if e.hadOpenCode {
		_ = os.Setenv("DEVFLOW_OPENCODE_PATH", e.oldOpenCode)
		return
	}
	_ = os.Unsetenv("DEVFLOW_OPENCODE_PATH")
}

func useFakeOpenCode() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("FLOWDEMO_FAKE_OPENCODE")))
	return value != "0" && value != "false" && value != "no"
}

func fakeOpenCodeCMD() string {
	return `@echo off
if not exist ".devflow" mkdir ".devflow"
echo flowdemo synthetic code change > "demo_agent_output.txt"
powershell.exe -NoProfile -Command "$payload = @{status='passed'; summary='Flow demo synthetic coding agent result'; changed_files=@('demo_agent_output.txt'); test_command='Write-Output demo tests passed'; test_passed=$true; fixed=$false; merged_branches=@('flowdemo/demo'); failure_summary=''; conflicts=@(); evidence=@('flowdemo fake opencode executed'); suspected_files=@(); reproduction_steps=@(); tests_added=@()} | ConvertTo-Json -Depth 5; Set-Content -Path '.devflow/result.json' -Value $payload -Encoding UTF8"
exit /b 0
`
}

func fakeOpenCodeSH() string {
	return `#!/usr/bin/env sh
mkdir -p .devflow
printf '%s\n' 'flowdemo synthetic code change' > demo_agent_output.txt
cat > .devflow/result.json <<'JSON'
{"status":"passed","summary":"Flow demo synthetic coding agent result","changed_files":["demo_agent_output.txt"],"test_command":"echo demo tests passed","test_passed":true,"fixed":false,"merged_branches":["flowdemo/demo"],"failure_summary":"","conflicts":[],"evidence":["flowdemo fake opencode executed"],"suspected_files":[],"reproduction_steps":[],"tests_added":[]}
JSON
`
}

func firstCoderTesterControls(feedback core.TaskMetaData) (core.Control, core.Control) {
	var coderControl core.Control
	var testerControl core.Control
	for _, control := range feedback.Control {
		switch control.Type {
		case core.ControlTypeNewCoder:
			if coderControl.AgentName == "" {
				coderControl = control
			}
		case core.ControlTypeNewTester:
			if testerControl.AgentName == "" {
				testerControl = control
			}
		}
	}
	return coderControl, testerControl
}

func firstURIContaining(uris []string, needle string) string {
	for _, uri := range uris {
		if strings.Contains(strings.ToLower(filepath.ToSlash(uri)), strings.ToLower(needle)) {
			return uri
		}
	}
	return ""
}

func compactURIs(uris []string) []string {
	out := make([]string, 0, len(uris))
	seen := map[string]bool{}
	for _, uri := range uris {
		uri = filepath.ToSlash(strings.TrimSpace(uri))
		if uri == "" || seen[uri] {
			continue
		}
		seen[uri] = true
		out = append(out, uri)
	}
	return out
}

func gitValue(ctx context.Context, repo string, args ...string) string {
	if strings.TrimSpace(repo) == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cloneRun(run *Run) *Run {
	if run == nil {
		return nil
	}
	cp := *run
	cp.Steps = append([]Step(nil), run.Steps...)
	for i := range cp.Steps {
		cp.Steps[i].Inputs = append([]string(nil), run.Steps[i].Inputs...)
		cp.Steps[i].Outputs = append([]string(nil), run.Steps[i].Outputs...)
	}
	return &cp
}
