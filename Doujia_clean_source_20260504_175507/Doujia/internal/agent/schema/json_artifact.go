package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"devflow/internal/agent/core"
)

func ValidateJSONArtifactFile(logicalKey string, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read JSON artifact %q: %w", logicalKey, err)
	}
	return ValidateJSONArtifactBytes(logicalKey, body)
}

func ValidateJSONArtifactBytes(logicalKey string, body []byte) error {
	if hasUTF8BOM(body) {
		return fmt.Errorf("%s must be UTF-8 without BOM", logicalKey)
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Errorf("%s must be non-empty JSON", logicalKey)
	}
	if bytes.HasPrefix(trimmed, []byte("```")) {
		return fmt.Errorf("%s must be raw JSON without Markdown fences", logicalKey)
	}

	switch {
	case logicalKey == core.LKCoderBranch:
		var value CoderBranch
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case logicalKey == core.LKMergedMainBranch:
		var value MergedMainBranch
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case logicalKey == core.LKFullTestFiles:
		var value FullTestFiles
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case logicalKey == core.LKModuleTestReport:
		var value ModuleTestReport
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case logicalKey == core.LKGlobalAcceptanceTests:
		var value GlobalAcceptanceTests
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate(true)
	case logicalKey == core.LKGlobalTestCommands:
		var value GlobalTestCommands
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case logicalKey == core.LKGlobalTestReport:
		var value GlobalTestReport
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case logicalKey == core.LKModuleSpecs:
		var value ModuleSpecs
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	case isModuleSpecLogicalKey(logicalKey):
		var value ModuleSpec
		if err := unmarshalStrictJSON(logicalKey, trimmed, &value); err != nil {
			return err
		}
		return value.Validate()
	default:
		var value any
		return unmarshalStrictJSON(logicalKey, trimmed, &value)
	}
}

func JSONArtifactExample(logicalKey string) string {
	switch {
	case logicalKey == core.LKMergedMainBranch:
		return `{
  "kind": "merged_main_branch",
  "result": "kok",
  "base_branch": "main",
  "merged_commit": "<required merged commit hash>",
  "modules": [
    {
      "module_id": "module01",
      "branch": "module01-work",
      "commit": "<module commit hash>",
      "base_branch": "main",
      "base_commit": "<base commit hash>"
    }
  ]
}`
	case logicalKey == core.LKFullTestFiles:
		return `{
  "kind": "full_test_files",
  "test_command": "npm test",
  "files": [
    {
      "path": "tests/example.test.js",
      "content": "test content"
    }
  ]
}`
	case logicalKey == core.LKGlobalAcceptanceTests:
		return `{
  "kind": "global_acceptance_tests",
  "target": "merged_main_branch",
  "target_commit": "<merged commit hash when available>",
  "scenarios": [
    {
      "name": "smoke"
    }
  ]
}`
	case logicalKey == core.LKGlobalTestCommands:
		return `{
  "kind": "global_test_commands",
  "commands": [
    {
      "name": "smoke",
      "command": "npm test",
      "cwd_from": "repo_dir"
    }
  ]
}`
	case logicalKey == core.LKModuleTestReport:
		return `{
  "kind": "module_test_report",
  "result": "kok",
  "test_passed": true,
  "tested_commit": "<module commit hash>",
  "commands": []
}`
	case logicalKey == core.LKGlobalTestReport:
		return `{
  "kind": "global_test_report",
  "result": "kok",
  "test_passed": true,
  "tested_branch": "main",
  "tested_commit": "<merged commit hash>",
  "commands": []
}`
	case logicalKey == core.LKCoderBranch:
		return `{
  "kind": "coder_branch",
  "module_id": "module01",
  "container_id": "container",
  "repo_dir": "/workspace/repo",
  "base_branch": "main",
  "base_commit": "<base commit hash>",
  "branch": "module01-work",
  "commit": "<new commit hash>",
  "worktree": "/workspace/worktrees/module01",
  "changed_files": [],
  "test_command": "npm test",
  "result": "kok",
  "test_passed": true
}`
	case logicalKey == core.LKModuleSpecs:
		return `{
  "modules": [
    {
      "module_id": "module01",
      "module_name": "frontend",
      "module_role": "frontend",
      "implementation_role": "front",
      "branch_name": "module01-work",
      "worktree_dir": "/workspace/worktrees/module01",
      "owned_paths": ["miniprogram/**"],
      "test_command": "npm test",
      "complexity": "high"
    }
  ]
}`
	case isModuleSpecLogicalKey(logicalKey):
		return `{
  "module_id": "module01",
  "module_name": "frontend",
  "module_role": "frontend",
  "implementation_role": "front",
  "branch_name": "module01-work",
  "worktree_dir": "/workspace/worktrees/module01",
  "owned_paths": ["miniprogram/**"],
  "test_command": "npm test",
  "complexity": "high"
}`
	default:
		return ""
	}
}

func JSONArtifactContract(logicalKey string) string {
	switch {
	case logicalKey == core.LKCoderBranch:
		return `required fields: kind, module_id, container_id, repo_dir, base_branch, base_commit, branch, commit, worktree, changed_files, test_command, result, test_passed`
	case logicalKey == core.LKMergedMainBranch:
		return `required fields: kind, result, base_branch; when result is kok, merged_commit is required`
	case logicalKey == core.LKFullTestFiles:
		return `required fields: kind, test_command, files`
	case logicalKey == core.LKModuleTestReport:
		return `required fields: kind, result, test_passed`
	case logicalKey == core.LKGlobalAcceptanceTests:
		return `required fields: kind, target, scenarios; kind must equal global_acceptance_tests and target must equal merged_main_branch`
	case logicalKey == core.LKGlobalTestCommands:
		return `required fields: kind, commands; commands must be a non-empty array`
	case logicalKey == core.LKGlobalTestReport:
		return `required fields: kind, result, test_passed, tested_commit`
	case logicalKey == core.LKModuleSpecs:
		return `required fields: modules; each module must be a complete module_spec object`
	case isModuleSpecLogicalKey(logicalKey):
		return `required fields: module_id, module_name, implementation_role, worktree_dir, owned_paths, test_command, complexity`
	default:
		return `must be raw valid JSON`
	}
}

type CoderBranch struct {
	Kind         string   `json:"kind"`
	ModuleID     string   `json:"module_id"`
	ContainerID  string   `json:"container_id"`
	RepoDir      string   `json:"repo_dir"`
	BaseBranch   string   `json:"base_branch"`
	BaseCommit   string   `json:"base_commit"`
	Branch       string   `json:"branch"`
	Commit       string   `json:"commit"`
	Worktree     string   `json:"worktree"`
	ChangedFiles []string `json:"changed_files"`
	TestCommand  string   `json:"test_command"`
	Result       string   `json:"result"`
	TestPassed   *bool    `json:"test_passed"`
}

func (s CoderBranch) Validate() error {
	for field, value := range map[string]string{
		"module_id":    s.ModuleID,
		"container_id": s.ContainerID,
		"repo_dir":     s.RepoDir,
		"base_branch":  s.BaseBranch,
		"base_commit":  s.BaseCommit,
		"branch":       s.Branch,
		"commit":       s.Commit,
		"worktree":     s.Worktree,
		"test_command": s.TestCommand,
		"result":       s.Result,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("coder_branch.%s must be a non-empty string", field)
		}
	}
	if s.TestPassed == nil {
		return fmt.Errorf("coder_branch.test_passed must be a boolean")
	}
	for i, file := range s.ChangedFiles {
		if strings.TrimSpace(file) == "" {
			return fmt.Errorf("coder_branch.changed_files[%d] must be non-empty", i)
		}
	}
	return nil
}

type MergedMainBranch struct {
	Kind         string               `json:"kind"`
	Result       string               `json:"result"`
	BaseBranch   string               `json:"base_branch"`
	MergedCommit string               `json:"merged_commit"`
	Modules      []MergedModuleCommit `json:"modules,omitempty"`
}

type MergedModuleCommit struct {
	ModuleID   string `json:"module_id"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	BaseBranch string `json:"base_branch"`
	BaseCommit string `json:"base_commit"`
}

func (s MergedMainBranch) Validate() error {
	if strings.TrimSpace(s.Result) == "" {
		return fmt.Errorf("merged_main_branch.result must be a non-empty string")
	}
	if strings.TrimSpace(s.BaseBranch) == "" {
		return fmt.Errorf("merged_main_branch.base_branch must be a non-empty string")
	}
	if strings.TrimSpace(s.Result) == "kok" && strings.TrimSpace(s.MergedCommit) == "" {
		return fmt.Errorf("merged_main_branch.merged_commit must be a non-empty string when result is kok")
	}
	return nil
}

type FullTestFiles struct {
	Kind        string          `json:"kind"`
	TestCommand string          `json:"test_command"`
	Files       json.RawMessage `json:"files"`
}

type TestFileSpec struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s FullTestFiles) Validate() error {
	if strings.TrimSpace(s.TestCommand) == "" {
		return fmt.Errorf("full_test_files.test_command must be a non-empty string")
	}
	files := bytes.TrimSpace(s.Files)
	if len(files) == 0 || bytes.Equal(files, []byte("null")) {
		return fmt.Errorf("full_test_files.files must be an array")
	}
	switch files[0] {
	case '[':
		var values []TestFileSpec
		if err := json.Unmarshal(files, &values); err != nil {
			return fmt.Errorf("full_test_files.files must be an array: %w", err)
		}
		for i, file := range values {
			if strings.TrimSpace(file.Path) == "" {
				return fmt.Errorf("full_test_files.files[%d].path must be non-empty", i)
			}
		}
	case '{':
		var values map[string]string
		if err := json.Unmarshal(files, &values); err != nil {
			return fmt.Errorf("full_test_files.files must be an object map: %w", err)
		}
		for key := range values {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("full_test_files.files must not contain empty paths")
			}
		}
	default:
		return fmt.Errorf("full_test_files.files must be an array or object")
	}
	return nil
}

type ModuleTestReport struct {
	Kind         string         `json:"kind"`
	Result       string         `json:"result"`
	TestPassed   *bool          `json:"test_passed"`
	TestedCommit string         `json:"tested_commit,omitempty"`
	Commands     []CommandEntry `json:"commands,omitempty"`
}

type CommandEntry struct {
	Name     string `json:"name,omitempty"`
	Command  string `json:"command,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

func (s ModuleTestReport) Validate() error {
	if strings.TrimSpace(s.Result) == "" {
		return fmt.Errorf("module_test_report.result must be a non-empty string")
	}
	if s.TestPassed == nil {
		return fmt.Errorf("module_test_report.test_passed must be a boolean")
	}
	return nil
}

type GlobalTestReport struct {
	Kind         string         `json:"kind"`
	Result       string         `json:"result"`
	TestPassed   *bool          `json:"test_passed"`
	TestedBranch string         `json:"tested_branch,omitempty"`
	TestedCommit string         `json:"tested_commit"`
	Commands     []CommandEntry `json:"commands,omitempty"`
}

func (s GlobalTestReport) Validate() error {
	if strings.TrimSpace(s.Result) == "" {
		return fmt.Errorf("global_test_report.result must be a non-empty string")
	}
	if s.TestPassed == nil {
		return fmt.Errorf("global_test_report.test_passed must be a boolean")
	}
	if strings.TrimSpace(s.TestedCommit) == "" {
		return fmt.Errorf("global_test_report.tested_commit must be a non-empty string")
	}
	return nil
}

func unmarshalStrictJSON(logicalKey string, body []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(dest); err != nil {
		return fmt.Errorf("%s must be valid JSON: %w", logicalKey, err)
	}
	if decoder.More() {
		return fmt.Errorf("%s must contain exactly one JSON value", logicalKey)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("%s must contain exactly one JSON value", logicalKey)
	}
	return nil
}

func hasUTF8BOM(body []byte) bool {
	return len(body) >= 3 && body[0] == 0xef && body[1] == 0xbb && body[2] == 0xbf
}

func isModuleSpecLogicalKey(logicalKey string) bool {
	return strings.HasPrefix(logicalKey, "module") && strings.HasSuffix(logicalKey, "_spec")
}
