package coder

import "fmt"

type Recipe struct {
	Op               string
	Description      string
	RequiredURIHints []string
	OutputKind       string
	SystemPrompt     string
	UserInstruction  string
	ResultFilename   string
	MaxContextChars  int
}

func GetRecipe(op string) (Recipe, error) {
	switch op {
	case "write_code":
		return Recipe{
			Op:          "write_code",
			Description: "Implement one assigned module on a coder branch created from the provided main branch.",
			RequiredURIHints: []string{
				"/artifacts/modules/",
				"/artifacts/branches/",
			},
			OutputKind:     "branches",
			SystemPrompt:   "You are a DevFlow Coder Agent. You implement code inside a prepared git worktree.",
			ResultFilename: "coder_branch.md",
			UserInstruction: `Implement the assigned module.
Do not switch branches.
Do not merge branches.
Do not commit changes.
Only modify files required by this module.
Run relevant tests when possible.
Write execution details to .devflow/result.json.
The test_command field must contain only executable shell syntax, not test notes or natural-language descriptions.`,
			MaxContextChars: 20000,
		}, nil
	case "debug":
		return Recipe{
			Op:          "debug",
			Description: "Repair the existing coder branch using the paired tester failure report.",
			RequiredURIHints: []string{
				"/artifacts/modules/",
				"/artifacts/branches/",
				"/artifacts/test_reports/",
			},
			OutputKind:     "branches",
			SystemPrompt:   "You are a DevFlow Coder Agent. You debug and repair an existing coder branch inside its prepared git worktree.",
			ResultFilename: "coder_branch.md",
			UserInstruction: `Debug the assigned module from the tester failure report.
Work on the existing coder branch worktree.
Do not switch branches.
Do not merge branches.
Do not commit changes.
Only modify files required to fix this module.
Run the failing test or the closest relevant test when possible.
Write execution details to .devflow/result.json.
The test_command field must contain only executable shell syntax, not test notes or natural-language descriptions.`,
			MaxContextChars: 20000,
		}, nil
	default:
		return Recipe{}, fmt.Errorf("unsupported coder op %q", op)
	}
}
