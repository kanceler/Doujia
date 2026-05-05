package pipeline

import "devflow/internal/core"

type StageSpec struct {
	ID           core.StageID
	Name         string
	AgentRole    core.AgentRole
	AgentAlias   core.AgentID
	Op           string
	DependsOnIDs []core.StageID
	External     bool
	InputBags    []BagSpec
	OutputBags   []BagSpec
}

type PipelineSpec struct {
	ID     core.PipelineID
	Name   string
	Stages []StageSpec
}
