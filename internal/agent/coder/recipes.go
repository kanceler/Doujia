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
				"/artifacts/contracts/",
				"/artifacts/seed_tests/",
			},
			OutputKind:     "branches",
			SystemPrompt:   "You are a DevFlow Coder Agent. You implement code inside a prepared git worktree.",
			ResultFilename: "coder_branch.md",
			UserInstruction: `Implement the assigned module.
Do not switch branches.
Do not merge branches.
Do not commit changes.
Only modify product code, configuration, or documentation files required by this module.
Do not create, modify, delete, or move seed test files; the host rewrites seed_tests.json before verification.
Do not create ad-hoc smoke tests or invent public APIs during write_code.
Write execution details to .devflow/result.json.
Set test_command to the official seed test command from seed_tests.json.
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
Only modify product code, configuration, or documentation files required to fix this module.
Do not create, modify, delete, or move test files; TesterAgent owns test data and executable tests.
Use the tester failure report to guide the repair, but do not perform formal verification.
Write execution details to .devflow/result.json.
The test_command field must contain only executable shell syntax, not test notes or natural-language descriptions.`,
			MaxContextChars: 20000,
		}, nil
	default:
		return Recipe{}, fmt.Errorf("unsupported coder op %q", op)
	}
}
