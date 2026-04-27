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

type mergeCodeInputs struct {
	mainBranch common.BranchArtifact
	mainDoc    agentengine.ArtifactDocument
	modules    []agentengine.ArtifactDocument
	branches   []mergeCoderBranchInput
}

type mergeCoderBranchInput struct {
	doc    agentengine.ArtifactDocument
	branch common.CoderBranchArtifact
}

type mergeCodeExecutionReport struct {
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	MergedBranches []string `json:"merged_branches"`
	TestCommand    string   `json:"test_command"`
	TestPassed     bool     `json:"test_passed"`
	FailureSummary string   `json:"failure_summary,omitempty"`
	Conflicts      []string `json:"conflicts,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
	SuspectedFiles []string `json:"suspected_files,omitempty"`
}

func (a *Agent) executeMergeCode(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	start := time.Now()
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("architect artifact store is nil")
	}
	if a.openCodeRunner == nil {
		a.openCodeRunner = common.NewManagedOpenCodeRunner("")
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return a.mergeFailureFeedback(ctx, task, nil, mergeCodeExecutionReport{}, fmt.Errorf("解析任务元数据失败"), start)
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return a.mergeFailureFeedback(ctx, task, nil, mergeCodeExecutionReport{}, fmt.Errorf("读取输入产物失败"), start)
	}
	inputs, err := findMergeCodeInputs(docs)
	if err != nil {
		return a.mergeFailureFeedback(ctx, task, nil, mergeCodeExecutionReport{}, err, start)
	}
	if err := validateMergeCodeInputs(inputs); err != nil {
		return a.mergeFailureFeedback(ctx, task, &inputs, mergeCodeExecutionReport{}, err, start)
	}

	repoDir := filepath.FromSlash(strings.TrimSpace(inputs.mainBranch.RepoDir))
	prompt := buildMergeCodePrompt(task, inputs)
	a.logStep(fmt.Sprintf("merge_code opencode request start: repo=%s prompt_chars=%d", repoDir, len(prompt)))
	if _, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: repoDir,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	}); err != nil {
		return a.mergeFailureFeedback(ctx, task, &inputs, mergeCodeExecutionReport{}, fmt.Errorf("OC 合并执行失败"), start)
	}

	report, err := readMergeCodeReport(repoDir)
	if err != nil {
		return a.mergeFailureFeedback(ctx, task, &inputs, mergeCodeExecutionReport{}, err, start)
	}
	if strings.EqualFold(strings.TrimSpace(report.Status), "passed") && report.TestPassed {
		a.logStep(fmt.Sprintf("merge_code success: branches=%d elapsed_ms=%d", len(inputs.branches), common.DurationMillis(time.Since(start))))
		return common.FeedbackFor(task, a.runID, a.agentID, nil), nil
	}
	return a.mergeFailureFeedback(ctx, task, &inputs, report, fmt.Errorf("OC 报告合并失败或测试未通过"), start)
}

func findMergeCodeInputs(docs []agentengine.ArtifactDocument) (mergeCodeInputs, error) {
	var inputs mergeCodeInputs
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.TrimSpace(doc.URI))
		lower := strings.ToLower(uri)
		switch {
		case strings.Contains(lower, "/artifacts/modules/"):
			inputs.modules = append(inputs.modules, doc)
		case strings.Contains(lower, "/artifacts/branches/"):
			kind, err := artifactKind([]byte(doc.Content))
			if err != nil {
				return inputs, fmt.Errorf("解析分支产物失败: %s", doc.URI)
			}
			switch kind {
			case "main_branch":
				mainBranch, err := common.ParseBranchArtifact([]byte(doc.Content))
				if err != nil {
					return inputs, fmt.Errorf("解析主分支产物失败: %s", doc.URI)
				}
				inputs.mainBranch = mainBranch
				inputs.mainDoc = doc
			case "coder_branch":
				coderBranch, err := common.ParseCoderBranchArtifact([]byte(doc.Content))
				if err != nil {
					return inputs, fmt.Errorf("解析程序员分支产物失败: %s", doc.URI)
				}
				inputs.branches = append(inputs.branches, mergeCoderBranchInput{doc: doc, branch: coderBranch})
			}
		}
	}
	if strings.TrimSpace(inputs.mainDoc.URI) == "" {
		return inputs, fmt.Errorf("merge_code 需要 architect split_module 输出的 /artifacts/branches/ 主分支产物")
	}
	if len(inputs.modules) == 0 {
		return inputs, fmt.Errorf("merge_code 需要至少一个 /artifacts/modules/ 子模块任务书")
	}
	if len(inputs.branches) == 0 {
		return inputs, fmt.Errorf("merge_code 需要至少一个 coder 输出的 /artifacts/branches/ 程序员分支产物")
	}
	return inputs, nil
}

func artifactKind(content []byte) (string, error) {
	var payload struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Kind), nil
}

func validateMergeCodeInputs(inputs mergeCodeInputs) error {
	mainRepo := filepath.Clean(filepath.FromSlash(inputs.mainBranch.RepoDir))
	mainBranch := strings.TrimSpace(inputs.mainBranch.Branch)
	mainCommit := strings.TrimSpace(inputs.mainBranch.Commit)
	for _, item := range inputs.branches {
		branchRepo := filepath.Clean(filepath.FromSlash(item.branch.RepoDir))
		if !samePath(mainRepo, branchRepo) {
			return fmt.Errorf("程序员分支仓库地址必须和主分支仓库地址一致: main=%s branch=%s", mainRepo, branchRepo)
		}
		if baseBranch := strings.TrimSpace(item.branch.BaseBranch); baseBranch != "" && mainBranch != "" && baseBranch != mainBranch {
			return fmt.Errorf("程序员分支基础分支必须和主分支一致: main=%s branch_base=%s", mainBranch, baseBranch)
		}
		if baseCommit := strings.TrimSpace(item.branch.BaseCommit); baseCommit != "" && mainCommit != "" && baseCommit != mainCommit {
			return fmt.Errorf("程序员分支基础提交必须和主分支提交一致: main=%s branch_base=%s", mainCommit, baseCommit)
		}
	}
	return nil
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func buildMergeCodePrompt(task core.TaskMetaData, inputs mergeCodeInputs) string {
	testCommand := mergeTestCommand(inputs)
	var builder strings.Builder
	builder.WriteString("# 合并程序员分支\n\n")
	builder.WriteString("# 角色\n\n")
	builder.WriteString("你是 DevFlow 架构师 Agent，负责把多个程序员分支合并到 split_module 创建的主仓库工作区。\n\n")
	builder.WriteString("# 任务\n\n")
	builder.WriteString(fmt.Sprintf("- task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("- agent_id: %s\n", task.AgentID))
	builder.WriteString(fmt.Sprintf("- op: %s\n", task.Op))
	builder.WriteString("\n# 主分支仓库\n\n")
	builder.WriteString(fmt.Sprintf("- 仓库地址: %s\n", inputs.mainBranch.RepoDir))
	builder.WriteString(fmt.Sprintf("- 主分支: %s\n", inputs.mainBranch.Branch))
	builder.WriteString(fmt.Sprintf("- 主分支提交: %s\n", inputs.mainBranch.Commit))
	builder.WriteString(fmt.Sprintf("- 建议测试命令: %s\n", testCommand))
	builder.WriteString("\n# 程序员分支\n\n")
	for _, item := range inputs.branches {
		builder.WriteString(fmt.Sprintf("- 分支: %s\n", item.branch.Branch))
		builder.WriteString(fmt.Sprintf("  - 提交: %s\n", item.branch.Commit))
		builder.WriteString(fmt.Sprintf("  - 产物: %s\n", item.doc.URI))
		builder.WriteString(fmt.Sprintf("  - 模块任务书: %s\n", item.branch.ModuleTaskURI))
	}
	builder.WriteString("\n# 输入文档\n")
	writeMergePromptDoc(&builder, "主分支产物", inputs.mainDoc)
	for _, doc := range inputs.modules {
		writeMergePromptDoc(&builder, "子模块任务书", doc)
	}
	for _, item := range inputs.branches {
		writeMergePromptDoc(&builder, "程序员分支产物", item.doc)
	}
	builder.WriteString("\n# 合并要求\n\n")
	builder.WriteString("1. 你当前位于主分支仓库工作区，最终合并必须发生在上面给出的主分支仓库地址中。\n")
	builder.WriteString("2. 按程序员分支产物中的分支逐个合并，不要跳过任何分支。\n")
	builder.WriteString("3. 遇到冲突时，根据子模块任务书中的职责范围、接口契约、依赖关系和实现说明进行解决。\n")
	builder.WriteString("4. 不要删除其他模块的代码，不要用一个模块的实现覆盖另一个模块的职责。\n")
	builder.WriteString("5. 合并完成后运行最终测试命令；如果多个分支提供了不同测试命令，优先运行覆盖全仓库的测试。\n")
	builder.WriteString("6. 合并和测试成功后提交合并结果。\n")
	builder.WriteString("7. 无论成功或失败，都必须写入 .devflow/result.json。\n")
	builder.WriteString("\n# Mandatory Completion Contract\n\n")
	builder.WriteString("- Before finishing, create the .devflow directory if it does not exist.\n")
	builder.WriteString("- Before finishing, write .devflow/result.json exactly once with the schema below.\n")
	builder.WriteString("- This report is mandatory even when merge/tests pass and even when no code changes are needed.\n")
	builder.WriteString("- If you cannot merge or run tests, still write .devflow/result.json with status=\"failed\", test_passed=false, and a failure_summary.\n")
	builder.WriteString("- test_command must be executable shell syntax only; put explanations in summary or evidence.\n")
	builder.WriteString("\n# .devflow/result.json 格式\n\n")
	builder.WriteString(`{"status":"passed|failed","summary":"简短总结","merged_branches":["已合并的分支"],"test_command":"实际执行的测试命令","test_passed":true,"failure_summary":"失败原因说明","conflicts":["冲突文件或冲突说明"],"evidence":["关键日志、测试输出或判断依据"],"suspected_files":["可能需要继续处理的文件"]}`)
	builder.WriteString("\n")
	return builder.String()
}

func writeMergePromptDoc(builder *strings.Builder, title string, doc agentengine.ArtifactDocument) {
	builder.WriteString("\n---\n")
	builder.WriteString(title)
	builder.WriteString("\n来源: ")
	builder.WriteString(doc.URI)
	builder.WriteString("\n")
	builder.WriteString(doc.Content)
	builder.WriteString("\n")
}

func mergeTestCommand(inputs mergeCodeInputs) string {
	for _, item := range inputs.branches {
		if command := strings.TrimSpace(item.branch.TestCommand); command != "" {
			return command
		}
	}
	return "go test ./..."
}

func readMergeCodeReport(repoDir string) (mergeCodeExecutionReport, error) {
	content, err := os.ReadFile(filepath.Join(repoDir, ".devflow", "result.json"))
	if err != nil {
		return mergeCodeExecutionReport{}, fmt.Errorf("读取 .devflow/result.json 失败: %w", err)
	}
	var report mergeCodeExecutionReport
	if err := json.Unmarshal(content, &report); err != nil {
		return mergeCodeExecutionReport{}, fmt.Errorf("解析 .devflow/result.json 失败: %w", err)
	}
	if strings.TrimSpace(report.Status) == "" {
		return mergeCodeExecutionReport{}, fmt.Errorf(".devflow/result.json status 不能为空")
	}
	return report, nil
}

func (a *Agent) mergeFailureFeedback(ctx context.Context, task core.TaskMetaData, inputs *mergeCodeInputs, report mergeCodeExecutionReport, cause error, start time.Time) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "merge_reports", "merge_code_report.md")
	content := buildMergeFailureReport(inputs, report, cause, common.DurationMillis(time.Since(start)))
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("写入合并失败报告失败: artifact store 为空")
	}
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, fmt.Errorf("写入合并失败报告失败: %w", err)
	}
	a.logStep(fmt.Sprintf("merge_code failed: %v", cause))
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

func buildMergeFailureReport(inputs *mergeCodeInputs, report mergeCodeExecutionReport, cause error, elapsedMillis int64) string {
	var builder strings.Builder
	builder.WriteString("# 合并失败报告\n\n")
	builder.WriteString("## 失败摘要\n\n")
	if summary := strings.TrimSpace(report.Summary); summary != "" {
		builder.WriteString(summary)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	} else {
		builder.WriteString("merge_code 执行失败。")
	}
	builder.WriteString("\n\n## 失败原因\n\n")
	if detail := strings.TrimSpace(report.FailureSummary); detail != "" {
		builder.WriteString(detail)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	} else {
		builder.WriteString("未提供详细失败原因。")
	}
	builder.WriteString("\n\n## 输入产物\n\n")
	if inputs == nil {
		builder.WriteString("- 输入尚未完成解析。\n")
	} else {
		builder.WriteString("- 主分支产物: ")
		builder.WriteString(inputs.mainDoc.URI)
		builder.WriteString("\n")
		builder.WriteString("- 主分支仓库: ")
		builder.WriteString(inputs.mainBranch.RepoDir)
		builder.WriteString("\n")
		for _, doc := range inputs.modules {
			builder.WriteString("- 子模块任务书: ")
			builder.WriteString(doc.URI)
			builder.WriteString("\n")
		}
		for _, item := range inputs.branches {
			builder.WriteString("- 程序员分支: ")
			builder.WriteString(item.branch.Branch)
			builder.WriteString(" repo=")
			builder.WriteString(item.branch.RepoDir)
			builder.WriteString(" artifact=")
			builder.WriteString(item.doc.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## 合并顺序\n\n")
	writeMergeListOrFallback(&builder, report.MergedBranches, "未报告已合并分支。")
	builder.WriteString("\n## 冲突信息\n\n")
	writeMergeListOrFallback(&builder, report.Conflicts, "未报告冲突信息。")
	builder.WriteString("\n## 测试命令\n\n")
	if command := strings.TrimSpace(report.TestCommand); command != "" {
		builder.WriteString(command)
	} else {
		builder.WriteString("未报告测试命令。")
	}
	builder.WriteString("\n\n## 证据\n\n")
	writeMergeListOrFallback(&builder, report.Evidence, "未报告证据。")
	builder.WriteString("\n## 疑似问题文件\n\n")
	writeMergeListOrFallback(&builder, report.SuspectedFiles, "未报告疑似问题文件。")
	builder.WriteString("\n## 耗时\n\n")
	builder.WriteString(fmt.Sprintf("%d ms\n", elapsedMillis))
	return builder.String()
}

func writeMergeListOrFallback(builder *strings.Builder, items []string, fallback string) {
	wrote := false
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
		wrote = true
	}
	if !wrote {
		builder.WriteString("- ")
		builder.WriteString(fallback)
		builder.WriteString("\n")
	}
}
