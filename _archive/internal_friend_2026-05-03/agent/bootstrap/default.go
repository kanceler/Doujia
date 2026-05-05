package bootstrap

import (
	"doujia/internal/agent/executor"
	"doujia/internal/agent/handler"
	"doujia/internal/agent/llm"
	"doujia/internal/agent/registry"
	rolearchitect "doujia/internal/agent/role/architect"
	rolecoder "doujia/internal/agent/role/coder"
	rolefront "doujia/internal/agent/role/front"
	rolepm "doujia/internal/agent/role/pm"
	roletester "doujia/internal/agent/role/tester"
	architectspec "doujia/internal/agent/spec/architect"
	coderspec "doujia/internal/agent/spec/coder"
	frontspec "doujia/internal/agent/spec/front"
	pmspec "doujia/internal/agent/spec/pm"
	testerspec "doujia/internal/agent/spec/tester"
)

func NewDefaultRuntime() *executor.Runtime {
	agentRegistry := registry.NewAgentRegistry()
	opRegistry := registry.NewOpRegistry()
	handlerRegistry := registry.NewHandlerRegistry()

	agentRegistry.Register(rolearchitect.NewAgent())
	agentRegistry.Register(rolecoder.NewAgent())
	agentRegistry.Register(rolefront.NewAgent())
	agentRegistry.Register(rolepm.NewAgent())
	agentRegistry.Register(roletester.NewAgent())
	opRegistry.Register(architectspec.WritePlanSpec())
	opRegistry.Register(architectspec.CreateContainerSpec())
	opRegistry.Register(architectspec.SplitModuleSpec())
	opRegistry.Register(architectspec.MergeCodeSpec())
	opRegistry.Register(architectspec.TestDataSpec())
	opRegistry.Register(architectspec.TestCodeSpec())
	opRegistry.Register(coderspec.WriteCodeSpec())
	opRegistry.Register(frontspec.WriteCodeSpec())
	opRegistry.Register(pmspec.WritePlanSpec())
	opRegistry.Register(pmspec.ReviewPlanSpec())
	opRegistry.Register(testerspec.TestDataSpec())
	opRegistry.Register(testerspec.TestCodeSpec())
	handlerRegistry.Register(handler.NewArtifactReadHandler())
	handlerRegistry.Register(handler.NewArtifactWriteHandler())
	handlerRegistry.Register(handler.NewContainerCreateHandler())
	handlerRegistry.Register(handler.NewContainerReadHandler())
	handlerRegistry.Register(handler.NewContainerWriteHandler())
	handlerRegistry.Register(handler.NewContainerRunHandler())
	handlerRegistry.Register(handler.NewContainerExecHandler())
	handlerRegistry.Register(handler.NewContainerGitCommitHandler())
	handlerRegistry.Register(handler.NewContainerGitCherryPickHandler())
	handlerRegistry.Register(handler.NewContainerGitWorktreePrepareHandler())
	handlerRegistry.Register(handler.NewTaskCompleteHandler())

	adapter := llm.NewOpenAIAdapterFromEnv()
	return executor.NewRuntime(agentRegistry, opRegistry, handlerRegistry).
		WithLLM(adapter).
		WithToolLoop(llm.NewToolLoop(adapter))
}
