package coder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
)

type testArtifactStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *testArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.files[uri]), nil
}

func (s *testArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[uri] = string(content)
	return nil
}

type debugRunner struct {
	report     openCodeExecutionReport
	reports    []openCodeExecutionReport
	workDir    string
	prompt     string
	prompts    []string
	skipReport bool
	err        error
}

func (r *debugRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
	r.workDir = req.WorkDir
	r.prompt = req.Prompt
	r.prompts = append(r.prompts, req.Prompt)
	if r.err != nil {
		return common.OpenCodeResult{ExitCode: 1}, r.err
	}
	if r.skipReport {
		return common.OpenCodeResult{ExitCode: 0, Stdout: "completed without report", Duration: time.Millisecond}, nil
	}
	if err := os.MkdirAll(filepath.Join(req.WorkDir, ".devflow"), 0o755); err != nil {
		return common.OpenCodeResult{}, err
	}
	report := r.report
	if len(r.reports) > 0 {
		report = r.reports[0]
		r.reports = r.reports[1:]
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return common.OpenCodeResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.WorkDir, ".devflow", "result.json"), raw, 0o644); err != nil {
		return common.OpenCodeResult{}, err
	}
	return common.OpenCodeResult{ExitCode: 0, Duration: time.Millisecond}, nil
}

type debugGitManager struct {
	hasChanges        bool
	testCommand       string
	testCommands      []string
	testResults       []common.CommandResult
	testErrors        []error
	commitMessage     string
	commitCalled      bool
	createWorktreeHit bool
	worktree          common.Worktree
}

func (m *debugGitManager) CreateCoderWorktree(_ context.Context, _ common.CreateCoderWorktreeRequest) (common.Worktree, error) {
	m.createWorktreeHit = true
	if m.worktree.Path != "" {
		if err := os.MkdirAll(filepath.Join(m.worktree.Path, ".devflow"), 0o755); err != nil {
			return common.Worktree{}, err
		}
		return m.worktree, nil
	}
	return common.Worktree{}, nil
}

func (m *debugGitManager) HasChanges(context.Context, string) (bool, error) {
	return m.hasChanges, nil
}

func (m *debugGitManager) RunTestCommand(_ context.Context, _ string, command string) (common.CommandResult, error) {
	m.testCommand = command
	m.testCommands = append(m.testCommands, command)
	index := len(m.testCommands) - 1
	var result common.CommandResult
	if index < len(m.testResults) {
		result = m.testResults[index]
	}
	if result.Duration == 0 {
		result.Duration = time.Millisecond
	}
	if index < len(m.testErrors) && m.testErrors[index] != nil {
		return result, m.testErrors[index]
	}
	return result, nil
}

func (m *debugGitManager) CommitAll(_ context.Context, _ string, message string) (common.CommitResult, error) {
	m.commitCalled = true
	m.commitMessage = message
	return common.CommitResult{Commit: "fix456", Duration: time.Millisecond}, nil
}

type fakeLLMClient struct{}

func (fakeLLMClient) Complete(context.Context, string) (string, error) {
	return "ok", nil
}

func TestValidateTestCommandRejectsNaturalLanguageReportText(t *testing.T) {
	command := "python -m compileall todo_app todo.py; PowerShell CLI smoke test covering list/add/list/done/list"

	if err := validateTestCommand(command); err == nil {
		t.Fatal("validateTestCommand accepted a command containing natural-language test notes")
	}
}

func TestValidateTestCommandAcceptsExecutableShellCommand(t *testing.T) {
	command := "python -m compileall todo_app todo.py; python todo.py list"

	if err := validateTestCommand(command); err != nil {
		t.Fatalf("validateTestCommand rejected executable command: %v", err)
	}
}

func TestPromptIncludesFrontendWebDeliveryInstructions(t *testing.T) {
	recipe, err := GetRecipe(core.TaskOpWriteCode)
	if err != nil {
		t.Fatalf("GetRecipe() error = %v", err)
	}
	prompt := buildPrompt(core.TaskMetaData{
		TaskID:  "task_write",
		AgentID: "coder01",
		Op:      core.TaskOpWriteCode,
	}, recipe, writeCodeInputs{
		moduleDoc:   coderAgentDocument("projects/run1/agents/architect01/artifacts/modules/coder01_task.md", "# Programmer Task\n"),
		branchDoc:   coderAgentDocument("projects/run1/agents/architect01/artifacts/branches/main_branch.md", "{}"),
		contractDoc: coderAgentDocument("projects/run1/agents/architect01/artifacts/contracts/module01_contract.json", `{"delivery_profile":"frontend_web","required_files":["index.html","README.md"]}`),
		seedDoc:     coderAgentDocument("projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json", "{}"),
	})
	for _, want := range []string{"module_contract.delivery_profile is frontend_web", "index.html must load code from src", "README.md must contain real run instructions", "must not be only module functions"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPrepareImplementationBriefWritesBriefAndContextRefs(t *testing.T) {
	worktreePath := t.TempDir()
	runRoot := t.TempDir()
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/module01_task.md"
	branchURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	agent := &Agent{runRoot: runRoot}

	brief, err := agent.prepareImplementationBrief(context.Background(), worktreePath, writeCodeInputs{
		moduleDoc: coderAgentDocument(moduleURI, "# Module Title\n\n## Goal\n\nBuild the compact core.\n\n## Module Scope\n\nOnly core runtime.\n\n## Deliverables\n\n- src/module01.js\n\n## Architecture Context\n\nVERY LARGE ARCHITECTURE TEXT\n"),
		branchDoc: coderAgentDocument(branchURI, branchArtifactJSON(t, common.BranchArtifact{
			SchemaVersion: 1,
			Kind:          "branch",
			RepoDir:       "C:/repo/project",
			Branch:        "main",
			Commit:        "base123",
			TestCommand:   "npm test",
		})),
		contractDoc: coderAgentDocument(contractURI, moduleContractJSON(t, "module01", "node test/module01.seed.test.js")),
		seedDoc:     coderAgentDocument(seedURI, seedBundleJSON(t, "module01", "node test/module01.seed.test.js")),
	}, common.BranchArtifact{
		RepoDir:     "C:/repo/project",
		Branch:      "main",
		Commit:      "base123",
		TestCommand: "npm test",
	}, common.TestFileBundle{
		SchemaVersion: 1,
		Kind:          "seed_tests",
		ModuleID:      "module01",
		TestCommand:   "node test/module01.seed.test.js",
		TestFiles:     []common.TestFile{{Path: "test/module01.seed.test.js", Content: "console.log('ok')\n"}},
	})
	if err != nil {
		t.Fatalf("prepareImplementationBrief() error = %v", err)
	}
	for _, path := range []string{brief.Path, brief.Refs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}
	for _, want := range []string{"## module_name", "module01", "Build the compact core.", `"src/module01.js"`, `"test/**"`, `"createModule"`, "node test/module01.seed.test.js", ".devflow/context/context_refs.json"} {
		if !strings.Contains(brief.Content, want) {
			t.Fatalf("brief missing %q:\n%s", want, brief.Content)
		}
	}
	var refs struct {
		Sources map[string]struct {
			ArtifactURI string `json:"artifact_uri"`
			Path        string `json:"path"`
		} `json:"sources"`
	}
	rawRefs, err := os.ReadFile(brief.Refs)
	if err != nil {
		t.Fatalf("read refs: %v", err)
	}
	if err := json.Unmarshal(rawRefs, &refs); err != nil {
		t.Fatalf("parse refs: %v", err)
	}
	if refs.Sources["module_task"].ArtifactURI != moduleURI {
		t.Fatalf("module_task artifact uri = %q, want %q", refs.Sources["module_task"].ArtifactURI, moduleURI)
	}
	wantPath := filepath.Clean(filepath.Join(runRoot, "agents", "architect01", "artifacts", "modules", "module01_task.md"))
	if refs.Sources["module_task"].Path != wantPath {
		t.Fatalf("module_task path = %q, want %q", refs.Sources["module_task"].Path, wantPath)
	}
}

func TestBuildBriefPromptUsesBriefWithoutFullModuleDoc(t *testing.T) {
	recipe, err := GetRecipe(core.TaskOpWriteCode)
	if err != nil {
		t.Fatalf("GetRecipe() error = %v", err)
	}
	brief := "# Implementation Brief\n\n## allowed_files\n\n[\"src/module01.js\"]\n\n## must_not_modify\n\n[\"test/**\"]\n\n## required_api\n\n[{\"name\":\"createModule\"}]\n\n## test_command\n\nnode test/module01.seed.test.js\n"
	prompt := buildBriefPrompt(core.TaskMetaData{
		TaskID:  "task_write",
		AgentID: "coder01",
		Op:      core.TaskOpWriteCode,
	}, recipe, brief)
	for _, want := range []string{"Use the implementation brief as the primary source", "Do not read every context file first", "src/module01.js", "test/**", "createModule", "node test/module01.seed.test.js", "context_files_read"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("brief prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "## Architecture Context") || strings.Contains(prompt, "VERY LARGE ARCHITECTURE TEXT") {
		t.Fatalf("brief prompt contains full module doc context:\n%s", prompt)
	}
}

func TestPrepareImplementationBriefIncludesFrontendWebRequirements(t *testing.T) {
	worktreePath := t.TempDir()
	contractRaw, err := common.MarshalJSONArtifact(map[string]any{
		"schema_version":             1,
		"kind":                       "module_contract",
		"module_name":                "frontend_module",
		"delivery_profile":           "frontend_web",
		"required_files":             []string{"index.html", "README.md"},
		"entry_files":                []string{"index.html"},
		"public_api":                 []map[string]string{{"name": "frontend_web_app", "kind": "browser_app"}},
		"allowed_files":              []string{"index.html", "README.md", "package.json", "src/**"},
		"forbidden_files":            []string{"test/**"},
		"official_seed_test_command": "node --test test/module01.seed.test.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{runRoot: t.TempDir()}
	brief, err := agent.prepareImplementationBrief(context.Background(), worktreePath, writeCodeInputs{
		moduleDoc:   coderAgentDocument("projects/run1/agents/architect01/artifacts/modules/module01_task.md", "# Frontend Module\n\n## Goal\n\nBuild app.\n"),
		branchDoc:   coderAgentDocument("projects/run1/agents/architect01/artifacts/branches/main_branch.md", "{}"),
		contractDoc: coderAgentDocument("projects/run1/agents/architect01/artifacts/contracts/module01_contract.json", string(contractRaw)),
		seedDoc:     coderAgentDocument("projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json", seedBundleJSON(t, "module01", "node --test test/module01.seed.test.js")),
	}, common.BranchArtifact{RepoDir: "C:/repo/project", Branch: "main", Commit: "base123"}, common.TestFileBundle{
		SchemaVersion: 1,
		Kind:          "seed_tests",
		ModuleID:      "module01",
		TestCommand:   "node --test test/module01.seed.test.js",
		TestFiles:     []common.TestFile{{Path: "test/module01.seed.test.js", Content: "console.log('ok')\n"}},
	})
	if err != nil {
		t.Fatalf("prepareImplementationBrief() error = %v", err)
	}
	for _, want := range []string{"frontend_web", "index.html must load code from src", "README.md must contain real run instructions", "not be only module functions", `"src/**"`} {
		if !strings.Contains(brief.Content, want) {
			t.Fatalf("frontend brief missing %q:\n%s", want, brief.Content)
		}
	}
}

func TestPrepareDebugBriefWritesBriefAndRefs(t *testing.T) {
	worktreePath := t.TempDir()
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/module01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/module01_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	fullTestsURI := "projects/run1/agents/tester_01/artifacts/test_data/full_test_files.json"
	reportURI := "projects/run1/agents/tester_01/artifacts/test_reports/module01_failure.md"
	branchInfo := common.CoderBranchArtifact{
		SchemaVersion: 1,
		Kind:          "coder_branch",
		RepoDir:       "C:/repo/project",
		BaseBranch:    "main",
		BaseCommit:    "base123",
		Branch:        "devflow/run1/coder_01/task/module01",
		Commit:        "coder123",
		Worktree:      filepath.ToSlash(worktreePath),
		ModuleTaskURI: moduleURI,
		Summary:       "Implemented initial module wiring.",
		ChangedFiles:  []string{"src/module01.js"},
		TestCommand:   "node test/module01.seed.test.js",
	}
	inputs := debugInputs{
		moduleDoc:    coderAgentDocument(moduleURI, "# Module 01\n\n## Goal\n\nFix compact behavior.\n\n## Architecture Context\n\nVERY LARGE ARCHITECTURE TEXT\n"),
		branchDoc:    coderAgentDocument(branchURI, coderBranchArtifactJSON(t, branchInfo)),
		contractDoc:  coderAgentDocument(contractURI, moduleContractJSON(t, "module01", "node test/module01.seed.test.js")),
		seedDoc:      coderAgentDocument(seedURI, seedBundleJSON(t, "module01", "node test/module01.seed.test.js")),
		fullTestsDoc: coderAgentDocument(fullTestsURI, fullBundleJSON(t, "module01", "node test/module01.seed.test.js")),
		failureDoc:   coderAgentDocument(reportURI, "# Host Test Failure Report\n\n## Test Command\n\nnode test/module01.seed.test.js\n\n## Stderr\n\nCannot find module './module01.js'\n"),
	}
	agent := &Agent{runRoot: t.TempDir()}

	brief, err := agent.prepareDebugBrief(context.Background(), worktreePath, inputs, branchInfo, "node test/module01.seed.test.js")
	if err != nil {
		t.Fatalf("prepareDebugBrief() error = %v", err)
	}
	for _, path := range []string{brief.Path, brief.Refs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}
	var refs struct {
		Sources map[string]struct {
			ArtifactURI string `json:"artifact_uri"`
			Path        string `json:"path"`
		} `json:"sources"`
	}
	rawRefs, err := os.ReadFile(brief.Refs)
	if err != nil {
		t.Fatalf("read refs: %v", err)
	}
	if err := json.Unmarshal(rawRefs, &refs); err != nil {
		t.Fatalf("parse refs: %v", err)
	}
	for _, key := range []string{"module_task", "coder_branch", "module_contract", "seed_tests", "failure_report", "full_tests"} {
		source, ok := refs.Sources[key]
		if !ok {
			t.Fatalf("debug refs missing %s: %#v", key, refs.Sources)
		}
		if !strings.HasPrefix(filepath.Clean(source.Path), filepath.Clean(filepath.Join(worktreePath, ".devflow", "context"))) {
			t.Fatalf("%s path = %q, want worktree context path", key, source.Path)
		}
		if _, err := os.Stat(source.Path); err != nil {
			t.Fatalf("%s local path missing: %v", key, err)
		}
	}
	if refs.Sources["module_task"].ArtifactURI != moduleURI {
		t.Fatalf("module_task artifact uri = %q, want %q", refs.Sources["module_task"].ArtifactURI, moduleURI)
	}
	for _, want := range []string{"# Debug Brief", "Cannot find module './module01.js'", "node test/module01.seed.test.js", "allowed_files", "forbidden_files", "public_api", ".devflow/context/debug_refs.json"} {
		if !strings.Contains(brief.Content, want) {
			t.Fatalf("debug brief missing %q:\n%s", want, brief.Content)
		}
	}
	if strings.Contains(brief.Content, "## Architecture Context") || strings.Contains(brief.Content, "VERY LARGE ARCHITECTURE TEXT") {
		t.Fatalf("debug brief contains full module architecture context:\n%s", brief.Content)
	}
}

func TestExecuteDebugRepairsExistingCoderBranchAndWritesUpdatedBranchArtifact(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	fullTestsURI := "projects/run1/agents/tester_01/artifacts/test_data/full_test_files.json"
	reportURI := "projects/run1/agents/tester_01/artifacts/test_reports/tester_01_test_report.md"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Coder Task\n\n## Architecture Context\n\nVERY LARGE ARCHITECTURE TEXT\n",
			contractURI:   moduleContractJSON(t, "module01", "go test ./internal/foo"),
			seedURI:       seedBundleJSON(t, "module01", "go test ./internal/foo"),
			fullTestsURI:  fullBundleJSON(t, "module01", "go test ./internal/foo"),
			branchURI: coderBranchArtifactJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       "C:/repo/project",
				BaseBranch:    "main",
				BaseCommit:    "base123",
				Branch:        "devflow/run1/coder_01/task/module",
				Commit:        "coder123",
				Worktree:      filepath.ToSlash(worktreePath),
				ModuleTaskURI: moduleTaskURI,
				TestCommand:   "go test ./...",
			}),
			reportURI: "# Test Failure Report\n\n## Test Command\n\ngo test ./internal/foo\n\n## Failure Summary\n\nexpected score update failed\n",
		},
	}
	runner := &debugRunner{report: openCodeExecutionReport{
		Status:       "fixed",
		Summary:      "fixed score update",
		ChangedFiles: []string{"internal/foo/score.go"},
		TestCommand:  "go test ./internal/foo",
		TestPassed:   true,
	}}
	git := &debugGitManager{hasChanges: true}
	agent := &Agent{
		runID:          "run1",
		agentID:        "coder_01",
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_debug_01",
		AgentID:      "coder_01",
		Op:           core.TaskOpDebug,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI, fullTestsURI, reportURI},
	})
	if err != nil {
		t.Fatalf("execute debug: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := runner.workDir, worktreePath; got != want {
		t.Fatalf("opencode workdir = %q, want %q", got, want)
	}
	if git.createWorktreeHit {
		t.Fatal("debug should not create a new worktree")
	}
	if got, want := git.testCommand, "go test ./internal/foo"; got != want {
		t.Fatalf("test command = %q, want %q", got, want)
	}
	for _, written := range []string{filepath.Join(worktreePath, "test", "module01.seed.test.js"), filepath.Join(worktreePath, "test", "module01.full.test.js")} {
		if _, err := os.Stat(written); err != nil {
			t.Fatalf("expected debug test file %s: %v", written, err)
		}
	}
	if !git.commitCalled || !strings.Contains(git.commitMessage, "debug") {
		t.Fatalf("commit message = %q, want debug commit", git.commitMessage)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("artifact count = %d, want %d", got, want)
	}
	if feedback.ArtifactURIs[0] == branchURI {
		t.Fatal("debug with changes should write a new branch artifact")
	}
	var output common.CoderBranchArtifact
	if err := json.Unmarshal([]byte(store.writes[feedback.ArtifactURIs[0]]), &output); err != nil {
		t.Fatalf("parse branch artifact: %v", err)
	}
	if output.Branch != "devflow/run1/coder_01/task/module" || output.Commit != "fix456" {
		t.Fatalf("output branch/commit = %s/%s, want original branch with new commit", output.Branch, output.Commit)
	}
	if output.TestCommand != "go test ./internal/foo" {
		t.Fatalf("output test command = %q, want failure report command", output.TestCommand)
	}
	for _, want := range []string{"Debug Brief", "expected score update failed", ".devflow/context/debug_refs.json", "allowed_files", "forbidden_files", "go test ./internal/foo"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("debug prompt missing %q:\n%s", want, runner.prompt)
		}
	}
	for _, unwanted := range []string{"VERY LARGE ARCHITECTURE TEXT", "console.log('seed tests passed');", "console.log('full tests passed');"} {
		if strings.Contains(runner.prompt, unwanted) {
			t.Fatalf("debug prompt contains full context %q:\n%s", unwanted, runner.prompt)
		}
	}
}

func TestExecuteDebugReturnsOriginalBranchWhenNoCodeChangesAndTestsPass(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	reportURI := "projects/run1/agents/tester_01/artifacts/test_reports/tester_01_test_report.md"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			contractURI:   moduleContractJSON(t, "module01", "go test ./..."),
			seedURI:       seedBundleJSON(t, "module01", "go test ./..."),
			branchURI: coderBranchArtifactJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       "C:/repo/project",
				Branch:        "devflow/run1/coder_01/task/module",
				Commit:        "coder123",
				Worktree:      filepath.ToSlash(worktreePath),
				ModuleTaskURI: moduleTaskURI,
				TestCommand:   "go test ./...",
			}),
			reportURI: "# Test Failure Report\n\n## Test Command\n\ngo test ./...\n",
		},
	}
	runner := &debugRunner{report: openCodeExecutionReport{
		Status:      "passed",
		Summary:     "failure no longer reproduces",
		TestCommand: "go test ./...",
		TestPassed:  true,
	}}
	git := &debugGitManager{hasChanges: false}
	agent := &Agent{
		runID:          "run1",
		agentID:        "coder_01",
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_debug_01",
		AgentID:      "coder_01",
		Op:           core.TaskOpDebug,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI, reportURI},
	})
	if err != nil {
		t.Fatalf("execute debug: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := strings.Join(feedback.ArtifactURIs, "|"), branchURI; got != want {
		t.Fatalf("output artifacts = %v, want original branch artifact", feedback.ArtifactURIs)
	}
	if git.commitCalled {
		t.Fatal("debug should not commit when there are no code changes")
	}
	for _, want := range []string{"# Debug Existing Coder Branch", "Debug Brief", ".devflow/context/debug_refs.json", "Do not switch branches"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
}

func TestExecuteWriteCodeRetriesOnceAfterHostTestFailure(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Programmer Task: core_logic_data_module\n\n## Goal\n\nImplement the snake core module.\n\n## Architecture Context\n\nFULL ARCHITECTURE CONTEXT SHOULD STAY OUT OF THE INITIAL PROMPT\n",
			contractURI:   moduleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       seedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
			branchURI: branchArtifactJSON(t, common.BranchArtifact{
				SchemaVersion: 1,
				Kind:          "branch",
				RunID:         "run1",
				RepoDir:       "C:/repo/project",
				Branch:        "main",
				Commit:        "base123",
				TestCommand:   "npm test",
			}),
		},
	}
	runner := &debugRunner{
		reports: []openCodeExecutionReport{
			{
				Status:       "completed",
				Summary:      "Implemented initial snake core module.",
				ChangedFiles: []string{"src/gameCore.js", "test/gameCore.test.js"},
				TestCommand:  "npm test",
				TestPassed:   true,
			},
			{
				Status:       "completed",
				Summary:      "Fixed JS module import path and aligned runtime test entry.",
				ChangedFiles: []string{"src/gameCore.js", "test/gameCore.test.js"},
				TestCommand:  "npm test",
				TestPassed:   true,
			},
		},
	}
	git := &debugGitManager{
		hasChanges: true,
		worktree: common.Worktree{
			RepoDir: "C:/repo/project",
			Branch:  "devflow/run1/coder_01/task/module",
			Path:    worktreePath,
		},
		testResults: []common.CommandResult{
			{ExitCode: 1, Stderr: "Cannot find module './gameCore.js'\n", Duration: time.Millisecond},
			{ExitCode: 0, Stdout: "tests passed", Duration: time.Millisecond},
		},
		testErrors: []error{
			fmt.Errorf("run test command %q: exit status 1", "node test/module01.seed.test.js"),
			nil,
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "coder_01",
		workspacePath:  t.TempDir(),
		runConfig:      core.RunConfig{LLM: core.LLMConfig{Model: "ep-test"}},
		artifactStore:  store,
		llmClient:      fakeLLMClient{},
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_write_01",
		AgentID:      "coder_01",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute write_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := len(runner.prompts), 2; got != want {
		t.Fatalf("runner prompt count = %d, want %d", got, want)
	}
	if strings.Contains(runner.prompts[0], "## Architecture Context") || strings.Contains(runner.prompts[0], "FULL ARCHITECTURE CONTEXT SHOULD STAY OUT OF THE INITIAL PROMPT") {
		t.Fatalf("initial write_code prompt contains full architecture context:\n%s", runner.prompts[0])
	}
	if !strings.Contains(runner.prompts[0], "Use the implementation brief as the primary source") {
		t.Fatalf("initial write_code prompt did not use brief-first instructions:\n%s", runner.prompts[0])
	}
	if got, want := len(git.testCommands), 2; got != want {
		t.Fatalf("test command count = %d, want %d", got, want)
	}
	if !strings.Contains(runner.prompts[1], "Host Test Failure Report") {
		t.Fatalf("repair prompt missing host test failure context:\n%s", runner.prompts[1])
	}
	if !strings.Contains(runner.prompts[1], "Cannot find module './gameCore.js'") {
		t.Fatalf("repair prompt missing original stderr:\n%s", runner.prompts[1])
	}
	for _, want := range []string{"Debug Brief", "allowed_files", "forbidden_files", "node test/module01.seed.test.js", ".devflow/context/debug_refs.json"} {
		if !strings.Contains(runner.prompts[1], want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, runner.prompts[1])
		}
	}
	for _, unwanted := range []string{"## Architecture Context", "FULL ARCHITECTURE CONTEXT SHOULD STAY OUT OF THE INITIAL PROMPT", "console.log('seed tests passed');"} {
		if strings.Contains(runner.prompts[1], unwanted) {
			t.Fatalf("repair prompt contains full context %q:\n%s", unwanted, runner.prompts[1])
		}
	}
	if !git.commitCalled {
		t.Fatal("write_code should commit after repair succeeds")
	}
	for _, command := range git.testCommands {
		if command != "node test/module01.seed.test.js" {
			t.Fatalf("test command = %q, want seed test command", command)
		}
	}
	seedPath := filepath.Join(worktreePath, "test", "module01.seed.test.js")
	if _, err := os.Stat(seedPath); err != nil {
		t.Fatalf("seed test file was not materialized in worktree: %v", err)
	}
	var output common.CoderBranchArtifact
	if err := json.Unmarshal([]byte(store.writes[feedback.ArtifactURIs[0]]), &output); err != nil {
		t.Fatalf("parse branch artifact: %v", err)
	}
	if output.TestCommand != "node test/module01.seed.test.js" {
		t.Fatalf("output test command = %q, want seed command", output.TestCommand)
	}
	if output.Commit != "fix456" {
		t.Fatalf("output commit = %q, want fix456", output.Commit)
	}
}

func TestExecuteWriteCodeRetryableFailureWritesDebugArtifacts(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Programmer Task: core_logic_data_module\n\nImplement the snake core module.\n",
			contractURI:   moduleContractJSON(t, "module01", "node --test test/module01.seed.test.js"),
			seedURI:       seedBundleJSON(t, "module01", "node --test test/module01.seed.test.js"),
			branchURI: branchArtifactJSON(t, common.BranchArtifact{
				SchemaVersion: 1,
				Kind:          "branch",
				RunID:         "run1",
				RepoDir:       "C:/repo/project",
				Branch:        "main",
				Commit:        "base123",
				TestCommand:   "node --test",
			}),
		},
	}
	runner := &debugRunner{
		reports: []openCodeExecutionReport{
			{
				Status:       "completed",
				Summary:      "Implemented initial snake core module.",
				ChangedFiles: []string{"src/core/game_controller.js", "test/game_controller.test.js"},
				TestCommand:  "node --test",
				TestPassed:   false,
			},
			{
				Status:       "completed",
				Summary:      "Attempted repair but tests still fail.",
				ChangedFiles: []string{"src/core/game_controller.js", "test/game_controller.test.js"},
				TestCommand:  "node --test",
				TestPassed:   false,
			},
		},
	}
	git := &debugGitManager{
		hasChanges: true,
		worktree: common.Worktree{
			RepoDir: "C:/repo/project",
			Branch:  "devflow/run1/coder_01/task/module",
			Path:    worktreePath,
		},
		testResults: []common.CommandResult{
			{ExitCode: 1, Stderr: "AssertionError: expected GAME_OVER\n", Duration: time.Millisecond},
			{ExitCode: 1, Stderr: "AssertionError: expected GAME_OVER\n", Duration: time.Millisecond},
		},
		testErrors: []error{
			fmt.Errorf("run test command %q: exit status 1", "node --test"),
			fmt.Errorf("run test command %q: exit status 1", "node --test"),
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "coder_01",
		workspacePath:  t.TempDir(),
		runConfig:      core.RunConfig{LLM: core.LLMConfig{Model: "ep-test"}},
		artifactStore:  store,
		llmClient:      fakeLLMClient{},
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_write_01",
		AgentID:      "coder_01",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute write_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeBug {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeBug)
	}
	if got, want := len(feedback.ArtifactURIs), 3; got != want {
		t.Fatalf("artifact count = %d, want %d", got, want)
	}
	failedBranchURI := feedback.ArtifactURIs[0]
	if !strings.Contains(failedBranchURI, "/artifacts/branches/") {
		t.Fatalf("first artifact = %q, want retryable branch artifact", failedBranchURI)
	}
	reportURI := feedback.ArtifactURIs[1]
	if !strings.Contains(reportURI, "/artifacts/test_reports/") {
		t.Fatalf("second artifact = %q, want test_reports artifact", reportURI)
	}
	reportContent := store.writes[reportURI]
	for _, want := range []string{"# Host Test Failure Report", "node --test", "AssertionError: expected GAME_OVER"} {
		if !strings.Contains(reportContent, want) {
			t.Fatalf("test failure report missing %q:\n%s", want, reportContent)
		}
	}
	if !strings.Contains(feedback.ArtifactURIs[2], "/artifacts/failures/write_code_failure.md") {
		t.Fatalf("third artifact = %q, want write_code failure artifact", feedback.ArtifactURIs[2])
	}
}

func TestExecuteWriteCodeSynthesizesReportWhenMissingButChangesExist(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_02_task.md"
	branchURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module02_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module02_seed_tests.json"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Programmer Task: ui_input_and_canvas_render\n\nImplement the UI module.\n",
			contractURI:   moduleContractJSON(t, "module02", "node test/module02.seed.test.js"),
			seedURI:       seedBundleJSON(t, "module02", "node test/module02.seed.test.js"),
			branchURI: branchArtifactJSON(t, common.BranchArtifact{
				SchemaVersion: 1,
				Kind:          "branch",
				RunID:         "run1",
				RepoDir:       "C:/repo/project",
				Branch:        "main",
				Commit:        "base123",
				TestCommand:   "npm test",
			}),
		},
	}
	runner := &debugRunner{skipReport: true}
	git := &debugGitManager{
		hasChanges: true,
		worktree: common.Worktree{
			RepoDir: "C:/repo/project",
			Branch:  "devflow/run1/coder_02/task/module",
			Path:    worktreePath,
		},
		testResults: []common.CommandResult{
			{ExitCode: 0, Stdout: "tests passed", Duration: time.Millisecond},
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "coder_02",
		workspacePath:  t.TempDir(),
		runConfig:      core.RunConfig{LLM: core.LLMConfig{Model: "ep-test"}},
		artifactStore:  store,
		llmClient:      fakeLLMClient{},
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_write_02",
		AgentID:      "coder_02",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute write_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if !git.commitCalled {
		t.Fatal("write_code should still commit when report is missing but code changes exist")
	}
	if got, want := git.testCommand, "node test/module02.seed.test.js"; got != want {
		t.Fatalf("test command = %q, want %q", got, want)
	}
	var output common.CoderBranchArtifact
	if err := json.Unmarshal([]byte(store.writes[feedback.ArtifactURIs[0]]), &output); err != nil {
		t.Fatalf("parse branch artifact: %v", err)
	}
	if !strings.Contains(output.Summary, "did not write .devflow/result.json") {
		t.Fatalf("summary = %q, want missing report note", output.Summary)
	}
	if output.TestCommand != "node test/module02.seed.test.js" {
		t.Fatalf("output test command = %q, want seed test command", output.TestCommand)
	}
}

func TestExecuteWriteCodeMissingRuntimeStaysFatal(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_03_task.md"
	branchURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module03_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module03_seed_tests.json"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Programmer Task: engine_services\n\nImplement the engine module.\n",
			contractURI:   moduleContractJSON(t, "module03", "node --test test/module03.seed.test.js"),
			seedURI:       seedBundleJSON(t, "module03", "node --test test/module03.seed.test.js"),
			branchURI: branchArtifactJSON(t, common.BranchArtifact{
				SchemaVersion: 1,
				Kind:          "branch",
				RunID:         "run1",
				RepoDir:       "C:/repo/project",
				Branch:        "main",
				Commit:        "base123",
				TestCommand:   "node --test test/engine-services.test.js",
			}),
		},
	}
	runner := &debugRunner{
		reports: []openCodeExecutionReport{
			{
				Status:       "completed",
				Summary:      "Implemented engine services.",
				ChangedFiles: []string{"src/engine-services.js", "test/engine-services.test.js"},
				TestCommand:  "node --test test/engine-services.test.js",
				TestPassed:   false,
			},
			{
				Status:       "completed",
				Summary:      "Attempted repair, but runtime is still unavailable.",
				ChangedFiles: []string{"src/engine-services.js", "test/engine-services.test.js"},
				TestCommand:  "node --test test/engine-services.test.js",
				TestPassed:   false,
			},
		},
	}
	git := &debugGitManager{
		hasChanges: true,
		worktree: common.Worktree{
			RepoDir: "C:/repo/project",
			Branch:  "devflow/run1/coder_03/task/module",
			Path:    worktreePath,
		},
		testResults: []common.CommandResult{
			{ExitCode: 1, Stderr: "node : The term 'node' is not recognized\n", Duration: time.Millisecond},
			{ExitCode: 1, Stderr: "node : The term 'node' is not recognized\n", Duration: time.Millisecond},
		},
		testErrors: []error{
			fmt.Errorf("run test command %q: required executable %q was not found in PATH; install Node.js or set DEVFLOW_NODE_PATH / DEVFLOW_EXTRA_PATH so the DevFlow process can find node.exe: exit status 1", "node --test test/engine-services.test.js", "node"),
			fmt.Errorf("run test command %q: required executable %q was not found in PATH; install Node.js or set DEVFLOW_NODE_PATH / DEVFLOW_EXTRA_PATH so the DevFlow process can find node.exe: exit status 1", "node --test test/engine-services.test.js", "node"),
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "coder_03",
		workspacePath:  t.TempDir(),
		runConfig:      core.RunConfig{LLM: core.LLMConfig{Model: "ep-test"}},
		artifactStore:  store,
		llmClient:      fakeLLMClient{},
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_write_03",
		AgentID:      "coder_03",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute write_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
}

func coderBranchArtifactJSON(t *testing.T, artifact common.CoderBranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func branchArtifactJSON(t *testing.T, artifact common.BranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func moduleContractJSON(t *testing.T, moduleID string, seedCommand string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(map[string]any{
		"schema_version":             1,
		"kind":                       "module_contract",
		"module_id":                  moduleID,
		"module_name":                moduleID,
		"entry_files":                []string{"src/" + moduleID + ".js"},
		"public_api":                 []map[string]string{{"name": "createModule", "kind": "function", "input": "none", "output": "module result"}},
		"allowed_files":              []string{"src/" + moduleID + ".js"},
		"forbidden_files":            []string{"test/**", "package.json"},
		"official_seed_test_command": seedCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func seedBundleJSON(t *testing.T, moduleID string, command string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(common.TestFileBundle{
		SchemaVersion: 1,
		Kind:          "seed_tests",
		ModuleID:      moduleID,
		TestCommand:   command,
		TestFiles: []common.TestFile{
			{
				Path:    "test/" + moduleID + ".seed.test.js",
				Content: "console.log('seed tests passed');\n",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func fullBundleJSON(t *testing.T, moduleID string, command string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(common.TestFileBundle{
		SchemaVersion: 1,
		Kind:          "full_test_files",
		ModuleID:      moduleID,
		TestCommand:   command,
		TestFiles: []common.TestFile{
			{
				Path:    "test/" + moduleID + ".full.test.js",
				Content: "console.log('full tests passed');\n",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func coderAgentDocument(uri, content string) agentengine.ArtifactDocument {
	return agentengine.ArtifactDocument{URI: uri, Content: content}
}

var _ common.OpenCodeRunner = (*debugRunner)(nil)
var _ common.GitManager = (*debugGitManager)(nil)
