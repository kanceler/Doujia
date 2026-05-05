package executor

import (
	"context"
	"fmt"
	"strings"

	"doujia/internal/agent/core"
	"doujia/internal/agent/prompt"
	"doujia/internal/agent/schema"
)

type AgentRegistryLike interface {
	Get(role string) (core.Agent, bool)
}

type OpRegistryLike interface {
	Get(role, op string) (core.OpSpec, bool)
}

type Runtime struct {
	agentRegistry   AgentRegistryLike
	opRegistry      OpRegistryLike
	handlerRegistry core.HandlerRegistryLike
	llm             core.LLMClientLike
	toolLoop        core.ToolLoopLike
}

func NewRuntime(agentRegistry AgentRegistryLike, opRegistry OpRegistryLike, handlerRegistry core.HandlerRegistryLike) *Runtime {
	return &Runtime{
		agentRegistry:   agentRegistry,
		opRegistry:      opRegistry,
		handlerRegistry: handlerRegistry,
	}
}

func (r *Runtime) WithLLM(llm core.LLMClientLike) *Runtime {
	r.llm = llm
	return r
}

func (r *Runtime) WithToolLoop(toolLoop core.ToolLoopLike) *Runtime {
	r.toolLoop = toolLoop
	return r
}

func (r *Runtime) Execute(ctx context.Context, task core.Task, bundle core.AgentInputBundle) (core.AgentResult, error) {
	return r.RunAgent(ctx, task, bundle)
}

func (r *Runtime) RunAgent(ctx context.Context, task core.Task, bundle core.AgentInputBundle) (core.AgentResult, error) {
	effectiveTask, err := resolveEffectiveTask(task, bundle)
	if err != nil {
		return failResult("invalid_agent_inputs", err.Error()), nil
	}
	effectiveTask.ExecutionMode = core.NormalizeExecutionMode(effectiveTask.ExecutionMode)

	spec, ok := r.opRegistry.Get(effectiveTask.Role, effectiveTask.Op)
	if !ok {
		return failResult("unknown_op", "op not registered"), nil
	}

	agent, ok := r.agentRegistry.Get(effectiveTask.Role)
	if !ok {
		return failResult("unknown_agent_role", "agent role not registered"), nil
	}

	if err := ValidateInputs(effectiveTask, bundle, spec); err != nil {
		return failResult("invalid_agent_inputs", err.Error()), nil
	}
	resolvedOutputs, err := spec.ResolveExpectedOutputs(bundle)
	if err != nil {
		return failResult("invalid_op_spec", err.Error()), nil
	}
	spec.ExpectedOutputs = resolvedOutputs

	if effectiveTask.ExecutionMode == core.ExecutionModeReuse {
		result, err := BuildReuseResult(effectiveTask, bundle, spec)
		if err != nil {
			return failResult("invalid_agent_outputs", err.Error()), nil
		}
		return result, nil
	}

	req := core.AgentRunRequest{
		Task:   effectiveTask,
		Bundle: bundle,
		OpSpec: spec,
		Prompt: prompt.Compile(effectiveTask, bundle, spec),
		Handlers: scopedHandlerRegistry{
			base:    r.handlerRegistry,
			bundle:  bundle,
			spec:    spec,
			task:    effectiveTask,
			allowed: allowedToolNames(spec),
		},
		LLM:      r.llm,
		ToolLoop: r.toolLoop,
	}
	result, err := agent.Run(ctx, req)
	if err != nil {
		return failResult("agent_run_failed", err.Error()), nil
	}
	if err := ValidateOutputs(effectiveTask, bundle, spec, result); err != nil {
		return failResult("invalid_agent_outputs", err.Error()), nil
	}

	return result, nil
}

func resolveEffectiveTask(task core.Task, bundle core.AgentInputBundle) (core.Task, error) {
	effectiveTask := task
	if strings.TrimSpace(task.Op) != "write_code" {
		return effectiveTask, nil
	}

	moduleSpecPath, ok := moduleSpecPathFromBundle(bundle)
	if !ok {
		return effectiveTask, nil
	}

	moduleSpec, err := schema.ReadModuleSpecFile(moduleSpecPath)
	if err != nil {
		return effectiveTask, fmt.Errorf("read module_spec: %w", err)
	}
	if err := moduleSpec.Validate(); err != nil {
		return effectiveTask, fmt.Errorf("invalid module_spec: %w", err)
	}

	effectiveTask.Role = moduleSpec.ImplementationRole
	return effectiveTask, nil
}

func moduleSpecPathFromBundle(bundle core.AgentInputBundle) (string, bool) {
	for _, input := range bundle.Inputs {
		if input.LogicalKey != core.LKModuleSpec {
			continue
		}
		if strings.TrimSpace(input.Path) == "" {
			return "", false
		}
		return input.Path, true
	}
	return "", false
}

func failResult(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}

type scopedHandlerRegistry struct {
	base    core.HandlerRegistryLike
	bundle  core.AgentInputBundle
	spec    core.OpSpec
	task    core.Task
	allowed map[string]struct{}
}

func (r scopedHandlerRegistry) Get(name string) (core.Handler, bool) {
	if _, ok := r.allowed[name]; !ok && !isInternalAgentHandler(name) {
		return nil, false
	}
	handler, ok := r.base.Get(name)
	if !ok {
		return nil, false
	}
	if scoped, ok := handler.(core.ScopedHandler); ok {
		return scoped.WithScope(r.bundle), true
	}
	return handler, true
}

func isInternalAgentHandler(name string) bool {
	switch name {
	case "container_git_worktree_prepare":
		return true
	default:
		return false
	}
}

func allowedToolNames(spec core.OpSpec) map[string]struct{} {
	allowed := make(map[string]struct{}, len(spec.AllowedTools))
	for _, tool := range spec.AllowedTools {
		allowed[tool.Name] = struct{}{}
	}
	return allowed
}

func agentFailureMessage(result core.AgentResult) string {
	if result.Message != "" {
		return result.Message
	}
	if len(result.Errors) > 0 && result.Errors[0].Message != "" {
		return result.Errors[0].Message
	}
	return "agent returned failure result"
}
