package pipeline

import "devflow/internal/core"

const PipelineIDPhaseOne core.PipelineID = "phase_one_requirement_flow"

func BuiltinPhaseOne() PipelineSpec {
	task02Depends := core.StageID("task_01")
	task03Depends := core.StageID("task_02")
	task04Depends := core.StageID("task_03")
	task05Depends := core.StageID("task_04")
	task06Depends := core.StageID("task_05")

	return PipelineSpec{
		ID:   PipelineIDPhaseOne,
		Name: "Phase One Requirement Flow",
		Stages: []StageSpec{
			{
				ID:         "task_01",
				Name:       "CEO Write Requirement",
				AgentRole:  core.AgentRoleCEO,
				AgentAlias: "ceo",
				Op:         "ceo_write_requirement",
				External:   true,
			},
			{
				ID:         "task_02",
				Name:       "PM Write Plan",
				AgentRole:  core.AgentRolePM,
				AgentAlias: "pm01",
				Op:         "pm_write_plan",
				DependsOn:  &task02Depends,
			},
			{
				ID:         "task_03",
				Name:       "CEO Review Plan",
				AgentRole:  core.AgentRoleCEO,
				AgentAlias: "ceo",
				Op:         "ceo_review_plan",
				DependsOn:  &task03Depends,
			},
			{
				ID:         "task_04",
				Name:       "Architect Write Design",
				AgentRole:  core.AgentRoleArchitect,
				AgentAlias: "architect01",
				Op:         "architecture_generation",
				DependsOn:  &task04Depends,
			},
			{
				ID:         "task_05",
				Name:       "PM Review Design",
				AgentRole:  core.AgentRolePM,
				AgentAlias: "pm01",
				Op:         "pm_review_design",
				DependsOn:  &task05Depends,
			},
			{
				ID:         "task_06",
				Name:       "CEO User Confirm",
				AgentRole:  core.AgentRoleCEO,
				AgentAlias: "ceo",
				Op:         "ceo_user_confirm",
				DependsOn:  &task06Depends,
			},
		},
	}
}
