package architect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doujia/internal/agent/core"
	"doujia/internal/agent/llm"
	architectspec "doujia/internal/agent/spec/architect"
)

type stubToolLoop struct {
	result core.AgentResult
	err    error
}

func (s stubToolLoop) Run(_ context.Context, _ core.ToolLoopRequest) (core.AgentResult, error) {
	return s.result, s.err
}

type callbackToolLoop struct {
	result core.AgentResult
	err    error
	onRun  func()
}

func (s callbackToolLoop) Run(_ context.Context, _ core.ToolLoopRequest) (core.AgentResult, error) {
	if s.onRun != nil {
		s.onRun()
	}
	return s.result, s.err
}

type stubHandlerRegistry struct{}

func (stubHandlerRegistry) Get(string) (core.Handler, bool) {
	return nil, false
}

type stubArtifactWriteHandler struct {
	outputDir string
	spec      core.OpSpec
	mutate    func(logicalKey, content string) string
}

func (h stubArtifactWriteHandler) Name() string { return "artifact_write" }

func (h stubArtifactWriteHandler) Description() string { return "stub artifact write" }

func (h stubArtifactWriteHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{Name: "artifact_write"}
}

func (h stubArtifactWriteHandler) Handle(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	logicalKey, _ := req.Args["logical_key"].(string)
	content, _ := req.Args["content"].(string)
	if h.mutate != nil {
		content = h.mutate(logicalKey, content)
	}
	output, ok := h.spec.FindOutput(logicalKey)
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("unknown output logical key %q", logicalKey)
	}
	path := filepath.Join(h.outputDir, filepath.FromSlash(output.FileName))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return core.HandlerResponse{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return core.HandlerResponse{}, err
	}
	return core.HandlerResponse{
		Data: map[string]any{
			"object_type": output.ObjectType,
			"status":      "produced",
			"path":        path,
		},
	}, nil
}

type mapHandlerRegistry struct {
	handlers map[string]core.Handler
}

func (r mapHandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

type stubHandler struct {
	name       string
	handleFunc func(context.Context, core.HandlerRequest) (core.HandlerResponse, error)
}

func (h stubHandler) Name() string { return h.name }

func (h stubHandler) Description() string { return h.name }

func (h stubHandler) ToolSpec() core.ToolSpec { return core.ToolSpec{Name: h.name} }

func (h stubHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if h.handleFunc == nil {
		return core.HandlerResponse{}, fmt.Errorf("unexpected handle for %s", h.name)
	}
	return h.handleFunc(ctx, req)
}

type stubSequentialLLM struct {
	responses []llm.ChatResponse
	errs      []error
	calls     int
}

func (s *stubSequentialLLM) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	index := s.calls
	s.calls++
	if index < len(s.errs) && s.errs[index] != nil {
		return llm.ChatResponse{}, s.errs[index]
	}
	if index < len(s.responses) {
		return s.responses[index], nil
	}
	return llm.ChatResponse{}, fmt.Errorf("unexpected llm call %d", index+1)
}

func TestArchitectWritePlanRejectsInvalidEnvironmentSpecOutput(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	envPath := filepath.Join(outputDir, "environment_spec.json")
	if err := os.WriteFile(envPath, []byte(`{"runtime":"node","image":"node:20-bookworm","package_manager":"npm","check_commands":["echo nope"],"default_test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("write env spec: %v", err)
	}
	planPath := filepath.Join(outputDir, "architecture_v1.md")
	if err := os.WriteFile(planPath, []byte("# plan"), 0o644); err != nil {
		t.Fatalf("write architecture plan: %v", err)
	}

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "write_plan", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
		},
		OpSpec:   architectspec.WritePlanSpec(),
		Handlers: stubHandlerRegistry{},
		ToolLoop: stubToolLoop{
			result: core.AgentResult{
				Result: "kok",
				Outputs: []core.AgentOutput{
					{LogicalKey: core.LKArchitecturePlan, ObjectType: "markdown", Status: "produced", Path: planPath},
					{LogicalKey: core.LKEnvironmentSpec, ObjectType: "json", Status: "produced", Path: envPath},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "environment_spec") {
		t.Fatalf("Run() errors = %+v, want environment_spec validation error", result.Errors)
	}
}

func TestArchitectSplitModuleRejectsInvalidModuleSpecOutput(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planPath := filepath.Join(outputDir, "architecture_v1.md")
	containerPath := filepath.Join(outputDir, "container_context.json")
	envPath := filepath.Join(outputDir, "environment_spec.json")
	if err := os.WriteFile(planPath, []byte("# Architecture Plan\n\nfrontend and backend\n"), 0o644); err != nil {
		t.Fatalf("write architecture plan: %v", err)
	}
	if err := os.WriteFile(containerPath, []byte(`{"repo_dir":"/workspace/repo","worktrees_dir":"/workspace/worktrees","test_runs_dir":"/workspace/test-runs","base_branch":"main","branch_prefix":"feature/","default_test_command":"cd server && npm test"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(envPath, []byte(`{"runtime":"node","image":"node:20-bookworm","package_manager":"npm","check_commands":["node --version","npm --version"],"default_test_command":"cd server && npm test"}`), 0o644); err != nil {
		t.Fatalf("write env spec: %v", err)
	}

	spec := architectspec.SplitModuleSpec()
	bundle := core.AgentInputBundle{
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKArchitecturePlan, Path: planPath},
			{LogicalKey: core.LKContainerContext, Path: containerPath},
			{LogicalKey: core.LKEnvironmentSpec, Path: envPath},
		},
	}
	resolved, err := spec.ResolveExpectedOutputs(bundle)
	if err != nil {
		t.Fatalf("ResolveExpectedOutputs() error = %v", err)
	}
	spec.ExpectedOutputs = resolved

	agent := NewAgent()
	llmClient := &stubSequentialLLM{
		responses: []llm.ChatResponse{
			{Message: llm.Message{Role: "assistant", Content: `{"modules":[{"module_id":"module01","module_role":"frontend","module_name":"frontend_pet_journal","owned_paths":["pages/home/**","pages/pet/**","components/**","utils/request.js","app.js","app.json","app.wxss"],"responsibilities":["Implement mini-program pages and request flow"],"coder_focus":["Keep frontend API calls in one request helper"],"tester_focus":["Validate page rendering and API error states"]},{"module_id":"module02","module_role":"backend","module_name":"backend_pet_summary","owned_paths":["server/routes/pets.js","server/models/pet.js"],"responsibilities":["Implement pet CRUD and summary endpoints"],"coder_focus":["Keep pet DTO fields stable for frontend"],"tester_focus":["Validate pet CRUD and summary responses"]}],"shared_contract":{"api_prefix":"/api","frontend_base_url_strategy":"Use one shared request helper to prepend the configured backend base URL","merge_rules":["Do not edit files outside owned_paths"],"integration_checks":["Frontend and backend must agree on pet_id fields"]}}`}},
		},
	}
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "architect", Op: "split_module", ExecutionMode: "normal"},
		Bundle: bundle,
		OpSpec: spec,
		LLM:    llmClient,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
				mutate: func(logicalKey, content string) string {
					if logicalKey == core.LKModule02Spec {
						return strings.Replace(content, `"module_id": "module02"`, `"module_id": ""`, 1)
					}
					return content
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
}

func TestArchitectSplitModuleRepairsSemanticPlanOnce(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	planPath := filepath.Join(outputDir, "architecture_v1.md")
	containerPath := filepath.Join(outputDir, "container_context.json")
	envPath := filepath.Join(outputDir, "environment_spec.json")
	configPath := filepath.Join(outputDir, "run_delivery_config.json")
	if err := os.WriteFile(planPath, []byte("# Architecture Plan\n\n/pages/home/home\n/pages/pet/pet\n/pages/records/records\nserver/routes/pets.js\nserver/routes/feedings.js\nserver/models/pet.js\nserver/models/record.js\n"), 0o644); err != nil {
		t.Fatalf("write architecture plan: %v", err)
	}
	if err := os.WriteFile(containerPath, []byte(`{"repo_dir":"/workspace/repo","worktrees_dir":"/workspace/worktrees","test_runs_dir":"/workspace/test-runs","base_branch":"main","branch_prefix":"feature/","default_test_command":"cd server && npm test"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(envPath, []byte(`{"runtime":"node","image":"node:20-bookworm","package_manager":"npm","check_commands":["node --version","npm --version"],"default_test_command":"cd server && npm test"}`), 0o644); err != nil {
		t.Fatalf("write env spec: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"backend_module_count":2}`), 0o644); err != nil {
		t.Fatalf("write run_delivery_config: %v", err)
	}

	spec := architectspec.SplitModuleSpec()
	bundle := core.AgentInputBundle{
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKArchitecturePlan, Path: planPath},
			{LogicalKey: core.LKContainerContext, Path: containerPath},
			{LogicalKey: core.LKEnvironmentSpec, Path: envPath},
			{LogicalKey: core.LKRunDeliveryConfig, Path: configPath},
		},
	}
	resolved, err := spec.ResolveExpectedOutputs(bundle)
	if err != nil {
		t.Fatalf("ResolveExpectedOutputs() error = %v", err)
	}
	spec.ExpectedOutputs = resolved

	llmClient := &stubSequentialLLM{
		responses: []llm.ChatResponse{
			{Message: llm.Message{Role: "assistant", Content: `{"modules":[{"module_id":"module01","module_role":"backend","module_name":"wrong_frontend","owned_paths":["server/routes/pets.js"],"responsibilities":["bad"],"coder_focus":["bad"],"tester_focus":["bad"]},{"module_id":"module02","module_role":"backend","module_name":"backend_pet_summary","owned_paths":["server/models/pet.js"],"responsibilities":["pet CRUD"],"coder_focus":["dto"],"tester_focus":["api"]},{"module_id":"module03","module_role":"backend","module_name":"backend_record_services","owned_paths":["server/routes/feedings.js","server/models/record.js"],"responsibilities":["record CRUD"],"coder_focus":["record payload"],"tester_focus":["record api"]}],"shared_contract":{"api_prefix":"/api","frontend_base_url_strategy":"Use one shared request helper","merge_rules":["Do not edit files outside owned_paths"],"integration_checks":["Frontend and backend must agree on pet_id fields"]}}`}},
			{Message: llm.Message{Role: "assistant", Content: `{"modules":[{"module_id":"module01","module_role":"frontend","module_name":"frontend_pet_journal","owned_paths":["pages/home/**","pages/pet/**","pages/records/**","components/**","utils/request.js","app.js","app.json","app.wxss"],"responsibilities":["Implement mini-program pages and request flow"],"coder_focus":["Keep frontend API calls in one request helper"],"tester_focus":["Validate page rendering and API error states"]},{"module_id":"module02","module_role":"backend","module_name":"backend_pet_summary","owned_paths":["server/routes/pets.js","server/models/pet.js"],"responsibilities":["Implement pet CRUD and summary endpoints"],"coder_focus":["Keep pet DTO fields stable for frontend"],"tester_focus":["Validate pet CRUD and summary responses"]},{"module_id":"module03","module_role":"backend","module_name":"backend_record_services","owned_paths":["server/routes/feedings.js","server/models/record.js"],"responsibilities":["Implement record CRUD flows"],"coder_focus":["Keep record type payloads consistent"],"tester_focus":["Validate record CRUD by type"]}],"shared_contract":{"api_prefix":"/api","frontend_base_url_strategy":"Use one shared request helper to prepend the configured backend base URL","merge_rules":["Do not edit files outside owned_paths"],"integration_checks":["Frontend and backend must agree on pet_id and record type fields"]}}`}},
		},
	}

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "architect", Op: "split_module", ExecutionMode: "normal"},
		Bundle: bundle,
		OpSpec: spec,
		LLM:    llmClient,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if llmClient.calls != 2 {
		t.Fatalf("LLM calls = %d, want 2 (initial + repair)", llmClient.calls)
	}
	module01Path := filepath.Join(outputDir, "module_specs", "module01_spec.json")
	body, err := os.ReadFile(module01Path)
	if err != nil {
		t.Fatalf("read module01 spec: %v", err)
	}
	if !strings.Contains(string(body), `"pages/home/**"`) {
		t.Fatalf("module01 spec = %s, want repaired semantic frontend owned_paths", string(body))
	}
}

func TestArchitectTestDataRejectsInvalidGlobalTestCommands(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	acceptancePath := filepath.Join(outputDir, "global_acceptance_tests.json")
	commandsPath := filepath.Join(outputDir, "global_test_commands.json")
	if err := os.WriteFile(acceptancePath, []byte(`{"kind":"global_acceptance_tests","target":"merged_main_branch","scenarios":[{"name":"happy path"}]}`), 0o644); err != nil {
		t.Fatalf("write acceptance: %v", err)
	}
	if err := os.WriteFile(commandsPath, []byte(`{"kind":"global_test_commands","commands":[{"name":"smoke","command":"","cwd_from":"/workspace/repo"}]}`), 0o644); err != nil {
		t.Fatalf("write commands: %v", err)
	}

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task:     core.Task{Role: "architect", Op: "test_data", ExecutionMode: "normal"},
		Bundle:   core.AgentInputBundle{OutputDir: outputDir},
		OpSpec:   architectspec.TestDataSpec(),
		Handlers: stubHandlerRegistry{},
		ToolLoop: stubToolLoop{
			result: core.AgentResult{
				Result: "kok",
				Outputs: []core.AgentOutput{
					{LogicalKey: core.LKGlobalTestData, ObjectType: "markdown", Status: "produced", Path: filepath.Join(outputDir, "global_test_data.md")},
					{LogicalKey: core.LKGlobalAcceptanceTests, ObjectType: "json", Status: "produced", Path: acceptancePath},
					{LogicalKey: core.LKGlobalTestCommands, ObjectType: "json", Status: "produced", Path: commandsPath},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
}

func TestArchitectTestDataReturnsKbugForMissingModuleSpecsModules(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	architecturePlanPath := filepath.Join(outputDir, "architecture_plan.md")
	environmentSpecPath := filepath.Join(outputDir, "environment_spec.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"global_test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(architecturePlanPath, []byte(`# Architecture Plan`), 0o644); err != nil {
		t.Fatalf("write architecture plan: %v", err)
	}
	if err := os.WriteFile(environmentSpecPath, []byte(`{"runtime":"node","image":"node:20-bookworm","package_manager":"npm","check_commands":["node --version"],"default_test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("write environment spec: %v", err)
	}

	spec := architectspec.TestDataSpec()
	spec.ExpectedOutputs = append(spec.ExpectedOutputs, core.OutputSpec{
		LogicalKey:  core.LKUpstreamArtifactIssue,
		ObjectType:  "markdown",
		FileName:    "upstream_artifact_issue.md",
		Description: "Upstream issue report.",
		Required:    false,
	})

	toolLoopCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "test_data", ExecutionMode: "normal", AgentID: "architect01"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKArchitecturePlan, Path: architecturePlanPath},
				{LogicalKey: core.LKEnvironmentSpec, Path: environmentSpecPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
		}},
		ToolLoop: callbackToolLoop{
			result: core.AgentResult{Result: "kok"},
			onRun:  func() { toolLoopCalled = true },
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if toolLoopCalled {
		t.Fatal("Run() called tool loop despite invalid module_specs")
	}
	if result.Result != "kbug" {
		t.Fatalf("Run() result = %q, want kbug", result.Result)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "upstream_artifact_issue.md"))
	if readErr != nil {
		t.Fatalf("read upstream_artifact_issue.md: %v", readErr)
	}
	if got := string(body); !strings.Contains(got, "module_specs") || !strings.Contains(got, "architect.test_data") {
		t.Fatalf("upstream_artifact_issue.md = %q, want module_specs and architect.test_data details", got)
	}
}

func TestArchitectMergeCodeBlocksBaseCommitMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	module01BranchPath := filepath.Join(outputDir, "module01_coder_branch.json")
	module02BranchPath := filepath.Join(outputDir, "module02_coder_branch.json")
	module01ReportPath := filepath.Join(outputDir, "module01_module_test_report.json")
	module02ReportPath := filepath.Join(outputDir, "module02_module_test_report.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"modules":[{"module_id":"module01","module_name":"frontend"},{"module_id":"module02","module_name":"backend"}]}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(module01BranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","result":"kok","base_branch":"main","base_commit":"aaa111","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 branch: %v", err)
	}
	if err := os.WriteFile(module02BranchPath, []byte(`{"module_id":"module02","branch":"feature/module02","commit":"def456","result":"kok","base_branch":"main","base_commit":"bbb222","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module02 branch: %v", err)
	}
	if err := os.WriteFile(module01ReportPath, []byte(`{"module_id":"module01","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 report: %v", err)
	}
	if err := os.WriteFile(module02ReportPath, []byte(`{"module_id":"module02","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module02 report: %v", err)
	}

	spec := architectspec.MergeCodeSpec()
	spec.ExpectedOutputs = append(spec.ExpectedOutputs, core.OutputSpec{
		LogicalKey: core.LKMergeCodeReport,
		ObjectType: "markdown",
		FileName:   "merge_code_report.md",
		Required:   false,
	})

	cherryPickCalled := false
	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKModule01CoderBranch, Path: module01BranchPath},
				{LogicalKey: core.LKModule02CoderBranch, Path: module02BranchPath},
				{LogicalKey: core.LKModule01ModuleTestReport, Path: module01ReportPath},
				{LogicalKey: core.LKModule02ModuleTestReport, Path: module02ReportPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_git_cherry_pick": stubHandler{
				name: "container_git_cherry_pick",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					cherryPickCalled = true
					return core.HandlerResponse{
						Data: map[string]any{
							"result":        "ok",
							"merged_commit": "merged123",
						},
					}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cherryPickCalled {
		t.Fatal("Run() called cherry-pick despite base_commit mismatch")
	}
	if len(result.Outputs) == 0 {
		t.Fatalf("Run() outputs = %+v, want failure artifacts", result.Outputs)
	}
	mergedBody, readErr := os.ReadFile(filepath.Join(outputDir, "merged_main_branch.json"))
	if readErr != nil {
		t.Fatalf("read merged_main_branch.json: %v", readErr)
	}
	if !strings.Contains(string(mergedBody), `"result": "kfail"`) {
		t.Fatalf("merged_main_branch.json = %s, want kfail result", string(mergedBody))
	}
	reportBody, readErr := os.ReadFile(filepath.Join(outputDir, "merge_code_report.md"))
	if readErr != nil {
		t.Fatalf("read merge_code_report.md: %v", readErr)
	}
	if !strings.Contains(string(reportBody), "base commit") && !strings.Contains(string(reportBody), "base_commit") {
		t.Fatalf("merge_code_report.md = %s, want base commit explanation", string(reportBody))
	}
}

func TestArchitectTestCodeReturnsKbugForMissingGlobalTestCommandsCommands(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	mergedPath := filepath.Join(outputDir, "merged_main_branch.json")
	globalTestDataPath := filepath.Join(outputDir, "global_test_data.md")
	acceptancePath := filepath.Join(outputDir, "global_acceptance_tests.json")
	commandsPath := filepath.Join(outputDir, "global_test_commands.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(mergedPath, []byte(`{"result":"kok","merged_commit":"abc123","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write merged main branch: %v", err)
	}
	if err := os.WriteFile(globalTestDataPath, []byte(`# Global Test Data`), 0o644); err != nil {
		t.Fatalf("write global test data: %v", err)
	}
	if err := os.WriteFile(acceptancePath, []byte(`{"kind":"global_acceptance_tests","target":"merged_main_branch","scenarios":[{"name":"happy path"}]}`), 0o644); err != nil {
		t.Fatalf("write acceptance tests: %v", err)
	}
	if err := os.WriteFile(commandsPath, []byte(`{"kind":"global_test_commands"}`), 0o644); err != nil {
		t.Fatalf("write global test commands: %v", err)
	}

	spec := architectspec.TestCodeSpec()
	spec.ExpectedOutputs = append(spec.ExpectedOutputs, core.OutputSpec{
		LogicalKey:  core.LKUpstreamArtifactIssue,
		ObjectType:  "markdown",
		FileName:    "upstream_artifact_issue.md",
		Description: "Upstream issue report.",
		Required:    false,
	})

	execCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "test_code", ExecutionMode: "normal", AgentID: "architect01"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKMergedMainBranch, Path: mergedPath},
				{LogicalKey: core.LKGlobalTestData, Path: globalTestDataPath},
				{LogicalKey: core.LKGlobalAcceptanceTests, Path: acceptancePath},
				{LogicalKey: core.LKGlobalTestCommands, Path: commandsPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_exec": stubHandler{
				name: "container_exec",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					execCalled = true
					return core.HandlerResponse{}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kbug" {
		t.Fatalf("Run() result = %q, want kbug", result.Result)
	}
	if execCalled {
		t.Fatal("Run() called container_exec despite invalid global_test_commands")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "upstream_artifact_issue.md"))
	if readErr != nil {
		t.Fatalf("read upstream_artifact_issue.md: %v", readErr)
	}
	if got := string(body); !strings.Contains(got, "global_test_commands") || !strings.Contains(got, "architect.test_code") {
		t.Fatalf("upstream_artifact_issue.md = %q, want global_test_commands and architect.test_code details", got)
	}
}

func TestArchitectTestCodeKeepsFailureArtifactsWhenCommandExecutionFails(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	mergedPath := filepath.Join(outputDir, "merged_main_branch.json")
	globalTestDataPath := filepath.Join(outputDir, "global_test_data.md")
	acceptancePath := filepath.Join(outputDir, "global_acceptance_tests.json")
	commandsPath := filepath.Join(outputDir, "global_test_commands.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(mergedPath, []byte(`{"result":"kok","merged_commit":"abc123","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write merged main branch: %v", err)
	}
	if err := os.WriteFile(globalTestDataPath, []byte(`# Global Test Data`), 0o644); err != nil {
		t.Fatalf("write global test data: %v", err)
	}
	if err := os.WriteFile(acceptancePath, []byte(`{"kind":"global_acceptance_tests","target":"merged_main_branch","scenarios":[{"name":"happy path"}]}`), 0o644); err != nil {
		t.Fatalf("write acceptance tests: %v", err)
	}
	if err := os.WriteFile(commandsPath, []byte(`{"kind":"global_test_commands","commands":[{"name":"smoke","command":"npm test","cwd_from":"/workspace/repo"}]}`), 0o644); err != nil {
		t.Fatalf("write global test commands: %v", err)
	}

	spec := architectspec.TestCodeSpec()
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "test_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKMergedMainBranch, Path: mergedPath},
				{LogicalKey: core.LKGlobalTestData, Path: globalTestDataPath},
				{LogicalKey: core.LKGlobalAcceptanceTests, Path: acceptancePath},
				{LogicalKey: core.LKGlobalTestCommands, Path: commandsPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_exec": stubHandler{
				name: "container_exec",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					return core.HandlerResponse{
						Data: map[string]any{
							"exit_code":   1,
							"stdout":      "",
							"stderr":      "test failed",
							"duration_ms": int64(12),
						},
					}, fmt.Errorf("process exited with code 1")
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	for _, output := range result.Outputs {
		if output.LogicalKey == core.LKUpstreamArtifactIssue {
			t.Fatalf("Run() outputs = %+v, do not want upstream_artifact_issue for execution failure", result.Outputs)
		}
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "upstream_artifact_issue.md")); !os.IsNotExist(statErr) {
		t.Fatalf("upstream_artifact_issue.md exists unexpectedly: %v", statErr)
	}
	reportBody, readErr := os.ReadFile(filepath.Join(outputDir, "global_test_report.json"))
	if readErr != nil {
		t.Fatalf("read global_test_report.json: %v", readErr)
	}
	if !strings.Contains(string(reportBody), `"result": "kfail"`) {
		t.Fatalf("global_test_report.json = %s, want kfail result", string(reportBody))
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "delivery_guide.md")); statErr != nil {
		t.Fatalf("delivery_guide.md missing: %v", statErr)
	}
}

func TestArchitectMergeCodeBlocksBaseBranchMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	module01BranchPath := filepath.Join(outputDir, "module01_coder_branch.json")
	module01ReportPath := filepath.Join(outputDir, "module01_module_test_report.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"modules":[{"module_id":"module01","module_name":"frontend"}]}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(module01BranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","result":"kok","base_branch":"release","base_commit":"aaa111","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 branch: %v", err)
	}
	if err := os.WriteFile(module01ReportPath, []byte(`{"module_id":"module01","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 report: %v", err)
	}

	spec := architectspec.MergeCodeSpec()
	spec.ExpectedOutputs = append(spec.ExpectedOutputs, core.OutputSpec{
		LogicalKey: core.LKMergeCodeReport,
		ObjectType: "markdown",
		FileName:   "merge_code_report.md",
		Required:   false,
	})

	cherryPickCalled := false
	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKModule01CoderBranch, Path: module01BranchPath},
				{LogicalKey: core.LKModule01ModuleTestReport, Path: module01ReportPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_git_cherry_pick": stubHandler{
				name: "container_git_cherry_pick",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					cherryPickCalled = true
					return core.HandlerResponse{
						Data: map[string]any{
							"result":        "ok",
							"merged_commit": "merged123",
						},
					}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cherryPickCalled {
		t.Fatal("Run() called cherry-pick despite base_branch mismatch")
	}
	if len(result.Outputs) == 0 {
		t.Fatalf("Run() outputs = %+v, want failure artifacts", result.Outputs)
	}
	reportBody, readErr := os.ReadFile(filepath.Join(outputDir, "merge_code_report.md"))
	if readErr != nil {
		t.Fatalf("read merge_code_report.md: %v", readErr)
	}
	if !strings.Contains(string(reportBody), "base_branch") && !strings.Contains(string(reportBody), "base branch") {
		t.Fatalf("merge_code_report.md = %s, want base branch explanation", string(reportBody))
	}
}

func TestArchitectMergeCodeReturnsKbugForMalformedUpstreamModuleReport(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	module01BranchPath := filepath.Join(outputDir, "module01_coder_branch.json")
	module01ReportPath := filepath.Join(outputDir, "module01_module_test_report.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"modules":[{"module_id":"module01","module_name":"frontend"}]}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(module01BranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","result":"kok","base_branch":"main","base_commit":"aaa111","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 branch: %v", err)
	}
	if err := os.WriteFile(module01ReportPath, []byte(`not-json`), 0o644); err != nil {
		t.Fatalf("write module01 report: %v", err)
	}

	spec := architectspec.MergeCodeSpec()
	cherryPickCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal", AgentID: "architect01"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKModule01CoderBranch, Path: module01BranchPath},
				{LogicalKey: core.LKModule01ModuleTestReport, Path: module01ReportPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_git_cherry_pick": stubHandler{
				name: "container_git_cherry_pick",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					cherryPickCalled = true
					return core.HandlerResponse{}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kbug" {
		t.Fatalf("Run() result = %q, want kbug", result.Result)
	}
	if cherryPickCalled {
		t.Fatal("Run() called cherry-pick despite malformed upstream module report")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
}

func TestArchitectMergeCodeReturnsKbugForMissingCoderBranchBaseCommit(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	module01BranchPath := filepath.Join(outputDir, "module01_coder_branch.json")
	module01ReportPath := filepath.Join(outputDir, "module01_module_test_report.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"modules":[{"module_id":"module01","module_name":"frontend"}]}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(module01BranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","result":"kok","base_branch":"main","base_commit":"","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 branch: %v", err)
	}
	if err := os.WriteFile(module01ReportPath, []byte(`{"module_id":"module01","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 report: %v", err)
	}

	spec := architectspec.MergeCodeSpec()
	cherryPickCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal", AgentID: "architect01"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKModule01CoderBranch, Path: module01BranchPath},
				{LogicalKey: core.LKModule01ModuleTestReport, Path: module01ReportPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_git_cherry_pick": stubHandler{
				name: "container_git_cherry_pick",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					cherryPickCalled = true
					return core.HandlerResponse{}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kbug" {
		t.Fatalf("Run() result = %q, want kbug", result.Result)
	}
	if cherryPickCalled {
		t.Fatal("Run() called cherry-pick despite missing coder_branch.base_commit")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "upstream_artifact_issue.md"))
	if readErr != nil {
		t.Fatalf("read upstream_artifact_issue.md: %v", readErr)
	}
	if got := string(body); !strings.Contains(got, "base_commit") || !strings.Contains(got, "architect.merge_code") {
		t.Fatalf("upstream_artifact_issue.md = %q, want base_commit and architect.merge_code details", got)
	}
}

func TestArchitectMergeCodeKeepsMergeFailureArtifactsWhenModuleTestsFail(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	module01BranchPath := filepath.Join(outputDir, "module01_coder_branch.json")
	module01ReportPath := filepath.Join(outputDir, "module01_module_test_report.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"modules":[{"module_id":"module01","module_name":"frontend"}]}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(module01BranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","result":"kok","base_branch":"main","base_commit":"aaa111","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 branch: %v", err)
	}
	if err := os.WriteFile(module01ReportPath, []byte(`{"module_id":"module01","result":"kfail","test_passed":false}`), 0o644); err != nil {
		t.Fatalf("write module01 report: %v", err)
	}

	spec := architectspec.MergeCodeSpec()
	cherryPickCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKModule01CoderBranch, Path: module01BranchPath},
				{LogicalKey: core.LKModule01ModuleTestReport, Path: module01ReportPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_git_cherry_pick": stubHandler{
				name: "container_git_cherry_pick",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					cherryPickCalled = true
					return core.HandlerResponse{}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok merge failure semantics", result.Result)
	}
	if cherryPickCalled {
		t.Fatal("Run() called cherry-pick despite failed module tests")
	}
	assertMergeFailureOutputsWithoutUpstreamIssue(t, outputDir, result, "kfail")
}

func TestArchitectMergeCodeKeepsMergeFailureArtifactsWhenCherryPickConflicts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	containerPath := filepath.Join(outputDir, "container_context.json")
	moduleSpecsPath := filepath.Join(outputDir, "module_specs.json")
	module01BranchPath := filepath.Join(outputDir, "module01_coder_branch.json")
	module01ReportPath := filepath.Join(outputDir, "module01_module_test_report.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("write container context: %v", err)
	}
	if err := os.WriteFile(moduleSpecsPath, []byte(`{"modules":[{"module_id":"module01","module_name":"frontend"}]}`), 0o644); err != nil {
		t.Fatalf("write module specs: %v", err)
	}
	if err := os.WriteFile(module01BranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","result":"kok","base_branch":"main","base_commit":"aaa111","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 branch: %v", err)
	}
	if err := os.WriteFile(module01ReportPath, []byte(`{"module_id":"module01","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("write module01 report: %v", err)
	}

	spec := architectspec.MergeCodeSpec()
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpecs, Path: moduleSpecsPath},
				{LogicalKey: core.LKModule01CoderBranch, Path: module01BranchPath},
				{LogicalKey: core.LKModule01ModuleTestReport, Path: module01ReportPath},
			},
		},
		OpSpec: spec,
		Handlers: mapHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubArtifactWriteHandler{
				outputDir: outputDir,
				spec:      spec,
			},
			"container_git_cherry_pick": stubHandler{
				name: "container_git_cherry_pick",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					return core.HandlerResponse{
						Data: map[string]any{
							"result":        "conflict",
							"failed_commit": "abc123",
							"stderr":        "merge conflict",
						},
					}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok merge failure semantics", result.Result)
	}
	assertMergeFailureOutputsWithoutUpstreamIssue(t, outputDir, result, "kbug")
}

func assertMergeFailureOutputsWithoutUpstreamIssue(t *testing.T, outputDir string, result core.AgentResult, mergedResult string) {
	t.Helper()

	if len(result.Outputs) == 0 {
		t.Fatalf("Run() outputs = %+v, want merge failure artifacts", result.Outputs)
	}
	for _, output := range result.Outputs {
		if output.LogicalKey == core.LKUpstreamArtifactIssue {
			t.Fatalf("Run() outputs = %+v, do not want upstream_artifact_issue for merge failure semantics", result.Outputs)
		}
	}

	mergedBody, readErr := os.ReadFile(filepath.Join(outputDir, "merged_main_branch.json"))
	if readErr != nil {
		t.Fatalf("read merged_main_branch.json: %v", readErr)
	}
	if !strings.Contains(string(mergedBody), `"result": "`+mergedResult+`"`) {
		t.Fatalf("merged_main_branch.json = %s, want result %s", string(mergedBody), mergedResult)
	}

	if _, statErr := os.Stat(filepath.Join(outputDir, "upstream_artifact_issue.md")); !os.IsNotExist(statErr) {
		t.Fatalf("upstream_artifact_issue.md exists unexpectedly: %v", statErr)
	}

	reportBody, readErr := os.ReadFile(filepath.Join(outputDir, "merge_code_report.md"))
	if readErr != nil {
		t.Fatalf("read merge_code_report.md: %v", readErr)
	}
	if got := string(reportBody); strings.TrimSpace(got) == "" {
		t.Fatal("merge_code_report.md is empty")
	}
}
