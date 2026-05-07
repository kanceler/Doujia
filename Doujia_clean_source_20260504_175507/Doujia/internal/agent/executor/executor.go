package executor

import (
	"context"
	"fmt"
	"strings"

	"devflow/internal/agent/core"
	"devflow/internal/agent/prompt"
	"devflow/internal/agent/registry"
	"devflow/internal/agent/schema"
)

type AgentRegistryLike interface {
	Get(role string) (core.Agent, bool)
}

type OpRegistryLike interface {
	Get(role, op string) (core.OpSpec, bool)
	GetByID(opID string) (core.OpSpec, bool)
}

type RoleSpecRegistryLike interface {
	RoleSpec(roleID string) (core.RoleSpec, bool)
}

type Runtime struct {
	agentRegistry   AgentRegistryLike
	opRegistry      OpRegistryLike
	roleRegistry    RoleSpecRegistryLike
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

func (r *Runtime) WithRoleRegistry(roleRegistry RoleSpecRegistryLike) *Runtime {
	r.roleRegistry = roleRegistry
	return r
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
	effectiveTask.OpID = resolveTaskOpID(effectiveTask, r.roleRegistry)

	spec, ok := resolveOpSpec(effectiveTask, r.opRegistry)
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
		result.ProducedBags = resolveProducedBags(effectiveTask, bundle, spec, result)
		if err := ValidateOutputs(effectiveTask, bundle, spec, result); err != nil {
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
	result.ProducedBags = resolveProducedBags(effectiveTask, bundle, spec, result)
	if err := ValidateOutputs(effectiveTask, bundle, spec, result); err != nil {
		return failResult("invalid_agent_outputs", err.Error()), nil
	}

	return result, nil
}

func resolveProducedBags(task core.Task, bundle core.AgentInputBundle, spec core.OpSpec, result core.AgentResult) []core.ProducedBagManifest {
	resolved := spec.ResolveProducedBags(task, bundle, result)
	if len(result.ProducedBags) == 0 {
		return resolved
	}
	if len(resolved) == 0 {
		return result.ProducedBags
	}
	return fillProducedBagIndexes(result.ProducedBags, resolved)
}

func fillProducedBagIndexes(actual, resolved []core.ProducedBagManifest) []core.ProducedBagManifest {
	resolvedByName := make(map[string][]core.ProducedBagManifest)
	for _, bag := range resolved {
		name := strings.TrimSpace(bag.Name)
		if name == "" || len(bag.Indexes) == 0 {
			continue
		}
		resolvedByName[name] = append(resolvedByName[name], bag)
	}
	usedByName := make(map[string]int)
	out := make([]core.ProducedBagManifest, len(actual))
	for i, bag := range actual {
		next := bag
		name := strings.TrimSpace(next.Name)
		if len(next.Indexes) == 0 && len(resolvedByName[name]) > 0 {
			pos := usedByName[name]
			if pos >= len(resolvedByName[name]) {
				pos = len(resolvedByName[name]) - 1
			}
			next.Indexes = cloneStringMap(resolvedByName[name][pos].Indexes)
			usedByName[name]++
		}
		out[i] = next
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func resolveTaskOpID(task core.Task, roleRegistry RoleSpecRegistryLike) string {
	if opID := strings.TrimSpace(task.OpID); opID != "" {
		return opID
	}
	if roleRegistry != nil {
		if spec, ok := roleRegistry.RoleSpec(task.Role); ok {
			if opID, ok := registry.ResolveRoleOpID(spec, task.Op); ok {
				return opID
			}
		}
	}
	return strings.TrimSpace(task.Role + "." + task.Op)
}

func resolveOpSpec(task core.Task, opRegistry OpRegistryLike) (core.OpSpec, bool) {
	if opRegistry == nil {
		return core.OpSpec{}, false
	}
	if opID := strings.TrimSpace(task.OpID); opID != "" {
		if spec, ok := opRegistry.GetByID(opID); ok {
			return spec, true
		}
	}
	return opRegistry.Get(task.Role, task.Op)
}

func resolveEffectiveTask(task core.Task, bundle core.AgentInputBundle) (core.Task, error) {
	effectiveTask := task
	if !isWriteCodeLikeOp(task.Op) {
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

func isWriteCodeLikeOp(op string) bool {
	switch strings.TrimSpace(op) {
	case "write_code", "debug_write_code":
		return true
	default:
		return false
	}
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
	if _, ok := r.allowed[name]; !ok && !containsString(r.spec.PreflightHandlers, name) {
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

func allowedToolNames(spec core.OpSpec) map[string]struct{} {
	allowed := make(map[string]struct{}, len(spec.AllowedTools))
	for _, tool := range spec.AllowedTools {
		allowed[tool.Name] = struct{}{}
	}
	return allowed
}

func containsString(items []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
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
