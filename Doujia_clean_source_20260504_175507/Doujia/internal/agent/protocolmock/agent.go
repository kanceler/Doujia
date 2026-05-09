package protocolmock

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/runtime"
)

type Agent struct {
	agentID       core.AgentID
	runID         core.RunID
	artifactStore artifact.Store
	committer     runtime.AgentOutputCommitter
}

func NewFactory() runtime.Agent {
	return &Agent{}
}

func (a *Agent) Create(init runtime.AgentInit, deps runtime.AgentDeps) runtime.Agent {
	return &Agent{
		agentID:       init.AgentID,
		runID:         init.RunID,
		artifactStore: deps.ArtifactStore,
		committer:     deps.OutputCommitter,
	}
}

func (a *Agent) Execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if task.Direction != core.TaskDirectionDispatch {
		return core.TaskMetaData{}, fmt.Errorf("protocolmock only executes dispatch tasks")
	}
	if strings.TrimSpace(string(a.agentID)) == "" {
		return core.TaskMetaData{}, fmt.Errorf("protocolmock agent id is required")
	}
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("protocolmock artifact store is required")
	}
	feedback, err := a.execute(ctx, task)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return runtime.CommitAgentFeedbackOutputs(ctx, a.committer, a.runID, a.agentID, task, feedback)
}

func (a *Agent) execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	switch task.Op {
	case "ceo_write_requirement":
		return a.writeSingleOutput(ctx, task, "requirement", artifactPath(a.runID, a.agentID, "requirement", "requirement_v1.md"), "protocolmock requirement")
	case "pm_write_plan":
		return a.writeSingleOutput(ctx, task, "product_plan", artifactPath(a.runID, a.agentID, "plan", "plan_v1.md"), "protocolmock product plan")
	case "architecture_generation":
		return a.writeSingleOutput(ctx, task, "architecture", artifactPath(a.runID, a.agentID, "architecture", "architecture_v1.md"), "protocolmock architecture")
	case "ceo_review_plan", "pm_review_design", "ceo_user_confirm":
		return a.feedback(task, core.TaskResultCodeOK, append([]string(nil), task.ArtifactURIs...)), nil
	case core.TaskOpWritePlan:
		return a.writeSingleOutput(ctx, task, outputKeyForWritePlan(a.agentID), writePlanArtifactPath(a.runID, a.agentID), "protocolmock plan")
	case core.TaskOpReviewPlan:
		return a.feedback(task, core.TaskResultCodeOK, append([]string(nil), task.ArtifactURIs...)), nil
	case core.TaskOpCreateContainer:
		return a.writeSingleOutput(ctx, task, "container_context", artifactPath(a.runID, a.agentID, "container", "container_context.json"), `{"container_id":"protocolmock","repo_dir":"/workspace/repo","worktrees_dir":"/workspace/worktrees","test_runs_dir":"/workspace/test-runs","base_branch":"main","branch_prefix":"feature/","default_test_command":"echo protocolmock"}`+"\n")
	case core.TaskOpSplitModule:
		return a.splitModule(ctx, task)
	case core.TaskOpWriteCode:
		name := string(a.agentID) + "_code_v1.md"
		if strings.Contains(strings.ToLower(string(task.TaskID)), "force_bug_once") {
			name = string(a.agentID) + "_protocolmock_force_bug_code_v1.md"
		}
		return a.writeSingleOutput(ctx, task, "code_bag", artifactPath(a.runID, a.agentID, "code", name), "protocolmock code")
	case core.TaskOpTestData:
		if strings.Contains(string(task.TaskID), "global") || a.agentID == "architect01" {
			return a.writeSingleOutput(ctx, task, "global_test_data", artifactPath(a.runID, a.agentID, "test_data", "architect_test_data.md"), "protocolmock global test data")
		}
		return a.writeSingleOutput(ctx, task, "test_data_bag", artifactPath(a.runID, a.agentID, "test", string(a.agentID)+"_test_data_v1.md"), "protocolmock module test data")
	case core.TaskOpTestCode:
		if shouldReturnBug(task) {
			return a.writeSingleOutputWithResult(ctx, task, core.TaskResultCodeBug, "failure_report", artifactPath(a.runID, a.agentID, "test_reports", moduleKeyFromTask(task)+"_failure_report_v1.md"), "protocolmock failure report")
		}
		if strings.Contains(string(task.TaskID), "global") || a.agentID == "architect01" {
			return a.writeSingleOutput(ctx, task, "global_test_report", artifactPath(a.runID, a.agentID, "test", "global_test_report_v1.md"), "protocolmock global test report")
		}
		return a.writeSingleOutput(ctx, task, "tested_module", artifactPath(a.runID, a.agentID, "test", string(a.agentID)+"_test_code_v1.md"), "protocolmock tested module")
	case "preview_edit":
		return a.writeSingleOutput(ctx, task, "preview_edit_bag", artifactPath(a.runID, a.agentID, "preview_edit", "preview_edit.json"), `{"kind":"front_preview_edit","approved":true}`+"\n")
	case "user_preview_confirm":
		return a.feedback(task, core.TaskResultCodeOK, append([]string(nil), task.ArtifactURIs...)), nil
	case "debug_global_code":
		return a.writeSingleOutput(ctx, task, "global_test_code_input", artifactPath(a.runID, a.agentID, "debug", "global_test_code_input_v2.md"), "protocolmock debug code")
	case core.TaskOpDebug, "debug_write_code":
		return a.writeSingleOutput(ctx, task, "code_bag", artifactPath(a.runID, a.agentID, "debug", moduleKeyFromTask(task)+"_code_debugged_v2.md"), "protocolmock debug code")
	case core.TaskOpMergeCode:
		return a.writeSingleOutput(ctx, task, "merged_code", artifactPath(a.runID, a.agentID, "code", "merged_code_v1.md"), "protocolmock merged code")
	default:
		return a.writeSingleOutput(ctx, task, "default", artifactPath(a.runID, a.agentID, "outputs", string(task.TaskID)+".md"), "protocolmock output")
	}
}

func (a *Agent) splitModule(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	frontRef := artifactPath(a.runID, a.agentID, "modules", "front_module_input.md")
	backendRef := artifactPath(a.runID, a.agentID, "modules", "backend_module_input.md")
	globalRef := artifactPath(a.runID, a.agentID, "test_data", "global_test_input.md")
	if err := a.artifactStore.Write(ctx, frontRef, []byte("protocolmock front module input")); err != nil {
		return core.TaskMetaData{}, err
	}
	if err := a.artifactStore.Write(ctx, backendRef, []byte("protocolmock backend module input")); err != nil {
		return core.TaskMetaData{}, err
	}
	if err := a.artifactStore.Write(ctx, globalRef, []byte("protocolmock global test input")); err != nil {
		return core.TaskMetaData{}, err
	}
	feedback := a.feedback(task, core.TaskResultCodeOK, []string{frontRef, backendRef, globalRef})
	feedback.Outputs = []core.AgentOutput{
		producedOutput(agentcore.ModuleSpecKey("module01"), "markdown", frontRef),
		producedOutput(agentcore.ModuleSpecKey("module02"), "markdown", backendRef),
		producedOutput(agentcore.LKModuleSpecs, "markdown", globalRef),
	}
	feedback.ProducedBags = []core.ProducedBagManifest{
		{
			Name:    "front_module_input",
			Indexes: map[string]string{"module_key": "front"},
			Members: []core.ProducedBagMember{
				{LogicalKey: agentcore.ModuleSpecKey("module01")},
			},
		},
		{
			Name:    "backend_module_input",
			Indexes: map[string]string{"module_key": "module02"},
			Members: []core.ProducedBagMember{
				{LogicalKey: agentcore.ModuleSpecKey("module02")},
			},
		},
		{
			Name: "global_test_input",
			Members: []core.ProducedBagMember{
				{LogicalKey: agentcore.LKModuleSpecs},
			},
		},
	}
	feedback.Control = []core.Control{
		{
			Type:         core.ControlTypeStartPipeline,
			TransitionID: "run_front_module",
			PipelineID:   "pipeline_front_module",
			InstanceKey:  "module01",
			Params:       map[string]string{"module_key": "front"},
			AgentBindings: map[string]core.AgentID{
				"front":  "front01",
				"tester": "tester01",
			},
			InputBags: map[string]string{"module_input": "front_module_input"},
		},
		{
			Type:         core.ControlTypeStartPipeline,
			TransitionID: "run_backend_module_group",
			PipelineID:   "pipeline_backend_module_group",
			InstanceKey:  "backend",
			AgentBindings: map[string]core.AgentID{
				"coder":  "coder01",
				"tester": "tester01",
			},
			InputBags: map[string]string{"module_input": "backend_module_input"},
		},
		{
			Type:         core.ControlTypeStartPipeline,
			TransitionID: "run_global_test_data",
			PipelineID:   "pipeline_global_test_data",
			InstanceKey:  "global",
			InputBags:    map[string]string{"global_test_input": "global_test_input"},
		},
	}
	return feedback, nil
}

func (a *Agent) writeSingleOutput(ctx context.Context, task core.TaskMetaData, outputKey string, outputRef string, content string) (core.TaskMetaData, error) {
	return a.writeSingleOutputWithResult(ctx, task, core.TaskResultCodeOK, outputKey, outputRef, content)
}

func (a *Agent) writeSingleOutputWithResult(ctx context.Context, task core.TaskMetaData, result core.TaskResultCode, outputKey string, outputRef string, content string) (core.TaskMetaData, error) {
	if err := a.artifactStore.Write(ctx, outputRef, []byte(content)); err != nil {
		return core.TaskMetaData{}, err
	}
	feedback := a.feedback(task, result, []string{outputRef})
	logicalKey := logicalKeyForOutputBag(outputKey)
	feedback.Outputs = []core.AgentOutput{producedOutput(logicalKey, objectTypeForOutputRef(outputRef), outputRef)}
	feedback.ProducedBags = []core.ProducedBagManifest{
		{Name: outputKey, Members: []core.ProducedBagMember{{LogicalKey: logicalKey}}},
	}
	return feedback, nil
}

func producedOutput(logicalKey string, objectType string, outputRef string) core.AgentOutput {
	return core.AgentOutput{
		LogicalKey:  logicalKey,
		ObjectType:  objectType,
		ContentType: artifactContentType(objectType, "", outputRef),
		Encoding:    "utf-8",
		Status:      "produced",
		ArtifactURI: outputRef,
	}
}

func artifactContentType(objectType, declared, fileName string) string {
	declared = strings.TrimSpace(declared)
	if declared != "" {
		return declared
	}
	switch strings.TrimSpace(strings.ToLower(objectType)) {
	case "markdown":
		return "text/markdown; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "html":
		return "text/html; charset=utf-8"
	case "text":
		return "text/plain; charset=utf-8"
	case "binary":
		return "application/octet-stream"
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func logicalKeyForOutputBag(outputKey string) string {
	switch outputKey {
	case "requirement":
		return agentcore.LKRequirement
	case "product_plan":
		return agentcore.LKPMPlan
	case "architecture":
		return agentcore.LKArchitecturePlan
	case "container_context":
		return agentcore.LKContainerContext
	case "code_bag":
		return agentcore.LKCoderBranch
	case "test_data_bag":
		return agentcore.LKFullTestFiles
	case "tested_module", "failure_report":
		return agentcore.LKModuleTestReport
	case "merged_code":
		return agentcore.LKMergeCodeReport
	case "global_test_data":
		return agentcore.LKGlobalTestData
	case "global_test_report":
		return agentcore.LKGlobalTestReport
	case "global_test_code_input":
		return agentcore.LKMergedCodeSummary
	case "preview_edit_bag":
		return agentcore.LKPreviewEdit
	default:
		return outputKey
	}
}

func objectTypeForOutputRef(outputRef string) string {
	if strings.HasSuffix(strings.ToLower(outputRef), ".json") {
		return "json"
	}
	return "markdown"
}

func (a *Agent) feedback(task core.TaskMetaData, result core.TaskResultCode, refs []string) core.TaskMetaData {
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOnIDs: append([]core.TaskID(nil), task.DependsOnIDs...),
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: append([]string(nil), refs...),
		InputBagIDs:  append([]string(nil), task.InputBagIDs...),
		Result:       result,
	}
}

func outputKeyForWritePlan(agentID core.AgentID) string {
	switch agentID {
	case "ceo":
		return "requirement"
	case "pm01":
		return "product_plan"
	default:
		return "architecture"
	}
}

func writePlanArtifactPath(runID core.RunID, agentID core.AgentID) string {
	switch agentID {
	case "ceo":
		return artifactPath(runID, agentID, "requirement", "requirement_v1.md")
	case "pm01":
		return artifactPath(runID, agentID, "plan", "plan_v1.md")
	default:
		return artifactPath(runID, agentID, "architecture", "architecture_v1.md")
	}
}

func artifactPath(runID core.RunID, agentID core.AgentID, kind string, name string) string {
	return path.Join("projects", string(runID), "agents", string(agentID), "artifacts", kind, name)
}

func moduleKeyFromTask(task core.TaskMetaData) string {
	id := string(task.TaskID)
	for _, module := range []string{"module01", "module02"} {
		if strings.Contains(id, module) {
			return module
		}
	}
	return "module"
}

func shouldReturnBug(task core.TaskMetaData) bool {
	if strings.Contains(strings.ToLower(string(task.TaskID)), "force_bug") {
		return true
	}
	if task.InputBundle == nil {
		return false
	}
	raw, _ := json.Marshal(task.InputBundle)
	return strings.Contains(strings.ToLower(string(raw)), "protocolmock_force_bug")
}
