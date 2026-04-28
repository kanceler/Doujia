package tester

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
	"devflow/internal/llm"
)

type testArtifactStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *testArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	content, ok := s.files[uri]
	if !ok {
		return nil, fmt.Errorf("missing read artifact %s", uri)
	}
	return []byte(content), nil
}

func (s *testArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = map[string]string{}
	}
	s.writes[uri] = string(content)
	return nil
}

type fakeLLMClient struct {
	response string
	prompt   string
	err      error
	delay    time.Duration
}

func (c *fakeLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	c.prompt = prompt
	if c.delay > 0 {
		timer := time.NewTimer(c.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return c.response, c.err
}

type fakeOpenCodeRunner struct {
	report testCodeExecutionReport
	prompt string
	err    error
}

func (r *fakeOpenCodeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
	r.prompt = req.Prompt
	if r.err != nil {
		return common.OpenCodeResult{ExitCode: 1}, r.err
	}
	if err := os.MkdirAll(filepath.Join(req.WorkDir, ".devflow"), 0o755); err != nil {
		return common.OpenCodeResult{}, err
	}
	raw, err := json.Marshal(r.report)
	if err != nil {
		return common.OpenCodeResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.WorkDir, ".devflow", "result.json"), raw, 0o644); err != nil {
		return common.OpenCodeResult{}, err
	}
	return common.OpenCodeResult{ExitCode: 0, Duration: time.Millisecond}, nil
}

type fakeGitManager struct {
	worktreePath string
	request      common.CreateCoderWorktreeRequest
	err          error
	testCommands []string
	testResults  []common.CommandResult
	testErrors   []error
}

func (m *fakeGitManager) CreateCoderWorktree(_ context.Context, req common.CreateCoderWorktreeRequest) (common.Worktree, error) {
	m.request = req
	if m.err != nil {
		return common.Worktree{}, m.err
	}
	return common.Worktree{RepoDir: req.RepoDir, Branch: "tester/worktree", Path: m.worktreePath}, nil
}

func (m *fakeGitManager) HasChanges(context.Context, string) (bool, error) {
	return false, nil
}

func (m *fakeGitManager) RunTestCommand(_ context.Context, _ string, command string) (common.CommandResult, error) {
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

func (m *fakeGitManager) CommitAll(context.Context, string, string) (common.CommitResult, error) {
	return common.CommitResult{}, nil
}

func TestExecuteTestDataSucceedsWithNoopLLM(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n\n## Pairing Metadata\n\n- paired_coder_agent: coder_01\n- module_task_uri: " + moduleTaskURI + "\n",
			moduleTaskURI: "# Coder Task\n\n## Pairing Metadata\n\n- module_id: engine\n- paired_tester_agent: tester_01\n",
			contractURI:   testerModuleContractJSON(t, "engine", "node test/engine.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "engine", "node test/engine.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient:     llm.NoopClient{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := len(feedback.ArtifactURIs), 2; got != want {
		t.Fatalf("artifact uri count = %d, want %d", got, want)
	}
	outputURI := feedback.ArtifactURIs[0]
	if !strings.Contains(outputURI, "/artifacts/test_data/full_test_files.json") {
		t.Fatalf("output uri = %s, want full_test_files artifact", outputURI)
	}
	bundle, err := common.ParseTestFileBundle([]byte(store.writes[outputURI]))
	if err != nil {
		t.Fatalf("full_test_files is not parseable: %v\n%s", err, store.writes[outputURI])
	}
	if bundle.Kind != "full_test_files" || bundle.ModuleID != "engine" || bundle.TestCommand == "" {
		t.Fatalf("bundle = %+v, want full_test_files for engine", bundle)
	}
	content := bundle.TestFiles[0].Content
	for _, want := range []string{
		`const api = require("../src/engine.js");`,
		"createModule must be exported by module_contract public_api",
		"typeof api[\"createModule\"]",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated test content missing %q:\n%s", want, content)
		}
	}
}

func TestExecuteTestDataCompilesValidLLMPlanIntoFullBundle(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	model := &fakeLLMClient{
		response: `{"schema_version":1,"kind":"test_case_plan","module_id":"module01","cases":[{"name":"boundary_case","type":"boundary","target":"createModule","scenario":"call near the boundary of supported input","expected":"the API keeps deterministic behavior"}]}`,
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient:     model,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got := feedback.ArtifactURIs[0]; got != "projects/run1/agents/tester_01/artifacts/test_data/full_test_files.json" {
		t.Fatalf("output uri = %s, want tester test_data artifact", got)
	}
	bundle, err := common.ParseTestFileBundle([]byte(store.writes[feedback.ArtifactURIs[0]]))
	if err != nil {
		t.Fatalf("generated full_test_files was not written as a bundle: %v\n%s", err, store.writes[feedback.ArtifactURIs[0]])
	}
	content := bundle.TestFiles[0].Content
	for _, want := range []string{
		"// case boundary_case [boundary] target=createModule",
		"// scenario: call near the boundary of supported input",
		"// expected: the API keeps deterministic behavior",
		`"boundary_case: target must remain callable"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated test content missing %q:\n%s", want, content)
		}
	}
	if !strings.Contains(model.prompt, "Return only a test_case_plan JSON object.") || !strings.Contains(model.prompt, "# Output JSON Schema") {
		t.Fatalf("prompt missing output schema:\n%s", model.prompt)
	}
}

func TestFrontendWebBaseFullTestsVerifyDeliveryEntry(t *testing.T) {
	contract, err := parseModuleContract(testerFrontendModuleContractJSON(t, "module02", "node test/module02.seed.test.js"))
	if err != nil {
		t.Fatalf("parseModuleContract returned error: %v", err)
	}
	bundle, err := buildBaseFullTestBundle(testDataInputs{}, contract, mustParseTestBundle(t, testerSeedBundleJSON(t, "module02", "node test/module02.seed.test.js")))
	if err != nil {
		t.Fatalf("buildBaseFullTestBundle returned error: %v", err)
	}
	if got, want := bundle.TestCommand, "node test/module02.full.test.js"; got != want {
		t.Fatalf("test command = %q, want %q", got, want)
	}
	testContent := bundle.TestFiles[0].Content
	for _, want := range []string{"fs.existsSync('index.html')", "fs.existsSync('README.md')", "fs.existsSync('src')", "script|module|src"} {
		if !strings.Contains(testContent, want) {
			t.Fatalf("frontend_web full test missing %q:\n%s", want, testContent)
		}
	}
}

func TestExecuteTestDataIgnoresLLMTimeoutAndStillSucceeds(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient:     &fakeLLMClient{delay: 16 * time.Second},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if _, err := common.ParseTestFileBundle([]byte(store.writes[feedback.ArtifactURIs[0]])); err != nil {
		t.Fatalf("timeout path should still write valid full_test_files: %v", err)
	}
}

func TestExecuteTestDataIgnoresLLMErrorAndStillSucceeds(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient:     &fakeLLMClient{err: fmt.Errorf("upstream llm failed")},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if _, err := common.ParseTestFileBundle([]byte(store.writes[feedback.ArtifactURIs[0]])); err != nil {
		t.Fatalf("error path should still write valid full_test_files: %v", err)
	}
}

func TestExecuteTestDataIgnoresLegacyArtifactOutputResponse(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient: &fakeLLMClient{
			response: `{"summary":"generated","artifact_outputs":[{"type":"test_data","filename":"full_test_files.json","content":"{}"}]}`,
		},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	content := mustReadTestDataBundleContent(t, store, feedback.ArtifactURIs[0])
	if strings.Contains(content, "artifact_outputs") {
		t.Fatalf("legacy artifact output response should be ignored:\n%s", content)
	}
}

func TestExecuteTestDataRejectsPlanWithUnknownFieldsAndStillSucceeds(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient: &fakeLLMClient{
			response: `{"schema_version":1,"kind":"test_case_plan","module_id":"module01","cases":[{"name":"bad_case","type":"boundary","target":"createModule","scenario":"something","expected":"something else","path":"hack.js"}]}`,
		},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	content := mustReadTestDataBundleContent(t, store, feedback.ArtifactURIs[0])
	if strings.Contains(content, "bad_case") || strings.Contains(content, "hack.js") {
		t.Fatalf("plan with unknown field should be rejected entirely:\n%s", content)
	}
}

func TestExecuteTestDataDropsInvalidPlanTargets(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient: &fakeLLMClient{
			response: `{"schema_version":1,"kind":"test_case_plan","module_id":"module01","cases":[{"name":"bad_target","type":"boundary","target":"missingAPI","scenario":"invalid target","expected":"should be dropped"},{"name":"good_target","type":"error","target":"createModule","scenario":"declared api target","expected":"should remain"}]}`,
		},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	content := mustReadTestDataBundleContent(t, store, feedback.ArtifactURIs[0])
	if strings.Contains(content, "bad_target") || strings.Contains(content, "missingAPI") {
		t.Fatalf("invalid target case should be dropped:\n%s", content)
	}
	for _, want := range []string{"good_target", "declared api target"} {
		if !strings.Contains(content, want) {
			t.Fatalf("valid target case missing %q:\n%s", want, content)
		}
	}
}

func TestExecuteTestDataFailsWithoutPairedModuleTask(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	store := &testArtifactStore{
		files: map[string]string{
			testerTaskURI: "# Tester Task\n",
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
		llmClient:     llm.NoopClient{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_data_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{testerTaskURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if len(feedback.ArtifactURIs) != 1 {
		t.Fatalf("failure artifact count = %d, want 1", len(feedback.ArtifactURIs))
	}
	if !strings.Contains(store.writes[feedback.ArtifactURIs[0]], "requires an artifact URI containing /artifacts/modules/") {
		t.Fatalf("failure output missing module requirement:\n%s", store.writes[feedback.ArtifactURIs[0]])
	}
}

func TestExecuteTestCodeReturnsOKWhenOpenCodeReportsPassed(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	fullTestsURI := "projects/run1/agents/tester_01/artifacts/test_data/full_test_files.json"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI:     coderBranchArtifactJSON(t, "C:/repo/project", "coder/01", "abc123", "go test ./..."),
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
			fullTestsURI:  testerFullBundleJSON(t, "module01", "node test/module01.full.test.js"),
		},
	}
	runner := &fakeOpenCodeRunner{report: testCodeExecutionReport{
		Status:      "passed",
		Summary:     "all test data passed",
		TestCommand: "go test ./...",
		TestPassed:  true,
	}}
	git := &fakeGitManager{worktreePath: worktreePath}
	agent := &Agent{
		runID:          "run1",
		agentID:        "tester_01",
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		llmClient:      &fakeLLMClient{},
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_code_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI, fullTestsURI},
	})
	if err != nil {
		t.Fatalf("execute test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("success artifact count = %d, want %d", got, want)
	}
	if git.request.RepoDir != "C:/repo/project" || git.request.BaseCommit != "abc123" {
		t.Fatalf("worktree request = %+v, want repo and coder commit", git.request)
	}
	if got, want := strings.Join(git.testCommands, "|"), "node test/module01.seed.test.js|node test/module01.full.test.js"; got != want {
		t.Fatalf("test commands = %q, want %q", got, want)
	}
	for _, written := range []string{filepath.Join(worktreePath, "test", "module01.seed.test.js"), filepath.Join(worktreePath, "test", "module01.full.test.js")} {
		if _, err := os.Stat(written); err != nil {
			t.Fatalf("expected materialized test file %s: %v", written, err)
		}
	}
}

func TestExecuteTestCodeReturnsKFailAndReportWhenOpenCodeReportsFailure(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	fullTestsURI := "projects/run1/agents/tester_01/artifacts/test_data/full_test_files.json"
	worktreePath := t.TempDir()
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI:     coderBranchArtifactJSON(t, "C:/repo/project", "coder/01", "abc123", ""),
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
			fullTestsURI:  testerFullBundleJSON(t, "module01", "node test/module01.full.test.js"),
		},
	}
	git := &fakeGitManager{
		worktreePath: worktreePath,
		testResults: []common.CommandResult{
			{ExitCode: 0, Stdout: "seed ok", Duration: time.Millisecond},
			{ExitCode: 1, Stdout: "full stdout", Stderr: "AssertionError: expected 2 got 1", Duration: time.Millisecond},
		},
		testErrors: []error{nil, fmt.Errorf("run test command %q: exit status 1", "node test/module01.full.test.js")},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "tester_01",
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		llmClient:      &fakeLLMClient{},
		openCodeRunner: &fakeOpenCodeRunner{},
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_code_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI, fullTestsURI},
	})
	if err != nil {
		t.Fatalf("execute test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeBug {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeBug)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("failure artifact count = %d, want %d", got, want)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	for _, want := range []string{"# Test Failure Report", "failed_phase: full_tests", "node test/module01.full.test.js", "AssertionError: expected 2 got 1", moduleTaskURI, branchURI, contractURI, seedURI, fullTestsURI} {
		if !strings.Contains(report, want) {
			t.Fatalf("failure report missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteTestCodeLoadsModuleTaskFromCoderBranchArtifact(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	testDataURI := "projects/run1/agents/tester_01/artifacts/test_data/tester_01_test_data.md"
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Coder Task\n\n## Pairing Metadata\n\n- module_id: engine\n",
			branchURI: branchArtifactWithModuleTaskJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       "C:/repo/project",
				Branch:        "coder/01",
				Commit:        "abc123",
				ModuleTaskURI: moduleTaskURI,
			}),
			testDataURI: "# Test Data\n",
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
	}

	inputs, err := agent.fillModuleTaskFromCoderBranch(context.Background(), testCodeInputs{
		branch:       agentDocument(branchURI, store.files[branchURI]),
		fullTestsDoc: agentDocument(testDataURI, store.files[testDataURI]),
	})
	if err != nil {
		t.Fatalf("fillModuleTaskFromCoderBranch returned error: %v", err)
	}
	if got := inputs.moduleTask.URI; got != moduleTaskURI {
		t.Fatalf("module task uri = %q, want %q", got, moduleTaskURI)
	}
	if !strings.Contains(inputs.moduleTask.Content, "module_id: engine") {
		t.Fatalf("module task content not loaded:\n%s", inputs.moduleTask.Content)
	}
}

func TestExecuteTestCodeFailsWithoutTestDataArtifact(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	contractURI := "projects/run1/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run1/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	store := &testArtifactStore{
		files: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI:     coderBranchArtifactJSON(t, "C:/repo/project", "coder/01", "abc123", ""),
			contractURI:   testerModuleContractJSON(t, "module01", "node test/module01.seed.test.js"),
			seedURI:       testerSeedBundleJSON(t, "module01", "node test/module01.seed.test.js"),
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "tester_01",
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_test_code_01",
		AgentID:      "tester_01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{moduleTaskURI, branchURI, contractURI, seedURI},
	})
	if err != nil {
		t.Fatalf("execute test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if !strings.Contains(store.writes[feedback.ArtifactURIs[0]], "full_test_files.json") {
		t.Fatalf("failure output missing full_test_files requirement:\n%s", store.writes[feedback.ArtifactURIs[0]])
	}
}

func coderBranchArtifactJSON(t *testing.T, repoDir, branch, commit, testCommand string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(common.CoderBranchArtifact{
		SchemaVersion: 1,
		Kind:          "coder_branch",
		RepoDir:       repoDir,
		Branch:        branch,
		Commit:        commit,
		TestCommand:   testCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func branchArtifactWithModuleTaskJSON(t *testing.T, artifact common.CoderBranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func testerModuleContractJSON(t *testing.T, moduleID string, seedCommand string) string {
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

func testerFrontendModuleContractJSON(t *testing.T, moduleID string, seedCommand string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(map[string]any{
		"schema_version":             1,
		"kind":                       "module_contract",
		"module_id":                  moduleID,
		"module_name":                moduleID,
		"delivery_profile":           "frontend_web",
		"entry_files":                []string{"index.html"},
		"public_api":                 []map[string]string{{"name": "frontend_web_app", "kind": "browser_app"}},
		"official_seed_test_command": seedCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func testerSeedBundleJSON(t *testing.T, moduleID string, command string) string {
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

func testerFullBundleJSON(t *testing.T, moduleID string, command string) string {
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

func agentDocument(uri, content string) agentengine.ArtifactDocument {
	return agentengine.ArtifactDocument{URI: uri, Content: content}
}

func mustParseTestBundle(t *testing.T, raw string) common.TestFileBundle {
	t.Helper()
	bundle, err := common.ParseTestFileBundle([]byte(raw))
	if err != nil {
		t.Fatalf("ParseTestFileBundle returned error: %v\n%s", err, raw)
	}
	return bundle
}

func mustReadTestDataBundleContent(t *testing.T, store *testArtifactStore, uri string) string {
	t.Helper()
	bundle, err := common.ParseTestFileBundle([]byte(store.writes[uri]))
	if err != nil {
		t.Fatalf("ParseTestFileBundle returned error: %v\n%s", err, store.writes[uri])
	}
	if len(bundle.TestFiles) == 0 {
		t.Fatal("generated bundle has no test files")
	}
	return bundle.TestFiles[0].Content
}

var _ common.OpenCodeRunner = (*fakeOpenCodeRunner)(nil)
var _ common.GitManager = (*fakeGitManager)(nil)
