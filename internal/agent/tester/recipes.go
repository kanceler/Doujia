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
			Description: "Generate a compact test case plan that extends local full executable tests for one paired programmer module task.",
			RequiredURIHints: []string{
				"/artifacts/tests/",
				"/artifacts/modules/",
				"/artifacts/contracts/",
				"/artifacts/seed_tests/",
			},
			OutputKind:   "test_data",
			SystemPrompt: "You are a DevFlow Tester Agent. You propose what to test for one paired programmer module task.",
			UserInstruction: `Return only a test_case_plan JSON object.
Do not return artifact_outputs.
Do not return full_test_files.json.
Do not include file paths.
Do not include shell commands.
Do not include source code.
Only propose what to test.
Allowed case types: boundary, error, state, integration.
target must be one public_api name from module_contract.json.`,
			OutputSchema:    `{"schema_version":1,"kind":"test_case_plan","module_id":"module01","cases":[{"name":"boundary_case","type":"boundary","target":"createModule","scenario":"what to test","expected":"expected result"}]}`,
			ResultFilename:  "full_test_files.json",
			MaxContextChars: 20000,
		}, nil
	case "test_code":
		return Recipe{
			Op:          "test_code",
			Description: "Use OpenCode to test a coder branch against the paired module task and generated unit test data.",
			RequiredURIHints: []string{
				"/artifacts/modules/",
				"/artifacts/branches/",
				"/artifacts/contracts/",
				"/artifacts/seed_tests/",
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
