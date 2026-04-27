package tester

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
	case "test_data":
		return Recipe{
			Op:          "test_data",
			Description: "Generate test data for an assigned module or architecture document.",
			RequiredURIHints: []string{
				"/artifacts/modules/",
			},
			OutputKind:      "test_data",
			SystemPrompt:    "You are a DevFlow Tester Agent. You design focused test data and test cases for assigned work.",
			UserInstruction: "Generate test data for the provided task. This op is not implemented yet.",
			ResultFilename:  "test_data.md",
			MaxContextChars: 20000,
		}, nil
	case "test_code":
		return Recipe{
			Op:          "test_code",
			Description: "Run and analyze tests for a coder branch using module task and test data artifacts.",
			RequiredURIHints: []string{
				"/artifacts/modules/",
				"/artifacts/branches/",
			},
			OutputKind:      "test_reports",
			SystemPrompt:    "You are a DevFlow Tester Agent. You verify coder branches and report bugs without changing product code.",
			UserInstruction: "Run and analyze tests for the provided coder branch. This op is not implemented yet.",
			ResultFilename:  "test_report.md",
			MaxContextChars: 20000,
		}, nil
	default:
		return Recipe{}, fmt.Errorf("unsupported tester op %q", op)
	}
}

func errNotImplemented(op string) error {
	return fmt.Errorf("tester op %s is not implemented yet", op)
}
