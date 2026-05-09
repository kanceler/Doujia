package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devflow/internal/agent/core"
	appcore "devflow/internal/core"
	"devflow/internal/pipeline"
)

func TestDefaultRuntimeSupportsFrontWriteCodeReuse(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()

	bundle := core.AgentInputBundle{
		InputDir:  inputDir,
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKContainerContext, Path: writeTestFile(t, inputDir, "container_context.json", `{"container_id":"c1"}`)},
			{LogicalKey: core.LKModuleSpec, Path: writeTestFile(t, inputDir, "module_spec.json", `{"module_id":"module01","module_name":"frontend","module_role":"frontend","implementation_role":"front","branch_name":"feature/module01-frontend","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`)},
			{LogicalKey: core.LKCoderTask, Path: writeTestFile(t, inputDir, "coder_task.md", "# Front task")},
			{LogicalKey: core.LKModuleContract, Path: writeTestFile(t, inputDir, "module_contract.md", "# Contract")},
			{LogicalKey: core.LKSeedTests, Path: writeTestFile(t, inputDir, "seed_tests.json", `{"files":[]}`)},
		},
		PreviousOutputs: []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKCoderBranch,
				ArtifactVersionID: "ver-front-branch",
				Path:              filepath.Join(outputDir, "coder_branch.json"),
				ObjectType:        "json",
				Description:       "reused branch output",
			},
		},
	}
	bundle = withModuleInputBag(bundle)

	runtime := NewDefaultRuntime()
	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "front",
		Op:            "write_code",
		ExecutionMode: "reuse",
	}, bundle)
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("RunAgent() result = %q, want %q; errors = %+v", result.Result, "kok", result.Errors)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("RunAgent() outputs len = %d, want 1", len(result.Outputs))
	}
	output := result.Outputs[0]
	if output.LogicalKey != core.LKCoderBranch {
		t.Fatalf("RunAgent() output logical_key = %q, want %q", output.LogicalKey, core.LKCoderBranch)
	}
	if output.Status != "reused" {
		t.Fatalf("RunAgent() output status = %q, want %q", output.Status, "reused")
	}
	if output.ArtifactVersionID != "ver-front-branch" {
		t.Fatalf("RunAgent() output artifact_version_id = %q, want %q", output.ArtifactVersionID, "ver-front-branch")
	}
}

func TestNewBuiltinPluginRegistryBuildsDefaultRolesAndOps(t *testing.T) {
	t.Parallel()

	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinPluginRegistry() error = %v", err)
	}

	if _, ok := plugins.Handlers().Get("artifact_write"); !ok {
		t.Fatal("artifact_write handler missing")
	}
	if _, ok := plugins.Ops().Get("pm", "write_plan"); !ok {
		t.Fatal("pm.write_plan op missing")
	}
	if _, ok := plugins.Ops().Get("architect", "merge_code"); !ok {
		t.Fatal("architect.merge_code op missing")
	}
	if _, ok := plugins.Ops().Get("front", "preview_edit"); !ok {
		t.Fatal("front.preview_edit op missing")
	}
	if _, ok := plugins.Ops().Get("front", "user_preview_confirm"); !ok {
		t.Fatal("front.user_preview_confirm op missing")
	}
	if _, ok := plugins.Agents().Get("tester"); !ok {
		t.Fatal("tester role missing")
	}
}

func TestNewBuiltinPluginRegistryKeepsDistinctWritePlanOpIDsAcrossRoles(t *testing.T) {
	t.Parallel()

	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinPluginRegistry() error = %v", err)
	}

	pmSpec, ok := plugins.Ops().GetByID("pm.write_plan")
	if !ok {
		t.Fatal("pm.write_plan op_id missing")
	}
	architectSpec, ok := plugins.Ops().GetByID("architect.write_plan")
	if !ok {
		t.Fatal("architect.write_plan op_id missing")
	}
	if pmSpec.Role != "pm" || pmSpec.Op != "write_plan" {
		t.Fatalf("pm.write_plan spec = %+v, want role=pm op=write_plan", pmSpec)
	}
	if architectSpec.Role != "architect" || architectSpec.Op != "write_plan" {
		t.Fatalf("architect.write_plan spec = %+v, want role=architect op=write_plan", architectSpec)
	}
}

func TestBuiltinWritePlanOpsResolveDistinctProducedBags(t *testing.T) {
	t.Parallel()

	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinPluginRegistry() error = %v", err)
	}

	pmSpec, ok := plugins.Ops().GetByID("pm.write_plan")
	if !ok {
		t.Fatal("pm.write_plan op_id missing")
	}
	architectSpec, ok := plugins.Ops().GetByID("architect.write_plan")
	if !ok {
		t.Fatal("architect.write_plan op_id missing")
	}

	pmBags := pmSpec.ResolveProducedBags(core.Task{
		Role: "pm",
		Op:   "write_plan",
		OpID: "pm.write_plan",
	}, core.AgentInputBundle{}, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{LogicalKey: core.LKPMPlan, ObjectType: "markdown", Status: "produced", Path: "/tmp/plan_v1.md", ArtifactURI: "projects/run/agents/pm01/artifacts/pm_write_plan/plan_v1.md"},
		},
	})
	if len(pmBags) != 1 || pmBags[0].Name != "product_plan" {
		t.Fatalf("pm produced bags = %+v, want product_plan", pmBags)
	}

	architectBags := architectSpec.ResolveProducedBags(core.Task{
		Role: "architect",
		Op:   "write_plan",
		OpID: "architect.write_plan",
	}, core.AgentInputBundle{}, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{LogicalKey: core.LKArchitecturePlan, ObjectType: "markdown", Status: "produced", Path: "/tmp/architecture_v1.md", ArtifactURI: "projects/run/agents/architect01/artifacts/architect_write_plan/architecture_v1.md"},
			{LogicalKey: core.LKEnvironmentSpec, ObjectType: "json", Status: "produced", Path: "/tmp/environment_spec.json", ArtifactURI: "projects/run/agents/architect01/artifacts/architect_write_plan/environment_spec.json"},
		},
	})
	if len(architectBags) != 1 || architectBags[0].Name != "architecture" {
		t.Fatalf("architect produced bags = %+v, want architecture", architectBags)
	}
}

func TestBuiltinSplitModuleOpResolvesModuleAndGlobalProducedBags(t *testing.T) {
	t.Parallel()

	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinPluginRegistry() error = %v", err)
	}

	spec, ok := plugins.Ops().GetByID("architect.split_module")
	if !ok {
		t.Fatal("architect.split_module op_id missing")
	}

	bags := spec.ResolveProducedBags(core.Task{
		Role: "architect",
		Op:   "split_module",
		OpID: "architect.split_module",
	}, core.AgentInputBundle{
		Versions: []core.AgentInputVersion{
			{LogicalKey: core.LKContainerContext, ArtifactVersionID: "version:container"},
			{LogicalKey: core.LKArchitecturePlan, ArtifactVersionID: "version:architecture"},
			{LogicalKey: core.LKEnvironmentSpec, ArtifactVersionID: "version:environment"},
		},
	}, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{LogicalKey: core.LKModuleSpecs, ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/module_specs/module_specs.json"},
			{LogicalKey: core.ModuleSpecKey("module01"), ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/module_specs/module01_spec.json"},
			{LogicalKey: core.ModuleCoderTaskKey("module01"), ObjectType: "markdown", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/modules/module01/coder_task.md"},
			{LogicalKey: core.ModuleTesterTaskKey("module01"), ObjectType: "markdown", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/modules/module01/tester_task.md"},
			{LogicalKey: core.ModuleContractKey("module01"), ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/contracts/module01_contract.json"},
			{LogicalKey: core.ModuleSeedTestsKey("module01"), ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/seed_tests/module01_seed_tests.json"},
			{LogicalKey: core.ModuleSpecKey("module02"), ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/module_specs/module02_spec.json"},
			{LogicalKey: core.ModuleCoderTaskKey("module02"), ObjectType: "markdown", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/modules/module02/coder_task.md"},
			{LogicalKey: core.ModuleTesterTaskKey("module02"), ObjectType: "markdown", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/modules/module02/tester_task.md"},
			{LogicalKey: core.ModuleContractKey("module02"), ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/contracts/module02_contract.json"},
			{LogicalKey: core.ModuleSeedTestsKey("module02"), ObjectType: "json", Status: "produced", ArtifactURI: "projects/run/agents/architect01/artifacts/split_module/seed_tests/module02_seed_tests.json"},
		},
	})

	if len(bags) != 3 {
		t.Fatalf("split_module produced bags len = %d, want 3", len(bags))
	}

	module01, ok := findProducedBagByIndex(bags, "front_module_input", "module01")
	if !ok {
		t.Fatalf("split_module produced bags = %+v, want module01 front_module_input", bags)
	}
	if !hasProducedBagMember(module01.Members, core.LKContainerContext, "version:container", "") {
		t.Fatalf("module01 front_module_input members = %+v, want container_context input passthrough", module01.Members)
	}
	if !hasProducedBagMember(module01.Members, core.ModuleSpecKey("module01"), "", "projects/run/agents/architect01/artifacts/split_module/module_specs/module01_spec.json") {
		t.Fatalf("module01 front_module_input members = %+v, want module01 spec output", module01.Members)
	}

	module02, ok := findProducedBagByIndex(bags, "backend_module_input", "module02")
	if !ok {
		t.Fatalf("split_module produced bags = %+v, want module02 backend_module_input", bags)
	}
	if !hasProducedBagMember(module02.Members, core.ModuleSeedTestsKey("module02"), "", "projects/run/agents/architect01/artifacts/split_module/seed_tests/module02_seed_tests.json") {
		t.Fatalf("module02 backend_module_input members = %+v, want module02 seed tests output", module02.Members)
	}

	globalBag, ok := findProducedBag(bags, "global_test_input")
	if !ok {
		t.Fatalf("split_module produced bags = %+v, want global_test_input", bags)
	}
	if !hasProducedBagMember(globalBag.Members, core.LKArchitecturePlan, "version:architecture", "") {
		t.Fatalf("global_test_input members = %+v, want architecture_plan input passthrough", globalBag.Members)
	}
	if !hasProducedBagMember(globalBag.Members, core.LKModuleSpecs, "", "projects/run/agents/architect01/artifacts/split_module/module_specs/module_specs.json") {
		t.Fatalf("global_test_input members = %+v, want module_specs output", globalBag.Members)
	}
}

func TestBuiltinSplitModuleOpDoesNotInferHappyPathBagsForFailureResult(t *testing.T) {
	t.Parallel()

	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinPluginRegistry() error = %v", err)
	}

	spec, ok := plugins.Ops().GetByID("architect.split_module")
	if !ok {
		t.Fatal("architect.split_module op_id missing")
	}

	bags := spec.ResolveProducedBags(core.Task{
		Role: "architect",
		Op:   "split_module",
		OpID: "architect.split_module",
	}, core.AgentInputBundle{
		Versions: []core.AgentInputVersion{
			{LogicalKey: core.LKContainerContext, ArtifactVersionID: "version:container"},
			{LogicalKey: core.LKArchitecturePlan, ArtifactVersionID: "version:architecture"},
		},
	}, core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{{Code: "invalid_semantic_split", Message: "split failed"}},
	})

	if len(bags) != 0 {
		t.Fatalf("split_module failure produced bags = %+v, want none", bags)
	}
}

func TestNewBuiltinPluginRegistryIncludesSessionCEORole(t *testing.T) {
	t.Parallel()

	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		t.Fatalf("NewBuiltinPluginRegistry() error = %v", err)
	}

	spec, ok := plugins.RoleSpec("ceo")
	if !ok {
		t.Fatal("RoleSpec(ceo) = false, want true")
	}
	if spec.InteractionMode != "session" {
		t.Fatalf("RoleSpec(ceo).InteractionMode = %q, want session", spec.InteractionMode)
	}
	if _, ok := plugins.Ops().GetByID("ceo.write_plan"); !ok {
		t.Fatal("ceo.write_plan op_id missing")
	}
	if _, ok := plugins.Ops().GetByID("ceo.review_plan"); !ok {
		t.Fatal("ceo.review_plan op_id missing")
	}
}

func TestBuiltinRoleSpecsDeclareExecutionDriverAndInteractionMode(t *testing.T) {
	for _, reg := range builtinRoleRegistrations() {
		spec := reg.Spec
		roleID := spec.ID
		if spec.ExecutionDriver == "" {
			t.Fatalf("RoleSpec(%s).ExecutionDriver is empty", roleID)
		}
		if spec.DriverRef == "" {
			t.Fatalf("RoleSpec(%s).DriverRef is empty", roleID)
		}
		if spec.InteractionMode == "" {
			t.Fatalf("RoleSpec(%s).InteractionMode is empty", roleID)
		}
	}
}

func TestNewPluginRegistryWithOptionsLoadsExternalPluginRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoleOnlyPluginPack(t, root, "pm_override", core.RoleSpec{
		ID:              "pm",
		Name:            "pm override",
		Description:     "external override",
		ExecutionDriver: "builtin_role",
		DriverRef:       "builtin:pm",
		InteractionMode: "task",
		SupportedOps: []core.RoleOpBinding{
			{Name: "write_plan", OpID: "pm.write_plan"},
			{Name: "review_plan", OpID: "pm.review_plan"},
		},
	})

	plugins, err := NewPluginRegistryWithOptions(RuntimeOptions{PluginRoots: []string{root}})
	if err != nil {
		t.Fatalf("NewPluginRegistryWithOptions() error = %v", err)
	}

	spec, ok := plugins.RoleSpec("pm")
	if !ok {
		t.Fatal("RoleSpec(pm) = false, want true")
	}
	if spec.Description != "external override" {
		t.Fatalf("RoleSpec(pm).Description = %q, want %q", spec.Description, "external override")
	}
}

func TestPluginManagerLoadsBuiltinPluginPack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := WriteBuiltinPluginPack(root); err != nil {
		t.Fatalf("WriteBuiltinPluginPack() error = %v", err)
	}
	plugins, err := NewPluginManager(NewDefaultDriverResolver()).LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if _, ok := plugins.Handlers().Get("artifact_read"); !ok {
		t.Fatal("artifact_read handler missing after plugin load")
	}
	if _, ok := plugins.Ops().Get("architect", "split_module"); !ok {
		t.Fatal("architect.split_module op missing after plugin load")
	}
	if _, ok := plugins.Agents().Get("pm"); !ok {
		t.Fatal("pm role missing after plugin load")
	}
}

func TestPluginManagerDispatchesSubprocessHandlerDriver(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginPack(t, root, "external_pack")
	writeHandlerManifest(t, root, "external_pack", "echo_handler", map[string]any{
		"handler_id":       "echo_handler",
		"execution_driver": "subprocess",
		"impl_ref":         testSubprocessPath(t, "handler"),
	})

	plugins, err := NewPluginManager(NewDefaultDriverResolver()).LoadDir(filepath.Join(root, "external_pack"))
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if _, ok := plugins.Handlers().Get("echo_handler"); !ok {
		t.Fatal("echo_handler not registered")
	}
}

func TestPluginManagerDispatchesRPCHandlerDriver(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginPack(t, root, "rpc_pack")
	writeHandlerManifest(t, root, "rpc_pack", "echo_handler", map[string]any{
		"handler_id":       "echo_handler",
		"execution_driver": "rpc",
		"impl_ref":         "http://127.0.0.1:1",
	})

	plugins, err := NewPluginManager(NewDefaultDriverResolver()).LoadDir(filepath.Join(root, "rpc_pack"))
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	handler, ok := plugins.Handlers().Get("echo_handler")
	if !ok {
		t.Fatal("echo_handler not registered")
	}
	if _, ok := handler.(*RPCHandler); !ok {
		t.Fatalf("handler type = %T, want *RPCHandler", handler)
	}
}

func TestPluginManagerDispatchesRPCRoleDriver(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginPack(t, root, "rpc_pack")
	writeRoleManifest(t, root, "rpc_pack", "remote_pm", map[string]any{
		"role_id":          "remote_pm",
		"execution_driver": "rpc",
		"driver_ref":       "http://127.0.0.1:1",
	})

	plugins, err := NewPluginManager(NewDefaultDriverResolver()).LoadDir(filepath.Join(root, "rpc_pack"))
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	agent, ok := plugins.Agents().Get("remote_pm")
	if !ok {
		t.Fatal("remote_pm not registered")
	}
	if _, ok := agent.(*RPCRoleAgent); !ok {
		t.Fatalf("agent type = %T, want *RPCRoleAgent", agent)
	}
}

func TestMutablePipelineCatalogReplaceKeepsNonLegacyDefinitionsWhileCompilingRunnableSubset(t *testing.T) {
	t.Parallel()

	spec, err := pipeline.LoadRegistrySpec(fullDeliveryRegistryPathForTest())
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	defs, err := pipeline.NewJSONRegistry(spec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	entryDef, ok := spec.Pipeline(spec.EntryPipelineID)
	if !ok {
		t.Fatalf("entry pipeline %q missing from spec", spec.EntryPipelineID)
	}
	entryLegacy, err := pipeline.CompileLegacyPipelineDefPrefix(entryDef)
	if err != nil {
		t.Fatalf("CompileLegacyPipelineDefPrefix(entry) error = %v", err)
	}
	catalog := NewMutablePipelineCatalog(pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne(), entryLegacy), defs)

	uploaded := pipeline.PipelineDefSpec{
		SchemaVersion: pipeline.PipelineSchemaVersionV04,
		PipelineID:    "workspace_preview_pipeline",
		Name:          "Workspace Preview Pipeline",
		Namespace: pipeline.NamespaceSpec{
			Agents: []pipeline.SignatureAgentSpec{{Name: "pm", Role: "pm"}},
		},
		Signature: pipeline.SignatureSpec{
			Agents: []pipeline.SignatureAgentSpec{{Name: "pm", Role: "pm"}},
		},
		StartState:    "draft_ready",
		DeliveryState: "preview_ready",
		States: []pipeline.StateSpec{
			{ID: "draft_ready", Next: &pipeline.NextSpec{Type: "all", Transitions: []string{"pm_write_plan"}}},
			{ID: "preview_ready"},
		},
		Transitions: []pipeline.TransitionSpec{
			{
				ID:        "pm_write_plan",
				Kind:      "task",
				FromState: "draft_ready",
				ToState:   "preview_ready",
				Agent:     &pipeline.AgentSpec{Role: "pm", Alias: "pm"},
				Op:        "write_plan",
			},
		},
	}
	spec.PipelineDefs = append(spec.PipelineDefs, uploaded)
	if err := catalog.Replace(spec); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	if _, err := catalog.GetDef(context.Background(), "pipeline_module"); err != nil {
		t.Fatalf("GetDef(pipeline_module) error = %v, want preserved non-legacy definition", err)
	}
	if _, err := catalog.Get(context.Background(), "workspace_preview_pipeline"); err != nil {
		t.Fatalf("Get(workspace_preview_pipeline) error = %v, want compiled runnable pipeline", err)
	}
	if _, err := catalog.Get(context.Background(), spec.EntryPipelineID); err != nil {
		t.Fatalf("Get(entry pipeline) error = %v", err)
	}
}

func TestGenericRoleDriverRunsToolLoop(t *testing.T) {
	t.Parallel()

	agent := NewGenericRoleAgent(core.RoleSpec{
		ID:              "generic_pm",
		ExecutionDriver: "generic_llm",
	})
	called := false
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "generic_pm", Op: "write_plan"},
		ToolLoop: genericRoleStubToolLoop{runFunc: func(context.Context, core.ToolLoopRequest) (core.AgentResult, error) {
			called = true
			return core.AgentResult{Result: "kok"}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !called || result.Result != "kok" {
		t.Fatalf("called/result = %v/%q, want true/kok", called, result.Result)
	}
}

type genericRoleStubToolLoop struct {
	runFunc func(context.Context, core.ToolLoopRequest) (core.AgentResult, error)
}

func (s genericRoleStubToolLoop) Run(ctx context.Context, req core.ToolLoopRequest) (core.AgentResult, error) {
	return s.runFunc(ctx, req)
}

func TestDefaultRuntimeSupportsFrontDebugWriteCodeReuse(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()

	bundle := core.AgentInputBundle{
		InputDir:  inputDir,
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKContainerContext, Path: writeTestFile(t, inputDir, "container_context.json", `{"container_id":"c1"}`)},
			{LogicalKey: core.LKModuleSpec, Path: writeTestFile(t, inputDir, "module_spec.json", `{"module_id":"module01","module_name":"frontend","module_role":"frontend","implementation_role":"front","branch_name":"feature/module01-frontend","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`)},
			{LogicalKey: core.LKCoderTask, Path: writeTestFile(t, inputDir, "coder_task.md", "# Front debug task")},
			{LogicalKey: core.LKModuleContract, Path: writeTestFile(t, inputDir, "module_contract.md", "# Contract")},
			{LogicalKey: core.LKSeedTests, Path: writeTestFile(t, inputDir, "seed_tests.json", `{"files":[]}`)},
			{LogicalKey: core.LKUpstreamArtifactIssue, Path: writeTestFile(t, inputDir, "upstream_artifact_issue.md", "# Issue")},
		},
		PreviousOutputs: []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKCoderBranch,
				ArtifactVersionID: "ver-front-debug-branch",
				Path:              filepath.Join(outputDir, "coder_branch.json"),
				ObjectType:        "json",
				Description:       "reused debug branch output",
			},
		},
	}
	bundle = withModuleInputBag(bundle)

	runtime := NewDefaultRuntime()
	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "coder",
		Op:            "debug_write_code",
		ExecutionMode: "reuse",
	}, bundle)
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("RunAgent() result = %q, want %q; errors = %+v", result.Result, "kok", result.Errors)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKCoderBranch {
		t.Fatalf("RunAgent() outputs = %+v, want reused coder_branch", result.Outputs)
	}
}

func writeRoleOnlyPluginPack(t *testing.T, root string, packName string, spec core.RoleSpec) {
	t.Helper()

	packDir := filepath.Join(root, packName)
	if err := os.MkdirAll(filepath.Join(packDir, "roles", spec.ID), 0o755); err != nil {
		t.Fatalf("mkdir plugin pack: %v", err)
	}
	if err := writeJSON(filepath.Join(packDir, "plugin.json"), PluginPackManifest{
		Name:        packName,
		Version:     "v1",
		Description: "test plugin pack",
	}); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := writeJSON(filepath.Join(packDir, "roles", spec.ID, "role.json"), spec); err != nil {
		t.Fatalf("write role manifest: %v", err)
	}
}

func writePluginPack(t *testing.T, root string, packName string) {
	t.Helper()

	packDir := filepath.Join(root, packName)
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin pack: %v", err)
	}
	if err := writeJSON(filepath.Join(packDir, "plugin.json"), PluginPackManifest{
		Name:        packName,
		Version:     "v1",
		Description: "test plugin pack",
	}); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
}

func writeHandlerManifest(t *testing.T, root string, packName string, handlerID string, manifest map[string]any) {
	t.Helper()

	dir := filepath.Join(root, packName, "handlers", handlerID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir handler manifest dir: %v", err)
	}
	writeRawJSON(t, filepath.Join(dir, "handler.json"), manifest)
}

func writeRoleManifest(t *testing.T, root string, packName string, roleID string, manifest map[string]any) {
	t.Helper()

	dir := filepath.Join(root, packName, "roles", roleID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir role manifest dir: %v", err)
	}
	writeRawJSON(t, filepath.Join(dir, "role.json"), manifest)
}

func writeRawJSON(t *testing.T, path string, value any) {
	t.Helper()

	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func testSubprocessPath(t *testing.T, mode string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), mode+".exe")
}

func withModuleInputBag(bundle core.AgentInputBundle) core.AgentInputBundle {
	versionIDs := make([]string, 0, len(bundle.Inputs))
	for i := range bundle.Inputs {
		if bundle.Inputs[i].ArtifactVersionID == "" {
			bundle.Inputs[i].ArtifactVersionID = "av_" + bundle.Inputs[i].LogicalKey
		}
		versionIDs = append(versionIDs, bundle.Inputs[i].ArtifactVersionID)
	}
	bundle.Bags = append(bundle.Bags, core.AgentInputBag{
		Name:               "module_input",
		BagID:              "bag_module_input",
		ArtifactVersionIDs: versionIDs,
	})
	return bundle
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func findProducedBag(items []appcore.ProducedBagManifest, name string) (appcore.ProducedBagManifest, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return appcore.ProducedBagManifest{}, false
}

func findProducedBagByIndex(items []appcore.ProducedBagManifest, name string, moduleKey string) (appcore.ProducedBagManifest, bool) {
	for _, item := range items {
		if item.Name == name && item.Indexes["module_key"] == moduleKey {
			return item, true
		}
	}
	return appcore.ProducedBagManifest{}, false
}

func hasProducedBagMember(items []appcore.ProducedBagMember, logicalKey string, artifactVersionID string, outputRef string) bool {
	for _, item := range items {
		if item.LogicalKey != logicalKey {
			continue
		}
		if artifactVersionID != "" && item.ArtifactVersionID != artifactVersionID {
			continue
		}
		if outputRef != "" && item.OutputRef != outputRef {
			continue
		}
		return true
	}
	return false
}

func fullDeliveryRegistryPathForTest() string {
	return filepath.Join("..", "..", "orchestrator", "testdata", "full_delivery", "pipeline_full_delivery.spec.json")
}
