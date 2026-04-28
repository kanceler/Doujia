package agentengine

import "fmt"

func GetRecipe(op string) (Recipe, error) {
	switch op {
	case "pm_write_plan":
		return Recipe{
			Op:                 op,
			Description:        "Generate a structured PRD/plan from requirement artifacts.",
			OutputKind:         "prd",
			RequiredInputKinds: []string{"requirement"},
			SystemPrompt:       "You are a PM Agent. Generate a structured plan from the requirement input only.",
			UserInstruction:    "Produce a concise but usable plan for the architect, covering goals, user scenarios, functional requirements, non-functional constraints, and acceptance criteria.",
			OutputSchema:       `{"summary":"short summary","artifact_outputs":[{"type":"prd","filename":"plan_v1.md","content":"markdown content"}],"control":[]}`,
			MaxContextChars:    12000,
		}, nil
	case "architecture_generation":
		return Recipe{
			Op:                 op,
			Description:        "Generate a technical design from PRD artifacts.",
			OutputKind:         "design",
			RequiredInputKinds: []string{"prd"},
			SystemPrompt:       "You are an Architect Agent. Generate a technical design from the PRD input only.",
			UserInstruction:    "Produce a concise but usable technical design, covering module boundaries, data objects, interfaces, key flows, error handling, non-functional constraints, and a Global Verification Strategy section. The Global Verification Strategy must name the likely runtime/test stack and propose executable final verification command candidates. These proposed commands are strategy only; split_module will write the official structured commands later.",
			OutputSchema:       `{"summary":"short summary","artifact_outputs":[{"type":"design","filename":"architecture_v1.md","content":"markdown content"}],"control":[]}`,
			MaxContextChars:    3000,
		}, nil
	default:
		return Recipe{}, fmt.Errorf("unsupported op %q", op)
	}
}
