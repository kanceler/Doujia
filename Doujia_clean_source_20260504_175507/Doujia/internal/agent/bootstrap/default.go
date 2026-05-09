package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/agent/executor"
	"devflow/internal/agent/handler"
	"devflow/internal/agent/llm"
	"devflow/internal/agent/registry"
	rolearchitect "devflow/internal/agent/role/architect"
	roleceo "devflow/internal/agent/role/ceo"
	rolecoder "devflow/internal/agent/role/coder"
	rolefront "devflow/internal/agent/role/front"
	rolepm "devflow/internal/agent/role/pm"
	roletester "devflow/internal/agent/role/tester"
	architectspec "devflow/internal/agent/spec/architect"
	ceospec "devflow/internal/agent/spec/ceo"
	coderspec "devflow/internal/agent/spec/coder"
	frontspec "devflow/internal/agent/spec/front"
	pmspec "devflow/internal/agent/spec/pm"
	testerspec "devflow/internal/agent/spec/tester"
)

type RuntimeOptions struct {
	PluginRoots []string
	LLMConfig   llm.OpenAIAdapterConfig
	LLMTracef   func(format string, args ...any)
}

func NewDefaultRuntime() *executor.Runtime {
	runtime, err := NewRuntimeWithOptions(RuntimeOptions{})
	if err != nil {
		panic(err)
	}
	return runtime
}

func NewRuntimeWithOptions(options RuntimeOptions) (*executor.Runtime, error) {
	plugins, err := NewPluginRegistryWithOptions(options)
	if err != nil {
		return nil, err
	}
	adapter := llm.NewOpenAIAdapterFromEnv()
	adapter.ApplyConfig(options.LLMConfig)
	if options.LLMTracef != nil {
		adapter.Tracef = options.LLMTracef
	}
	return executor.NewRuntime(plugins.Agents(), plugins.Ops(), plugins.Handlers()).
		WithRoleRegistry(plugins).
		WithLLM(adapter).
		WithToolLoop(llm.NewToolLoopFromEnv(adapter)), nil
}

func NewPluginRegistryWithOptions(options RuntimeOptions) (*registry.PluginRegistry, error) {
	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		return nil, err
	}
	options = NormalizeRuntimeOptions(options)
	if len(options.PluginRoots) == 0 {
		return plugins, nil
	}
	manager := NewPluginManager(NewDefaultDriverResolver())
	for _, root := range options.PluginRoots {
		if err := manager.LoadInto(plugins, root); err != nil {
			return nil, err
		}
	}
	return plugins, nil
}

func NormalizeRuntimeOptions(options RuntimeOptions) RuntimeOptions {
	out := RuntimeOptions{
		PluginRoots: make([]string, 0, len(options.PluginRoots)),
		LLMConfig:   options.LLMConfig,
		LLMTracef:   options.LLMTracef,
	}
	for _, root := range options.PluginRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		out.PluginRoots = append(out.PluginRoots, filepath.Clean(root))
	}
	return out
}

func NewBuiltinPluginRegistry() (*registry.PluginRegistry, error) {
	return loadBuiltinPluginRegistry()
}

func builtinHandlerRegistrations() []agentcore.HandlerRegistration {
	return []agentcore.HandlerRegistration{
		{Spec: agentcore.HandlerSpec{ID: "artifact_read", ExecutionDriver: "builtin", ImplRef: "builtin:artifact_read"}, Handler: handler.NewArtifactReadHandler()},
		{Spec: agentcore.HandlerSpec{ID: "artifact_write", ExecutionDriver: "builtin", ImplRef: "builtin:artifact_write"}, Handler: handler.NewArtifactWriteHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_create", ExecutionDriver: "builtin", ImplRef: "builtin:container_create"}, Handler: handler.NewContainerCreateHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_read", ExecutionDriver: "builtin", ImplRef: "builtin:container_read"}, Handler: handler.NewContainerReadHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_write", ExecutionDriver: "builtin", ImplRef: "builtin:container_write"}, Handler: handler.NewContainerWriteHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_run", ExecutionDriver: "builtin", ImplRef: "builtin:container_run"}, Handler: handler.NewContainerRunHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_exec", ExecutionDriver: "builtin", ImplRef: "builtin:container_exec"}, Handler: handler.NewContainerExecHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_git_commit", ExecutionDriver: "builtin", ImplRef: "builtin:container_git_commit"}, Handler: handler.NewContainerGitCommitHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_git_cherry_pick", ExecutionDriver: "builtin", ImplRef: "builtin:container_git_cherry_pick"}, Handler: handler.NewContainerGitCherryPickHandler()},
		{Spec: agentcore.HandlerSpec{ID: "container_git_worktree_prepare", ExecutionDriver: "builtin", ImplRef: "builtin:container_git_worktree_prepare"}, Handler: handler.NewContainerGitWorktreePrepareHandler()},
		{Spec: agentcore.HandlerSpec{ID: "task_complete", ExecutionDriver: "builtin", ImplRef: "builtin:task_complete"}, Handler: handler.NewTaskCompleteHandler()},
	}
}

func builtinOpRegistrations() []agentcore.OpRegistration {
	specs := []struct {
		id   string
		spec agentcore.OpSpec
	}{
		{id: "architect.write_plan", spec: architectspec.WritePlanSpec()},
		{id: "architect.create_container", spec: architectspec.CreateContainerSpec()},
		{id: "architect.split_module", spec: architectspec.SplitModuleSpec()},
		{id: "architect.merge_code", spec: architectspec.MergeCodeSpec()},
		{id: "architect.test_data", spec: architectspec.TestDataSpec()},
		{id: "architect.test_code", spec: architectspec.TestCodeSpec()},
		{id: "ceo.write_plan", spec: ceospec.WritePlanSpec()},
		{id: "ceo.review_plan", spec: ceospec.ReviewPlanSpec()},
		{id: "coder.write_code", spec: coderspec.WriteCodeSpec()},
		{id: "coder.debug_write_code", spec: coderspec.DebugWriteCodeSpec()},
		{id: "front.write_code", spec: frontspec.WriteCodeSpec()},
		{id: "front.debug_write_code", spec: frontspec.DebugWriteCodeSpec()},
		{id: "front.preview_edit", spec: frontspec.PreviewEditSpec()},
		{id: "front.user_preview_confirm", spec: frontspec.UserPreviewConfirmSpec()},
		{id: "pm.write_plan", spec: pmspec.WritePlanSpec()},
		{id: "pm.review_plan", spec: pmspec.ReviewPlanSpec()},
		{id: "tester.test_data", spec: testerspec.TestDataSpec()},
		{id: "tester.test_code", spec: testerspec.TestCodeSpec()},
	}
	out := make([]agentcore.OpRegistration, 0, len(specs))
	for _, item := range specs {
		out = append(out, agentcore.OpRegistration{
			ID:   item.id,
			Spec: item.spec,
		})
	}
	return out
}

func builtinRoleRegistrations() []agentcore.RoleRegistration {
	return []agentcore.RoleRegistration{
		{
			Spec: agentcore.RoleSpec{
				ID:              "architect",
				ExecutionDriver: "builtin_role",
				DriverRef:       "builtin:architect",
				InteractionMode: "task",
				SupportedOps: []agentcore.RoleOpBinding{
					{Name: "write_plan", OpID: "architect.write_plan"},
					{Name: "create_container", OpID: "architect.create_container"},
					{Name: "split_module", OpID: "architect.split_module"},
					{Name: "merge_code", OpID: "architect.merge_code"},
					{Name: "test_data", OpID: "architect.test_data"},
					{Name: "test_code", OpID: "architect.test_code"},
				},
			},
			Agent: rolearchitect.NewAgent(),
		},
		{
			Spec: agentcore.RoleSpec{
				ID:              "ceo",
				ExecutionDriver: "builtin_role",
				DriverRef:       "builtin:ceo",
				InteractionMode: "session",
				SupportedOps: []agentcore.RoleOpBinding{
					{Name: "write_plan", OpID: "ceo.write_plan"},
					{Name: "review_plan", OpID: "ceo.review_plan"},
				},
			},
			Agent: roleceo.NewAgent(),
		},
		{
			Spec: agentcore.RoleSpec{
				ID:              "coder",
				ExecutionDriver: "builtin_role",
				DriverRef:       "builtin:coder",
				InteractionMode: "task",
				SupportedOps: []agentcore.RoleOpBinding{
					{Name: "write_code", OpID: "coder.write_code"},
					{Name: "debug_write_code", OpID: "coder.debug_write_code"},
				},
			},
			Agent: rolecoder.NewAgent(),
		},
		{
			Spec: agentcore.RoleSpec{
				ID:              "front",
				ExecutionDriver: "builtin_role",
				DriverRef:       "builtin:front",
				InteractionMode: "task",
				SupportedOps: []agentcore.RoleOpBinding{
					{Name: "write_code", OpID: "front.write_code"},
					{Name: "debug_write_code", OpID: "front.debug_write_code"},
				},
			},
			Agent: rolefront.NewAgent(),
		},
		{
			Spec: agentcore.RoleSpec{
				ID:              "pm",
				ExecutionDriver: "builtin_role",
				DriverRef:       "builtin:pm",
				InteractionMode: "task",
				SupportedOps: []agentcore.RoleOpBinding{
					{Name: "write_plan", OpID: "pm.write_plan"},
					{Name: "review_plan", OpID: "pm.review_plan"},
				},
			},
			Agent: rolepm.NewAgent(),
		},
		{
			Spec: agentcore.RoleSpec{
				ID:              "tester",
				ExecutionDriver: "builtin_role",
				DriverRef:       "builtin:tester",
				InteractionMode: "task",
				SupportedOps: []agentcore.RoleOpBinding{
					{Name: "test_data", OpID: "tester.test_data"},
					{Name: "test_code", OpID: "tester.test_code"},
				},
			},
			Agent: roletester.NewAgent(),
		},
	}
}

func MustBuiltinPluginRegistry() *registry.PluginRegistry {
	plugins, err := NewBuiltinPluginRegistry()
	if err != nil {
		panic(fmt.Sprintf("build builtin plugin registry: %v", err))
	}
	return plugins
}

func loadBuiltinPluginRegistry() (*registry.PluginRegistry, error) {
	root, err := os.MkdirTemp("", "doujia-builtin-plugins-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	if _, err := WriteBuiltinPluginPack(root); err != nil {
		return nil, err
	}
	return NewPluginManager(NewDefaultDriverResolver()).LoadDir(root)
}
