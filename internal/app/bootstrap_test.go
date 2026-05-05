package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devflow/internal/core"
	"devflow/internal/runtime"
)

func TestNewBootstrap(t *testing.T) {
	bootstrap := NewBootstrap(t.TempDir())
	if bootstrap.Modules.RunManager == nil {
		t.Fatalf("RunManager should not be nil")
	}
	if bootstrap.Modules.SessionRuntime == nil {
		t.Fatalf("SessionRuntime should not be nil")
	}
	if bootstrap.Modules.MessageGateway == nil {
		t.Fatalf("MessageGateway should not be nil")
	}
	if bootstrap.Modules.TaskRuntime == nil {
		t.Fatalf("TaskRuntime should not be nil")
	}
	if bootstrap.Internals.Orchestrator == nil {
		t.Fatalf("Orchestrator should not be nil")
	}
	if bootstrap.Internals.InstanceRepository == nil {
		t.Fatalf("InstanceRepository should not be nil")
	}
}

func TestNewBootstrapWithOptionsUsesTaskWorkerCount(t *testing.T) {
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		TaskWorkerCount: 1,
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}
	if got := bootstrap.Modules.TaskRuntime.WorkerCount(); got != 1 {
		t.Fatalf("TaskRuntime.WorkerCount() = %d, want 1", got)
	}
}

func TestNewSQLiteBootstrap(t *testing.T) {
	bootstrap, db, err := NewSQLiteBootstrap(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBootstrap() error = %v", err)
	}
	defer db.Close()
	if bootstrap.Modules.RunManager == nil {
		t.Fatalf("RunManager should not be nil")
	}
	if bootstrap.Internals.ArtifactRepository == nil {
		t.Fatalf("ArtifactRepository should not be nil")
	}
	if bootstrap.Internals.EventRepository == nil {
		t.Fatalf("EventRepository should not be nil")
	}
	if bootstrap.Internals.InstanceRepository == nil {
		t.Fatalf("InstanceRepository should not be nil")
	}
}

func TestNewBootstrapWithOptionsLoadsLinearJSONRegistry(t *testing.T) {
	path := writeBootstrapTestRegistry(t, linearBootstrapRegistryJSON)
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		PipelineRegistryPath: path,
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}
	if bootstrap.Internals.PipelineDefinitions == nil {
		t.Fatalf("PipelineDefinitions should not be nil for JSON registry")
	}
	spec, err := bootstrap.Internals.PipelineRegistry.Get(context.Background(), "linear_entry")
	if err != nil {
		t.Fatalf("PipelineRegistry.Get(linear_entry) error = %v", err)
	}
	if got, want := len(spec.Stages), 2; got != want {
		t.Fatalf("legacy stage count = %d, want %d", got, want)
	}
}

func TestNewBootstrapWithOptionsLoadsFullDeliveryJSONTaskPrefix(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json")
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		PipelineRegistryPath: path,
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}
	spec, err := bootstrap.Internals.PipelineRegistry.Get(context.Background(), "pipeline_full_delivery")
	if err != nil {
		t.Fatalf("PipelineRegistry.Get(pipeline_full_delivery) error = %v", err)
	}
	if got, want := len(spec.Stages), 7; got != want {
		t.Fatalf("legacy stage count = %d, want %d", got, want)
	}
	if got := spec.Stages[len(spec.Stages)-2].Op; got != "create_container" {
		t.Fatalf("penultimate legacy op = %s, want create_container", got)
	}
	if got := spec.Stages[len(spec.Stages)-1].Op; got != "split_module" {
		t.Fatalf("last legacy op = %s, want split_module", got)
	}
}

func TestNewBootstrapWithOptionsRegistersBuiltinPluginTaskRolesInRealMode(t *testing.T) {
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		AgentMode: "real",
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}

	_, err = bootstrap.Modules.TaskRuntime.EnsureAgent(context.Background(), runtime.EnsureAgentRequest{
		RunID:       "run_front",
		Role:        "front",
		AgentID:     "front01",
		ProjectRoot: t.TempDir(),
		RunConfig:   noopBootstrapRunConfig(),
	})
	if err != nil {
		t.Fatalf("EnsureAgent(front) error = %v", err)
	}
}

func TestNewBootstrapWithOptionsBuildsSessionRoleInRealMode(t *testing.T) {
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		AgentMode: "real",
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}

	run := core.PipelineRun{
		ID:         "run_session_real",
		ProjectDir: t.TempDir(),
		Config:     noopBootstrapRunConfig(),
	}
	session, err := bootstrap.Modules.SessionRuntime.CreateSession(context.Background(), run)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.Role != core.AgentRoleCEO {
		t.Fatalf("session role = %q, want ceo", session.Role)
	}
	if session.AgentID != "ceo" {
		t.Fatalf("session agent_id = %q, want ceo", session.AgentID)
	}
}

func TestProtocolmockBootstrapRegistersTaskRolesFromPluginSpecs(t *testing.T) {
	root := t.TempDir()
	writeBootstrapRolePlugin(t, root, "remote_pm", map[string]any{
		"role_id":          "remote_pm",
		"execution_driver": "rpc",
		"driver_ref":       "http://127.0.0.1:18080",
		"interaction_mode": "task",
		"supported_ops": []map[string]any{
			{"name": "write_plan", "op_id": "pm.write_plan"},
		},
	})

	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		AgentMode:        "protocolmock",
		AgentPluginRoots: []string{root},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}
	_, err = bootstrap.Modules.TaskRuntime.EnsureAgent(context.Background(), runtime.EnsureAgentRequest{
		RunID:       "run_remote_pm",
		Role:        core.AgentRole("remote_pm"),
		AgentID:     "remote_pm",
		ProjectRoot: t.TempDir(),
		RunConfig:   noopBootstrapRunConfig(),
	})
	if err != nil {
		t.Fatalf("EnsureAgent(remote_pm) error = %v", err)
	}
}

func TestRealBootstrapUsesPluginRoleSpecsForRuntimePlan(t *testing.T) {
	root := t.TempDir()
	writeBootstrapRolePlugin(t, root, "remote_pm", map[string]any{
		"role_id":          "remote_pm",
		"execution_driver": "rpc",
		"driver_ref":       "http://127.0.0.1:18080",
		"interaction_mode": "task",
		"supported_ops": []map[string]any{
			{"name": "write_plan", "op_id": "pm.write_plan"},
		},
	})

	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		AgentMode:        "real",
		AgentPluginRoots: []string{root},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions(real) error = %v", err)
	}
	_, err = bootstrap.Modules.TaskRuntime.EnsureAgent(context.Background(), runtime.EnsureAgentRequest{
		RunID:       "run_real_remote_pm",
		Role:        core.AgentRole("remote_pm"),
		AgentID:     "remote_pm",
		ProjectRoot: t.TempDir(),
		RunConfig:   noopBootstrapRunConfig(),
	})
	if err != nil {
		t.Fatalf("EnsureAgent(remote_pm) error = %v", err)
	}
}

func TestProtocolMockAgentsRunFullDeliveryJSON(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json")
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		PipelineRegistryPath: path,
		AgentMode:            "protocolmock",
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}

	const runID core.RunID = "run_protocolmock_full_delivery_json"
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "pipeline_full_delivery", noopBootstrapRunConfig()); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForBootstrapRunStatus(t, ctx, bootstrap, runID, core.RunStatusCompleted)
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %s, want completed", run.Status)
	}
	instances, err := bootstrap.Internals.InstanceRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(instances) error = %v", err)
	}
	if len(instances) < 8 {
		t.Fatalf("instances = %d, want expanded JSON pipeline instances", len(instances))
	}
	snapshots, err := bootstrap.Internals.DoujiaGitRepository.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListSnapshotsByRun() error = %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatalf("expected DoujiaGit task snapshots")
	}
	var sawSplitModule bool
	var sawRuntimeContext bool
	for _, snapshot := range snapshots {
		if snapshot.TaskID == "split_module" {
			sawSplitModule = true
		}
		if snapshot.RuntimeContextJSON != "" {
			sawRuntimeContext = true
		}
	}
	if !sawSplitModule {
		t.Fatalf("snapshots did not include split_module: %+v", snapshots)
	}
	if !sawRuntimeContext {
		t.Fatalf("expected at least one snapshot runtime context")
	}
}

func waitForBootstrapRunStatus(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID, want core.RunStatus) core.PipelineRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	t.Fatalf("run status = %s, want %s", run.Status, want)
	return core.PipelineRun{}
}

func noopBootstrapRunConfig() core.RunConfig {
	return core.RunConfig{
		Delivery: core.DeliveryConfig{
			MaxCoderAgents:         2,
			MaxTesterAgents:        2,
			RequireTesterPerModule: true,
			AllowParallelWork:      true,
			Git: core.GitRunConfig{
				MainBranch: "main",
			},
		},
	}
}

func writeBootstrapTestRegistry(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pipeline_registry.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test registry: %v", err)
	}
	return path
}

func writeBootstrapRolePlugin(t *testing.T, root string, roleID string, role map[string]any) {
	t.Helper()
	packDir := filepath.Join(root, "pack_"+roleID)
	roleDir := filepath.Join(packDir, "roles", roleID)
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONTestFile(t, filepath.Join(packDir, "plugin.json"), map[string]any{
		"name": "pack_" + roleID,
	})
	writeJSONTestFile(t, filepath.Join(roleDir, "role.json"), role)
}

func writeJSONTestFile(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

const linearBootstrapRegistryJSON = `{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "linear_registry",
  "entry_pipeline_id": "linear_entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "linear_entry",
      "kind": "entry",
      "name": "Linear Entry",
      "namespace": {
        "agents": [
          {"name": "ceo", "role": "ceo", "default_agent_id": "ceo"},
          {"name": "pm01", "role": "pm", "default_agent_id": "pm01"}
        ]
      },
      "start_state": "delivery_start",
      "delivery_state": "product_plan_ready",
      "states": [
        {
          "id": "delivery_start",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["ceo_write_requirement"]}
        },
        {
          "id": "requirement_ready",
          "kind": "state",
          "name": "CEO 交付需求包",
          "proof": {"type": "transition_result", "transition": "ceo_write_requirement"},
          "next": {"type": "all", "transitions": ["pm_write_plan"]}
        },
        {
          "id": "product_plan_ready",
          "kind": "delivery",
          "name": "PM 交付产品计划包",
          "proof": {"type": "transition_result", "transition": "pm_write_plan"}
        }
      ],
      "transitions": [
        {
          "id": "ceo_write_requirement",
          "kind": "task",
          "from_state": "delivery_start",
          "to_state": "requirement_ready",
          "agent": {"role": "ceo", "alias": "ceo"},
          "op": "write_plan",
          "output_bags": [{"name": "requirement", "required": true}]
        },
        {
          "id": "pm_write_plan",
          "kind": "task",
          "from_state": "requirement_ready",
          "to_state": "product_plan_ready",
          "agent": {"role": "pm", "alias": "pm01"},
          "op": "write_plan",
          "input_bags": [{"name": "requirement", "from_state": "requirement_ready"}],
          "output_bags": [{"name": "product_plan", "required": true}]
        }
      ]
    }
  ]
}`
