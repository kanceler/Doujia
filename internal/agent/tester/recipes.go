package tester

import "fmt"

type Recipe struct {
	Op               string
	Description      string
	RequiredURIHints []string
	OutputKind       string
	SystemPrompt     string
	UserInstruction  string
	OutputSchema     string
	ResultFilename   string
	MaxContextChars  int
}

func GetRecipe(op string) (Recipe, error) {
	switch op {
	case "test_data":
		return Recipe{
			Op:          "test_data",
			Description: "Generate unit test data for the tester task and its paired programmer module task.",
			RequiredURIHints: []string{
				"/artifacts/tests/",
				"/artifacts/modules/",
			},
			OutputKind:   "test_data",
			SystemPrompt: "You are a DevFlow Tester Agent. You design focused unit test data for one paired programmer module task.",
			UserInstruction: `Generate test data from the provided tester task and paired programmer module task.
Focus on test data only; do not require git branch metadata and do not write executable tests.
The markdown content must include: document basis, pairing metadata, unit test data, boundary and error data, and an acceptance matrix.`,
			OutputSchema:    `{"summary":"short summary","artifact_outputs":[{"type":"test_data","filename":"test_data.md","content":"markdown test data"}],"control":[]}`,
			ResultFilename:  "test_data.md",
			MaxContextChars: 20000,
		}, nil
	case "test_code":
		return Recipe{
			Op:          "test_code",
			Description: "Use OpenCode to test a coder branch against the paired module task and generated unit test data.",
			RequiredURIHints: []string{
				"/artifacts/modules/",
				"/artifacts/branches/",
				"/artifacts/test_data/",
			},
			OutputKind:      "test_reports",
			SystemPrompt:    "You are a DevFlow Tester Agent. You verify coder branches and report bugs without changing product code.",
			UserInstruction: "Run tests in the provided worktree using the module task and test data. Do not commit or merge. Write .devflow/result.json with the required verification result.",
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
