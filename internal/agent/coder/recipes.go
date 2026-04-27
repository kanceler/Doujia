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
Write execution details to .devflow/result.json.`,
			MaxContextChars: 20000,
		}, nil
	default:
		return Recipe{}, fmt.Errorf("unsupported coder op %q", op)
	}
}
