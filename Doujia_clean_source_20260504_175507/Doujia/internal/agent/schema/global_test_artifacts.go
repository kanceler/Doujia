package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type GlobalAcceptanceScenario struct {
	Name string `json:"name"`
}

type GlobalAcceptanceTests struct {
	Kind         string                     `json:"kind"`
	Target       string                     `json:"target"`
	TargetCommit string                     `json:"target_commit,omitempty"`
	Scenarios    []GlobalAcceptanceScenario `json:"scenarios"`
}

type GlobalTestCommand struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	CwdFrom string `json:"cwd_from"`
}

type GlobalTestCommands struct {
	Kind     string              `json:"kind"`
	Commands []GlobalTestCommand `json:"commands"`
}

func (s GlobalAcceptanceTests) Validate(hasMergedMainBranch bool) error {
	if s.Kind != "global_acceptance_tests" {
		return fmt.Errorf("global_acceptance_tests.kind must equal global_acceptance_tests")
	}
	if s.Target != "merged_main_branch" {
		return fmt.Errorf("global_acceptance_tests.target must equal merged_main_branch")
	}
	if len(s.Scenarios) == 0 {
		return fmt.Errorf("global_acceptance_tests.scenarios must be a non-empty array")
	}
	for i, scenario := range s.Scenarios {
		if strings.TrimSpace(scenario.Name) == "" {
			return fmt.Errorf("global_acceptance_tests.scenarios[%d].name must be non-empty", i)
		}
	}
	if !hasMergedMainBranch && strings.TrimSpace(s.TargetCommit) != "" {
		return fmt.Errorf("target_commit must be absent or empty when merged_main_branch input is missing")
	}
	return nil
}

func (s GlobalTestCommands) Validate() error {
	if s.Kind != "global_test_commands" {
		return fmt.Errorf("global_test_commands.kind must equal global_test_commands")
	}
	if len(s.Commands) == 0 {
		return fmt.Errorf("global_test_commands.commands must be a non-empty array")
	}
	for i, cmd := range s.Commands {
		if strings.TrimSpace(cmd.Name) == "" {
			return fmt.Errorf("global_test_commands.commands[%d].name must be non-empty", i)
		}
		if strings.TrimSpace(cmd.Command) == "" {
			return fmt.Errorf("global_test_commands.commands[%d].command must be non-empty", i)
		}
		if strings.TrimSpace(cmd.CwdFrom) == "" {
			return fmt.Errorf("global_test_commands.commands[%d].cwd_from must be non-empty", i)
		}
	}
	return nil
}

func ReadGlobalAcceptanceTestsFile(path string) (GlobalAcceptanceTests, error) {
	var value GlobalAcceptanceTests
	return value, readGlobalArtifactJSONFile(path, &value)
}

func ReadGlobalTestCommandsFile(path string) (GlobalTestCommands, error) {
	var value GlobalTestCommands
	return value, readGlobalArtifactJSONFile(path, &value)
}

func GlobalTestArtifactsPromptContract() string {
	return strings.TrimSpace(`global_acceptance_tests.json 和 global_test_commands.json 必须是合法 JSON，并严格遵守以下结构约束：
- global_acceptance_tests.json:
  - "kind" 必须等于 "global_acceptance_tests"
  - "target" 必须等于 "merged_main_branch"
  - "scenarios" 必须是非空数组
  - 每个 scenario 至少包含非空字符串字段 "name"
  - 只有在输入里存在 merged_main_branch 时，才允许设置非空 "target_commit"
- global_test_commands.json:
  - "kind" 必须等于 "global_test_commands"
  - "commands" 必须是非空数组
  - 每个 command 项都必须包含非空字符串字段：
    - "name"
    - "command"
    - "cwd_from"
- 只输出 JSON 文本，不要输出注释，不要输出 Markdown 代码块。`)
}

func readGlobalArtifactJSONFile(path string, dest any) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(content, dest)
}
