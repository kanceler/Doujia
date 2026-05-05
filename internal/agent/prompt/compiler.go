package prompt

import (
	"fmt"
	"strings"

	"devflow/internal/agent/core"
	"devflow/internal/agent/schema"
)

func Compile(task core.Task, bundle core.AgentInputBundle, spec core.OpSpec) string {
	var b strings.Builder
	mode := core.NormalizeExecutionMode(task.ExecutionMode)

	fmt.Fprintf(&b, "你是 role 为 %s 的 Agent。\n\n", task.Role)
	fmt.Fprintf(&b, "角色说明：\n%s\n\n", spec.RoleDescription)
	fmt.Fprintf(&b, "当前任务：\n- role: %s\n- op: %s\n- execution_mode: %s\n\n", task.Role, task.Op, mode)
	writeExecutionModeRules(&b, mode)
	fmt.Fprintf(&b, "操作说明：\n%s\n\n", spec.OpDescription)

	b.WriteString("可读取的输入 logical_key：\n")
	if len(bundle.Inputs) == 0 {
		b.WriteString("- 无\n")
	}
	for i, input := range bundle.Inputs {
		fmt.Fprintf(&b, "%d. %s\n", i+1, input.LogicalKey)
		fmt.Fprintf(&b, "   说明：%s\n", input.Description)
		fmt.Fprintf(&b, "   artifact_version_id: %s\n", input.ArtifactVersionID)
		if input.LogicalArtifactID != "" {
			fmt.Fprintf(&b, "   logical_artifact_id: %s\n", input.LogicalArtifactID)
		}
		if input.ObjectType != "" {
			fmt.Fprintf(&b, "   object_type: %s\n", input.ObjectType)
		}
	}
	b.WriteString("\n")

	if len(bundle.PreviousOutputs) > 0 {
		b.WriteString("可复用的上一轮输出 logical_key：\n")
		for i, previous := range bundle.PreviousOutputs {
			fmt.Fprintf(&b, "%d. %s\n", i+1, previous.LogicalKey)
			fmt.Fprintf(&b, "   说明：%s\n", previous.Description)
			fmt.Fprintf(&b, "   artifact_version_id: %s\n", previous.ArtifactVersionID)
			if previous.LogicalArtifactID != "" {
				fmt.Fprintf(&b, "   logical_artifact_id: %s\n", previous.LogicalArtifactID)
			}
			if previous.ObjectType != "" {
				fmt.Fprintf(&b, "   object_type: %s\n", previous.ObjectType)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("可用工具：\n")
	if len(spec.AllowedTools) == 0 {
		b.WriteString("- 无\n")
	}
	for _, tool := range spec.AllowedTools {
		fmt.Fprintf(&b, "- %s: %s\n", tool.Name, tool.Description)
	}
	b.WriteString("\n")

	b.WriteString("输出要求：\n")
	for i, output := range spec.ExpectedOutputs {
		fmt.Fprintf(&b, "%d. logical_key: %s\n", i+1, output.LogicalKey)
		fmt.Fprintf(&b, "   object_type: %s\n", output.ObjectType)
		if output.ContentType != "" {
			fmt.Fprintf(&b, "   content_type: %s\n", output.ContentType)
		}
		if output.Encoding != "" {
			fmt.Fprintf(&b, "   encoding: %s\n", output.Encoding)
		}
		fmt.Fprintf(&b, "   是否必需：%t\n", output.Required)
		fmt.Fprintf(&b, "   说明：%s\n", output.Description)
	}
	b.WriteString("\n")

	b.WriteString("工具规则：\n")
	b.WriteString("- 不要猜测输入文件内容。\n")
	b.WriteString("- 只能使用上方列出的工具，不要调用未声明工具。\n")
	b.WriteString("- 如果需要读取输入产物或上一轮输出，只能使用 artifact_read(logical_key)。\n")
	b.WriteString("- 如果需要写出声明的最终产物，只能使用 artifact_write(logical_key, content)。\n")
	b.WriteString("- 如果需要在容器工作区中读写文件或执行命令，只能使用声明的 container_* 工具。\n")
	b.WriteString("- 不要自行编造或传入 path、output_dir、file_name 等路径参数；运行时会根据 logical_key 或容器上下文解析真实路径。\n")
	b.WriteString("- 如果 requirement 或 op description 没有明确要求，不要添加里程碑、时间线、排期、预估工时等章节。\n")
	b.WriteString("- 如果上游已提供 seed_tests 或其他基础产物，优先复用并做最小增强，不要重写一整套冗长的大模板。\n")
	b.WriteString("- 如果 requirement 没有明确要求其他语言，自然语言产物默认使用简体中文。\n")
	writeStrictJSONOutputContracts(&b, spec.ExpectedOutputs)
	if allowsTool(spec.AllowedTools, "task_complete") {
		b.WriteString("- 完成必要工具调用后，调用 task_complete(result, message, outputs?, errors?) 返回最终 AgentResult；不要直接输出最终 JSON 文本。\n")
	} else {
		b.WriteString("- 完成必要工具调用后，直接返回最终 AgentResult JSON 文本，不要再调用未声明工具。\n")
	}
	b.WriteString("- 当 result = kok 时，最终 AgentResult 必须包含每个必需输出；当 result = kbug 或 kfail 时，只返回能说明问题的声明输出，不要伪造必需输出。\n")

	return b.String()
}

func writeStrictJSONOutputContracts(b *strings.Builder, outputs []core.OutputSpec) {
	jsonOutputs := make([]core.OutputSpec, 0)
	for _, output := range outputs {
		if strings.EqualFold(strings.TrimSpace(output.ObjectType), "json") {
			jsonOutputs = append(jsonOutputs, output)
		}
	}
	if len(jsonOutputs) == 0 {
		return
	}
	b.WriteString("\nSTRICT JSON OUTPUT CONTRACTS:\n")
	b.WriteString("- For every JSON artifact, artifact_write.content must be raw valid JSON only.\n")
	b.WriteString("- Do not wrap JSON in Markdown code fences. Do not add comments or explanatory text outside JSON.\n")
	b.WriteString("- Include every required field shown in the example for that logical_key.\n")
	for _, output := range jsonOutputs {
		fmt.Fprintf(b, "- logical_key: %s", output.LogicalKey)
		if output.FileName != "" {
			fmt.Fprintf(b, " file_name: %s", output.FileName)
		}
		b.WriteString("\n")
		if contract := schema.JSONArtifactContract(output.LogicalKey); contract != "" {
			fmt.Fprintf(b, "  Contract: %s\n", contract)
		}
		if example := schema.JSONArtifactExample(output.LogicalKey); example != "" {
			b.WriteString("  Example:\n")
			for _, line := range strings.Split(example, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				fmt.Fprintf(b, "  %s\n", line)
			}
		}
	}
	b.WriteString("\n")
}

func writeExecutionModeRules(b *strings.Builder, mode string) {
	switch mode {
	case core.ExecutionModeRepair:
		b.WriteString("执行模式说明：\n")
		b.WriteString("当前执行模式：repair\n")
		b.WriteString("你正在重新执行一个之前失败或需要修复的任务。\n")
		b.WriteString("- 必须先读取 repair_instruction。\n")
		b.WriteString("- 如果提供 previous_outputs，请先判断哪些输出可以复用。\n")
		b.WriteString("- 只修复 repair_instruction 指定的问题。\n")
		b.WriteString("- 不需要修改的产物应尽量复用，不要无理由重新生成所有产物。\n")
		b.WriteString("- 如果某个输出复用，请在 AgentResult.outputs 中标记 status = reused，并提供 artifact_version_id。\n")
		b.WriteString("- 如果发现上游产物仍有问题，返回 kbug，并输出 upstream_artifact_issue.md。\n\n")
	case core.ExecutionModeNormal:
		b.WriteString("执行模式说明：\n")
		b.WriteString("当前执行模式：normal\n")
		b.WriteString("请根据输入产物完成本 op 的正常产出。\n\n")
	case core.ExecutionModeReuse:
		b.WriteString("执行模式说明：\n")
		b.WriteString("当前执行模式：reuse\n")
		b.WriteString("本次不需要重新生成内容，请根据 previous_outputs 返回 reused outputs。\n\n")
	case core.ExecutionModeRewrite:
		b.WriteString("执行模式说明：\n")
		b.WriteString("当前执行模式：rewrite\n")
		b.WriteString("你需要根据 rewrite_instruction 重写指定产物；如果 previous_outputs 中存在不相关产物，可以复用。\n\n")
	default:
		b.WriteString("执行模式说明：\n")
		fmt.Fprintf(b, "当前执行模式：%s\n\n", mode)
	}
}

func allowsTool(tools []core.ToolSpec, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
