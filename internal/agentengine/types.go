package agentengine

import "devflow/internal/core"

type TaskEnvelope struct {
	TaskID         core.TaskID
	ParentID       *core.TaskID
	AgentID        core.AgentID
	Op             string
	InputArtifacts []ArtifactRef
	Control        []core.Control
}

type ArtifactRef struct {
	URI  string
	Kind string
}

type Recipe struct {
	Op                 string
	Description        string
	OutputKind         string
	RequiredInputKinds []string
	SystemPrompt       string
	UserInstruction    string
	OutputSchema       string
	MaxContextChars    int
}

type ModelOutput struct {
	Summary         string            `json:"summary"`
	ArtifactOutputs []ModelOutputFile `json:"artifact_outputs"`
	Control         []core.Control    `json:"control,omitempty"`
}

type ModelOutputFile struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}
