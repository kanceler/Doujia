package common

import (
	"encoding/json"
	"fmt"
	"strings"
)

type BranchArtifact struct {
	SchemaVersion        int      `json:"schema_version"`
	Kind                 string   `json:"kind"`
	RunID                string   `json:"run_id"`
	RepoDir              string   `json:"repo_dir"`
	Branch               string   `json:"branch"`
	Commit               string   `json:"commit"`
	DeliveryProfile      string   `json:"delivery_profile,omitempty"`
	RequiredFiles        []string `json:"required_files,omitempty"`
	TestCommand          string   `json:"test_command,omitempty"`
	GlobalVerifyCommands []string `json:"global_verify_commands,omitempty"`
}

type CoderBranchArtifact struct {
	SchemaVersion            int      `json:"schema_version"`
	Kind                     string   `json:"kind"`
	RepoDir                  string   `json:"repo_dir"`
	BaseBranch               string   `json:"base_branch"`
	BaseCommit               string   `json:"base_commit"`
	Branch                   string   `json:"branch"`
	Commit                   string   `json:"commit"`
	Worktree                 string   `json:"worktree"`
	ModuleTaskURI            string   `json:"module_task_uri"`
	Summary                  string   `json:"summary"`
	ChangedFiles             []string `json:"changed_files,omitempty"`
	TestCommand              string   `json:"test_command,omitempty"`
	CodingAgentElapsedMillis int64    `json:"coding_agent_elapsed_ms"`
	TestElapsedMillis        int64    `json:"test_elapsed_ms"`
	CommitElapsedMillis      int64    `json:"commit_elapsed_ms"`
	TotalElapsedMillis       int64    `json:"total_elapsed_ms"`
}

func ParseBranchArtifact(content []byte) (BranchArtifact, error) {
	var artifact BranchArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		return BranchArtifact{}, fmt.Errorf("parse branch artifact json: %w", err)
	}
	if strings.TrimSpace(artifact.RepoDir) == "" {
		return BranchArtifact{}, fmt.Errorf("branch artifact repo_dir is required")
	}
	if strings.TrimSpace(artifact.Branch) == "" {
		return BranchArtifact{}, fmt.Errorf("branch artifact branch is required")
	}
	if strings.TrimSpace(artifact.Commit) == "" {
		return BranchArtifact{}, fmt.Errorf("branch artifact commit is required")
	}
	return artifact, nil
}

func ParseCoderBranchArtifact(content []byte) (CoderBranchArtifact, error) {
	var artifact CoderBranchArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		return CoderBranchArtifact{}, fmt.Errorf("parse coder branch artifact json: %w", err)
	}
	if strings.TrimSpace(artifact.RepoDir) == "" {
		return CoderBranchArtifact{}, fmt.Errorf("coder branch artifact repo_dir is required")
	}
	if strings.TrimSpace(artifact.Branch) == "" {
		return CoderBranchArtifact{}, fmt.Errorf("coder branch artifact branch is required")
	}
	if strings.TrimSpace(artifact.Commit) == "" {
		return CoderBranchArtifact{}, fmt.Errorf("coder branch artifact commit is required")
	}
	return artifact, nil
}

func MarshalJSONArtifact(value any) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
