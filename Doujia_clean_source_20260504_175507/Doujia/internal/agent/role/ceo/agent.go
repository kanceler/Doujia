package ceo

import (
	"context"
	"fmt"
	"os"
	"strings"

	"devflow/internal/agent/core"
	rolecommon "devflow/internal/agent/role/common"
)

type Agent struct{}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Role() string {
	return "ceo"
}

func (a *Agent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	return rolecommon.DispatchByOpID(ctx, req, map[string]rolecommon.OpHandler{
		"ceo.write_plan":  a.runWritePlan,
		"ceo.review_plan": a.runReviewPlan,
	}, "unsupported_ceo_op")
}

func (a *Agent) runWritePlan(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	content, err := ceoRequirementContent(req.Bundle)
	if err != nil {
		return core.AgentResult{}, err
	}
	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("artifact_write handler is required")
	}
	resp, err := writeHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"logical_key": core.LKRequirement,
			"content":     content,
		},
	})
	if err != nil {
		return core.AgentResult{}, err
	}
	return core.AgentResult{
		Result:  "kok",
		Message: "ceo write_plan completed",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:  core.LKRequirement,
				ObjectType:  stringValue(resp.Data["object_type"]),
				ContentType: stringValue(resp.Data["content_type"]),
				Encoding:    stringValue(resp.Data["encoding"]),
				Status:      stringValue(resp.Data["status"]),
				Path:        stringValue(resp.Data["path"]),
				ArtifactURI: stringValue(resp.Data["artifact_uri"]),
			},
		},
	}, nil
}

func (a *Agent) runReviewPlan(context.Context, core.AgentRunRequest) (core.AgentResult, error) {
	return core.AgentResult{
		Result:  "kok",
		Message: "ceo review_plan completed",
		Outputs: []core.AgentOutput{},
	}, nil
}

func ceoRequirementContent(bundle core.AgentInputBundle) (string, error) {
	if path, ok := rolecommon.ArtifactPath(bundle, core.LKRequirement); ok {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return normalizeRequirementContent(string(body)), nil
	}
	return defaultSnakeRequirement(), nil
}

func normalizeRequirementContent(raw string) string {
	content := strings.TrimSpace(raw)
	if !strings.HasPrefix(content, "#") {
		content = "# 需求文档\n\n" + content
	}
	return content + "\n"
}

func defaultSnakeRequirement() string {
	lines := []string{
		"# 需求文档",
		"",
		"## 项目概述",
		"",
		"我需要一个可以直接运行和交付的单机版贪吃蛇游戏成品，最终交付物必须包含真实游戏代码，而不是占位文档或伪代码。",
		"",
		"## 目标",
		"",
		"- 提供一个可玩的贪吃蛇游戏。",
		"- 玩家可以开始新游戏、控制蛇移动、看到分数变化，并在失败后重新开始。",
		"- 代码结构清晰，方便后续继续扩展。",
		"",
		"## 核心功能",
		"",
		"- 使用键盘控制蛇的上、下、左、右移动。",
		"- 地图中随机生成食物，蛇吃到食物后身体增长。",
		"- 实时显示当前分数。",
		"- 撞墙或撞到自己时游戏结束。",
		"- 游戏结束后可以重新开始。",
		"",
		"## 体验要求",
		"",
		"- 游戏启动步骤简单，尽量减少额外依赖。",
		"- 操作响应及时，主循环稳定。",
		"- 画面可以简洁，但要清楚显示游戏区域、蛇、食物和分数。",
		"",
		"## 交付要求",
		"",
		"- 输出真实可运行的项目代码。",
		"- 提供必要的运行说明。",
		"- 尽量补充基础测试或校验方式，帮助确认游戏能正常运行。",
	}
	return strings.Join(lines, "\n") + "\n"
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}
