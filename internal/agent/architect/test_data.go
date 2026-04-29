package architect

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
)

func (a *Agent) executeTestData(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		return a.executeTestDataFallback(task, nil)
	}
	docs, err := resolveArchitectDocs(ctx, a.artifactStore, task.ArtifactURIs)
	if err != nil {
		a.logStep(fmt.Sprintf("architect_test_data fallback: %v", err))
		return a.executeTestDataFallback(task, nil)
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_data", "architect_test_data.md")
	content := prependDocumentBasis(buildArchitectTestDataDocument(docs), task.ArtifactURIs)
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write architect test_data artifact: %w", err)
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
}

func (a *Agent) executeTestDataFallback(task core.TaskMetaData, docs []agentengine.ArtifactDocument) (core.TaskMetaData, error) {
	content := prependDocumentBasis(buildArchitectTestDataDocument(docs), task.ArtifactURIs)
	if a.artifactStore != nil {
		outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_data", "architect_test_data.md")
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "test_data", "architect_test_data.md"), content)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func buildArchitectTestDataDocument(docs []agentengine.ArtifactDocument) string {
	var builder strings.Builder
	builder.WriteString("# Architect Test Data\n\n")
	builder.WriteString("## Purpose\n\n")
	builder.WriteString("Architecture-level verification data generated from the current design and split-module outputs.\n\n")
	builder.WriteString("## Source Artifacts\n\n")
	if len(docs) == 0 {
		builder.WriteString("- No source artifacts were provided.\n")
	} else {
		for _, doc := range docs {
			builder.WriteString("- ")
			builder.WriteString(doc.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Architecture Verification Matrix\n\n")
	builder.WriteString("| Area | Data To Check | Expected Result |\n")
	builder.WriteString("| --- | --- | --- |\n")
	builder.WriteString("| module_boundaries | module task ownership and dependencies | responsibilities do not overlap unexpectedly |\n")
	builder.WriteString("| integration_flow | main branch plus coder branches | modules can be merged in declared order |\n")
	builder.WriteString("| state_contracts | shared state and interface notes | implementation preserves architecture contracts |\n")
	builder.WriteString("| failure_paths | missing input, invalid state, failed tests | reports are explicit and actionable |\n")
	return builder.String()
}
