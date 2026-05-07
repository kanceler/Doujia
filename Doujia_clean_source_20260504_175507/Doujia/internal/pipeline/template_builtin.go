package pipeline

import "devflow/internal/core"

const PipelineIDPhaseOne core.PipelineID = "phase_one_requirement_flow"
const PipelineIDPhaseTwo core.PipelineID = "phase_two_delivery_flow"

func BuiltinPhaseOne() PipelineSpec {
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
				ID:           "task_02",
				Name:         "PM Write Plan",
				AgentRole:    core.AgentRolePM,
				AgentAlias:   "pm01",
				Op:           "pm_write_plan",
				DependsOnIDs: []core.StageID{"task_01"},
			},
			{
				ID:           "task_03",
				Name:         "CEO Review Plan",
				AgentRole:    core.AgentRoleCEO,
				AgentAlias:   "ceo",
				Op:           "ceo_review_plan",
				DependsOnIDs: []core.StageID{"task_02"},
			},
			{
				ID:           "task_04",
				Name:         "Architect Write Design",
				AgentRole:    core.AgentRoleArchitect,
				AgentAlias:   "architect01",
				Op:           "architecture_generation",
				DependsOnIDs: []core.StageID{"task_03"},
			},
			{
				ID:           "task_05",
				Name:         "PM Review Design",
				AgentRole:    core.AgentRolePM,
				AgentAlias:   "pm01",
				Op:           "pm_review_design",
				DependsOnIDs: []core.StageID{"task_04"},
			},
			{
				ID:           "task_06",
				Name:         "CEO User Confirm",
				AgentRole:    core.AgentRoleCEO,
				AgentAlias:   "ceo",
				Op:           "ceo_user_confirm",
				DependsOnIDs: []core.StageID{"task_05"},
			},
		},
	}
}

func BuiltinPhaseTwo() PipelineSpec {
	return PipelineSpec{
		ID:   PipelineIDPhaseTwo,
		Name: "Phase Two Delivery Flow",
		Stages: []StageSpec{
			{
				ID:         "task_01",
				Name:       "CEO Write Plan",
				AgentRole:  core.AgentRoleCEO,
				AgentAlias: "ceo",
				Op:         core.TaskOpWritePlan,
				External:   true,
			},
			{
				ID:           "task_02",
				Name:         "PM Write Plan",
				AgentRole:    core.AgentRolePM,
				AgentAlias:   "pm01",
				Op:           core.TaskOpWritePlan,
				DependsOnIDs: []core.StageID{"task_01"},
			},
			{
				ID:           "task_03",
				Name:         "CEO Review Plan",
				AgentRole:    core.AgentRoleCEO,
				AgentAlias:   "ceo",
				Op:           core.TaskOpReviewPlan,
				DependsOnIDs: []core.StageID{"task_02"},
			},
			{
				ID:           "task_04",
				Name:         "Architect Write Plan",
				AgentRole:    core.AgentRoleArchitect,
				AgentAlias:   "architect01",
				Op:           core.TaskOpWritePlan,
				DependsOnIDs: []core.StageID{"task_03"},
			},
			{
				ID:           "task_05",
				Name:         "PM Review Plan",
				AgentRole:    core.AgentRolePM,
				AgentAlias:   "pm01",
				Op:           core.TaskOpReviewPlan,
				DependsOnIDs: []core.StageID{"task_04"},
			},
			{
				ID:           "task_06",
				Name:         "Architect Split Module",
				AgentRole:    core.AgentRoleArchitect,
				AgentAlias:   "architect01",
				Op:           core.TaskOpSplitModule,
				DependsOnIDs: []core.StageID{"task_05"},
			},
		},
	}
}
