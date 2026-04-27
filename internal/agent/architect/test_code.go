package architect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
)

type architectTestCodeInputs struct {
	design     agentengine.ArtifactDocument
	mainBranch common.BranchArtifact
	mainDoc    agentengine.ArtifactDocument
}

type architectTestCodeExecutionReport struct {
	Status            string   `json:"status"`
	Summary           string   `json:"summary"`
	TestCommand       string   `json:"test_command"`
	TestPassed        bool     `json:"test_passed"`
	Fixed             bool     `json:"fixed"`
	ChangedFiles      []string `json:"changed_files,omitempty"`
	FailureSummary    string   `json:"failure_summary,omitempty"`
	TestsAdded        []string `json:"tests_added,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	SuspectedFiles    []string `json:"suspected_files,omitempty"`
	ReproductionSteps []string `json:"reproduction_steps,omitempty"`
}

func (a *Agent) executeTestCode(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	start := time.Now()
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("architect artifact store is nil")
	}
	if a.openCodeRunner == nil {
		a.openCodeRunner = common.NewManagedOpenCodeRunner("")
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return a.architectTestFailureFeedback(ctx, task, nil, architectTestCodeExecutionReport{}, fmt.Errorf("解析任务元数据失败"), start)
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return a.architectTestFailureFeedback(ctx, task, nil, architectTestCodeExecutionReport{}, fmt.Errorf("读取输入产物失败"), start)
	}
	inputs, err := findArchitectTestCodeInputs(docs)
	if err != nil {
		return a.architectTestFailureFeedback(ctx, task, nil, architectTestCodeExecutionReport{}, err, start)
	}

	repoDir := filepath.FromSlash(strings.TrimSpace(inputs.mainBranch.RepoDir))
	prompt := buildArchitectTestCodePrompt(task, inputs)
	a.logStep(fmt.Sprintf("test_code opencode request start: repo=%s prompt_chars=%d", repoDir, len(prompt)))
	if _, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: repoDir,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	}); err != nil {
		return a.architectTestFailureFeedback(ctx, task, &inputs, architectTestCodeExecutionReport{}, fmt.Errorf("OC 架构测试执行失败"), start)
	}

	report, err := readArchitectTestCodeReport(repoDir)
	if err != nil {
		return a.architectTestFailureFeedback(ctx, task, &inputs, architectTestCodeExecutionReport{}, err, start)
	}
	if strings.EqualFold(strings.TrimSpace(report.Status), "passed") && report.TestPassed {
		if report.Fixed || len(report.ChangedFiles) > 0 {
			hasChanges, err := hasUncommittedNonDevflowChanges(ctx, repoDir)
			if err != nil {
				return a.architectTestFailureFeedback(ctx, task, &inputs, report, fmt.Errorf("检查修复提交状态失败"), start)
			}
			if hasChanges {
				return a.architectTestFailureFeedback(ctx, task, &inputs, report, fmt.Errorf("OC 报告已修复并测试通过，但仓库仍存在未提交代码改动"), start)
			}
		}
		a.logStep(fmt.Sprintf("test_code success: fixed=%t elapsed_ms=%d", report.Fixed, common.DurationMillis(time.Since(start))))
		return common.FeedbackFor(task, a.runID, a.agentID, nil), nil
	}
	return a.architectTestFailureFeedback(ctx, task, &inputs, report, fmt.Errorf("OC 报告架构测试失败或未能修复"), start)
}

func findArchitectTestCodeInputs(docs []agentengine.ArtifactDocument) (architectTestCodeInputs, error) {
	var inputs architectTestCodeInputs
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.TrimSpace(doc.URI))
		lower := strings.ToLower(uri)
		switch {
		case agentengine.InferArtifactKind(uri) == "design":
			inputs.design = doc
		case strings.Contains(lower, "/artifacts/branches/"):
			kind, err := artifactKind([]byte(doc.Content))
			if err != nil {
				return inputs, fmt.Errorf("解析主分支产物失败: %s", doc.URI)
			}
			if kind != "main_branch" {
				continue
			}
			mainBranch, err := common.ParseBranchArtifact([]byte(doc.Content))
			if err != nil {
				return inputs, fmt.Errorf("解析主分支产物失败: %s", doc.URI)
			}
			inputs.mainBranch = mainBranch
			inputs.mainDoc = doc
		}
	}
	if strings.TrimSpace(inputs.design.URI) == "" {
		return inputs, fmt.Errorf("test_code 需要架构书产物 /artifacts/design/")
	}
	if strings.TrimSpace(inputs.mainDoc.URI) == "" {
		return inputs, fmt.Errorf("test_code 需要 architect split_module 输出的主分支产物 /artifacts/branches/")
	}
	return inputs, nil
}

func buildArchitectTestCodePrompt(task core.TaskMetaData, inputs architectTestCodeInputs) string {
	var builder strings.Builder
	builder.WriteString("# 架构师级代码测试与修复\n\n")
	builder.WriteString("# 角色\n\n")
	builder.WriteString("你是 DevFlow 架构师 Agent，负责根据架构书对 split_module 创建的主仓库代码进行整体验收测试，并在发现问题时按架构书自行修复。\n\n")
	builder.WriteString("# 任务\n\n")
	builder.WriteString(fmt.Sprintf("- task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("- agent_id: %s\n", task.AgentID))
	builder.WriteString(fmt.Sprintf("- op: %s\n", task.Op))
	builder.WriteString("\n# Git 仓库\n\n")
	builder.WriteString(fmt.Sprintf("- 仓库地址: %s\n", inputs.mainBranch.RepoDir))
	builder.WriteString(fmt.Sprintf("- 分支: %s\n", inputs.mainBranch.Branch))
	builder.WriteString(fmt.Sprintf("- 提交: %s\n", inputs.mainBranch.Commit))
	builder.WriteString("\n# 输入文档\n")
	writeMergePromptDoc(&builder, "架构书", inputs.design)
	writeMergePromptDoc(&builder, "主分支产物", inputs.mainDoc)
	builder.WriteString("\n# 测试与修复要求\n\n")
	builder.WriteString("1. 你当前位于 split_module 创建的主仓库工作区。\n")
	builder.WriteString("2. 请根据架构书自行设计测试，必要时补充测试代码并运行测试。\n")
	builder.WriteString("3. 测试应覆盖架构书中的核心模块、关键流程、接口契约、边界条件和错误处理。\n")
	builder.WriteString("4. 如果发现实现与架构书不一致，允许你修改产品代码进行修复。\n")
	builder.WriteString("5. 修复后必须重新运行测试，不要为了通过测试而削弱架构要求。\n")
	builder.WriteString("6. 不要删除重要功能，不要绕过测试。\n")
	builder.WriteString("7. 如果修改了产品代码或测试代码，测试通过后提交改动。\n")
	builder.WriteString("8. 无论成功或失败，都必须写入 .devflow/result.json。\n")
	builder.WriteString("\n# Mandatory Completion Contract\n\n")
	builder.WriteString("- Before finishing, create the .devflow directory if it does not exist.\n")
	builder.WriteString("- Before finishing, write .devflow/result.json exactly once with the schema below.\n")
	builder.WriteString("- This report is mandatory even when tests pass and even when no code changes are needed.\n")
	builder.WriteString("- If you cannot run tests, still write .devflow/result.json with status=\"failed\", test_passed=false, and a failure_summary.\n")
	builder.WriteString("- test_command must be executable shell syntax only; put explanations in summary or evidence.\n")
	builder.WriteString("\n# .devflow/result.json 格式\n\n")
	builder.WriteString(`{"status":"passed|failed","summary":"简短总结","test_command":"实际执行的测试命令","test_passed":true,"fixed":true,"changed_files":[],"failure_summary":"失败原因说明","tests_added":[],"evidence":[],"suspected_files":[],"reproduction_steps":[]}`)
	builder.WriteString("\n")
	return builder.String()
}

func readArchitectTestCodeReport(repoDir string) (architectTestCodeExecutionReport, error) {
	content, err := os.ReadFile(filepath.Join(repoDir, ".devflow", "result.json"))
	if err != nil {
		return architectTestCodeExecutionReport{}, fmt.Errorf("读取 .devflow/result.json 失败")
	}
	var report architectTestCodeExecutionReport
	if err := json.Unmarshal(content, &report); err != nil {
		return architectTestCodeExecutionReport{}, fmt.Errorf("解析 .devflow/result.json 失败")
	}
	if strings.TrimSpace(report.Status) == "" {
		return architectTestCodeExecutionReport{}, fmt.Errorf(".devflow/result.json status 不能为空")
	}
	return report, nil
}

func hasUncommittedNonDevflowChanges(ctx context.Context, repoDir string) (bool, error) {
	output, err := gitOutput(ctx, repoDir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		pathPart := line
		if len(line) > 3 {
			pathPart = line[3:]
		}
		pathPart = filepath.ToSlash(strings.TrimSpace(pathPart))
		if isIgnorableArchitectTestPath(pathPart) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func isIgnorableArchitectTestPath(pathPart string) bool {
	pathPart = filepath.ToSlash(strings.TrimSpace(pathPart))
	if pathPart == "" {
		return false
	}
	if strings.HasPrefix(pathPart, ".devflow/") || pathPart == ".devflow" {
		return true
	}
	if strings.HasPrefix(pathPart, ".pytest_cache/") || pathPart == ".pytest_cache" {
		return true
	}
	if pathPart == ".coverage" {
		return true
	}
	if strings.HasPrefix(pathPart, "__pycache__/") || strings.Contains(pathPart, "/__pycache__/") {
		return true
	}
	if strings.HasSuffix(pathPart, ".pyc") || strings.HasSuffix(pathPart, ".pyo") {
		return true
	}
	return false
}

func (a *Agent) architectTestFailureFeedback(ctx context.Context, task core.TaskMetaData, inputs *architectTestCodeInputs, report architectTestCodeExecutionReport, cause error, start time.Time) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_reports", "architect_test_code_report.md")
	content := buildArchitectTestFailureReport(inputs, report, cause, common.DurationMillis(time.Since(start)))
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("写入架构师测试失败报告失败: artifact store 为空")
	}
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, fmt.Errorf("写入架构师测试失败报告失败: %w", err)
	}
	a.logStep(fmt.Sprintf("test_code failed: %v", cause))
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: []string{outputURI},
		Result:       core.TaskResultCodeFail,
	}, nil
}

func buildArchitectTestFailureReport(inputs *architectTestCodeInputs, report architectTestCodeExecutionReport, cause error, elapsedMillis int64) string {
	var builder strings.Builder
	builder.WriteString("# 架构师测试失败报告\n\n")
	builder.WriteString("## 失败摘要\n\n")
	if summary := strings.TrimSpace(report.Summary); summary != "" {
		builder.WriteString(summary)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	} else {
		builder.WriteString("架构师级代码测试失败。")
	}
	builder.WriteString("\n\n## 架构依据\n\n")
	if inputs != nil {
		builder.WriteString("- 架构书: ")
		builder.WriteString(inputs.design.URI)
		builder.WriteString("\n")
	} else {
		builder.WriteString("- 输入尚未完成解析。\n")
	}
	builder.WriteString("\n## Git 仓库\n\n")
	if inputs != nil {
		builder.WriteString("- 仓库地址: ")
		builder.WriteString(inputs.mainBranch.RepoDir)
		builder.WriteString("\n- 分支: ")
		builder.WriteString(inputs.mainBranch.Branch)
		builder.WriteString("\n- 提交: ")
		builder.WriteString(inputs.mainBranch.Commit)
		builder.WriteString("\n")
	} else {
		builder.WriteString("- 输入尚未完成解析。\n")
	}
	builder.WriteString("\n## 测试命令\n\n")
	if command := strings.TrimSpace(report.TestCommand); command != "" {
		builder.WriteString(command)
	} else {
		builder.WriteString("未报告测试命令。")
	}
	builder.WriteString("\n\n## 已尝试修复\n\n")
	builder.WriteString(fmt.Sprintf("- fixed: %t\n", report.Fixed))
	writeMergeListOrFallback(&builder, report.ChangedFiles, "未报告改动文件。")
	builder.WriteString("\n## 失败原因\n\n")
	if detail := strings.TrimSpace(report.FailureSummary); detail != "" {
		builder.WriteString(detail)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	} else {
		builder.WriteString("未提供详细失败原因。")
	}
	builder.WriteString("\n\n## 新增测试\n\n")
	writeMergeListOrFallback(&builder, report.TestsAdded, "未报告新增测试。")
	builder.WriteString("\n## 复现步骤\n\n")
	writeMergeListOrFallback(&builder, report.ReproductionSteps, "未报告复现步骤。")
	builder.WriteString("\n## 失败证据\n\n")
	writeMergeListOrFallback(&builder, report.Evidence, "未报告失败证据。")
	builder.WriteString("\n## 疑似问题文件\n\n")
	writeMergeListOrFallback(&builder, report.SuspectedFiles, "未报告疑似问题文件。")
	builder.WriteString("\n## 后续建议\n\n")
	builder.WriteString("- 根据架构书重新检查失败模块的职责边界、接口契约和关键流程。\n")
	builder.WriteString("- 优先处理失败证据中直接指向的文件和行为。\n")
	builder.WriteString("\n## 耗时\n\n")
	builder.WriteString(fmt.Sprintf("%d ms\n", elapsedMillis))
	return builder.String()
}
