package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devflow/internal/app"
	"devflow/internal/core"
)

func TestSeedRequirementFeedbackWritesArtifactAndCompletesPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	projectsRoot := t.TempDir()
	registryPath := filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json")
	bootstrap, err := app.NewBootstrapWithOptions(projectsRoot, app.BootstrapOptions{
		PipelineRegistryPath: registryPath,
		AgentMode:            "protocolmock",
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}

	const runID core.RunID = "run_seed_requirement_cli"
	runConfig, err := demoRunConfig()
	if err != nil {
		t.Fatalf("demoRunConfig() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "pipeline_full_delivery", runConfig); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	runDir := filepath.Join(projectsRoot, string(runID))
	feedback, err := seedRequirementFeedback(ctx, bootstrap, runID, runDir, defaultRequirementText)
	if err != nil {
		t.Fatalf("seedRequirementFeedback() error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("OnFeedback() error = %v", err)
	}
	if err := waitForRunStatus(ctx, bootstrap, runID, core.RunStatusCompleted); err != nil {
		t.Fatalf("waitForRunStatus() error = %v", err)
	}

	path := filepath.Join(runDir, "agents", "ceo", "artifacts", "requirement", "requirement_v1.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if got, want := string(content), defaultRequirementText+"\n"; got != want {
		t.Fatalf("requirement content = %q, want %q", got, want)
	}
}

func TestDemoRunConfigAllowsLowConcurrency(t *testing.T) {
	config, err := demoRunConfig(demoRunOptions{
		MaxCoderAgents:  1,
		MaxTesterAgents: 1,
		AllowParallel:   false,
	})
	if err != nil {
		t.Fatalf("demoRunConfig() error = %v", err)
	}

	if got := config.Delivery.MaxCoderAgents; got != 1 {
		t.Fatalf("MaxCoderAgents = %d, want 1", got)
	}
	if got := config.Delivery.MaxTesterAgents; got != 1 {
		t.Fatalf("MaxTesterAgents = %d, want 1", got)
	}
	if config.Delivery.AllowParallelWork {
		t.Fatal("AllowParallelWork = true, want false")
	}
	if !config.Delivery.RequireTesterPerModule {
		t.Fatal("RequireTesterPerModule = false, want true")
	}
	if got := config.Delivery.Git.MainBranch; got != "main" {
		t.Fatalf("MainBranch = %q, want main", got)
	}
}

func TestDemoTaskWorkerCountUsesSingleWorkerForLowConcurrency(t *testing.T) {
	config, err := demoRunConfig(demoRunOptions{
		MaxCoderAgents:  1,
		MaxTesterAgents: 1,
		AllowParallel:   false,
	})
	if err != nil {
		t.Fatalf("demoRunConfig() error = %v", err)
	}

	if got := demoTaskWorkerCount(config.Delivery); got != 1 {
		t.Fatalf("demoTaskWorkerCount(low concurrency) = %d, want 1", got)
	}

	defaultConfig, err := demoRunConfig()
	if err != nil {
		t.Fatalf("demoRunConfig() error = %v", err)
	}
	if got := demoTaskWorkerCount(defaultConfig.Delivery); got != 4 {
		t.Fatalf("demoTaskWorkerCount(default) = %d, want 4", got)
	}
}

func TestValidateRealAgentEnvRequiresGatewaySettings(t *testing.T) {
	err := validateRealAgentEnv("real", func(key string) string {
		switch key {
		case "OPENAI_API_KEY":
			return "test-key"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("validateRealAgentEnv() error = nil, want missing model/base url")
	}
	if got := err.Error(); !strings.Contains(got, "OPENAI_MODEL") || !strings.Contains(got, "OPENAI_BASE_URL") || strings.Contains(got, "OPENAI_API_KEY") {
		t.Fatalf("validateRealAgentEnv() error = %v, want only missing model/base url", err)
	}
}

func TestValidateRealAgentEnvSkipsProtocolMock(t *testing.T) {
	if err := validateRealAgentEnv("protocolmock", func(string) string { return "" }); err != nil {
		t.Fatalf("validateRealAgentEnv(protocolmock) error = %v", err)
	}
}
