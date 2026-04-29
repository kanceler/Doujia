package tester

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
)

type testDataInputs struct {
	testerTask  agentengine.ArtifactDocument
	moduleTask  agentengine.ArtifactDocument
	contractDoc agentengine.ArtifactDocument
	seedDoc     agentengine.ArtifactDocument
}

func (a *Agent) executeTestData(ctx context.Context, task core.TaskMetaData, recipe Recipe) core.TaskMetaData {
	if a.artifactStore == nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("tester artifact store is nil"))
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("resolve artifacts: %w", err))
	}
	inputs, err := findTestDataInputs(env.InputArtifacts, docs, recipe)
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}

	contract, err := parseModuleContract(inputs.contractDoc.Content)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("parse module_contract.json: %w", err))
	}
	seedBundle, err := common.ParseTestFileBundle([]byte(inputs.seedDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("parse seed_tests.json: %w", err))
	}
	if err := common.ValidateTestFileBundle(seedBundle, "seed_tests"); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("validate seed_tests.json: %w", err))
	}

	baseBundle, err := buildBaseFullTestBundle(inputs, contract, seedBundle)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("build full_test_files.json: %w", err))
	}
	cases := a.tryGenerateTestCasePlan(ctx, task, recipe, inputs, contract, seedBundle)
	fullBundle := renderFullTestBundleWithCases(baseBundle, contract, cases)
	if err := common.ValidateTestFileBundle(fullBundle, "full_test_files"); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("validate full_test_files.json: %w", err))
	}

	content, err := common.MarshalJSONArtifact(fullBundle)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("marshal full_test_files.json: %w", err))
	}
	output := agentengine.ModelOutput{
		Summary: "Generated full executable test bundle from local contract and seed tests with optional LLM case-plan enhancement.",
		ArtifactOutputs: []agentengine.ModelOutputFile{
			{
				Type:     "test_data",
				Filename: "full_test_files.json",
				Content:  string(content),
			},
		},
	}
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, recipe.OutputKind, output)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("write test data artifact: %w", err))
	}
	legacyFeedback, err := a.writeTestArtifact(ctx, task)
	if err == nil {
		uris = append(uris, legacyFeedback.ArtifactURIs...)
	}
	a.logStep(fmt.Sprintf("test_data success: outputs=%d llm_cases=%d", len(uris), len(cases)))
	return common.FeedbackFor(task, a.runID, a.agentID, uris)
}

func (a *Agent) tryGenerateTestCasePlan(
	ctx context.Context,
	task core.TaskMetaData,
	recipe Recipe,
	inputs testDataInputs,
	contract moduleContract,
	seedBundle common.TestFileBundle,
) []TestCase {
	if llm.IsNoop(a.llmClient) {
		return nil
	}

	docs := []agentengine.ArtifactDocument{inputs.testerTask, inputs.moduleTask, inputs.contractDoc, inputs.seedDoc}
	planCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	raw, err := a.llmClient.Complete(planCtx, buildPrompt(task, recipe, docs))
	if err != nil {
		a.logStep(fmt.Sprintf("test_data llm enhancement skipped: %v", err))
		return nil
	}
	plan, err := parseTestCasePlan(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("test_data llm enhancement skipped: %v", err))
		return nil
	}
	cases := validateTestCasePlan(plan, contract, seedBundle)
	if len(cases) == 0 && len(plan.Cases) > 0 {
		a.logStep("test_data llm enhancement skipped: no valid test cases")
	}
	return cases
}

func findTestDataInputs(refs []agentengine.ArtifactRef, docs []agentengine.ArtifactDocument, recipe Recipe) (testDataInputs, error) {
	docsByURI := make(map[string]agentengine.ArtifactDocument, len(docs))
	for _, doc := range docs {
		docsByURI[filepath.ToSlash(doc.URI)] = doc
	}

	var inputs testDataInputs
	for _, ref := range refs {
		uri := filepath.ToSlash(strings.TrimSpace(ref.URI))
		lower := strings.ToLower(uri)
		doc := docsByURI[uri]
		switch {
		case strings.Contains(lower, "/artifacts/tests/"):
			inputs.testerTask = doc
		case strings.Contains(lower, "/artifacts/modules/"):
			inputs.moduleTask = doc
		case strings.Contains(lower, "/artifacts/contracts/"):
			inputs.contractDoc = doc
		case strings.Contains(lower, "/artifacts/seed_tests/"):
			inputs.seedDoc = doc
		}
	}
	if strings.TrimSpace(inputs.testerTask.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/tests/", recipe.Op)
	}
	if strings.TrimSpace(inputs.moduleTask.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/modules/", recipe.Op)
	}
	if strings.TrimSpace(inputs.contractDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/contracts/", recipe.Op)
	}
	if strings.TrimSpace(inputs.seedDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/seed_tests/", recipe.Op)
	}
	return inputs, nil
}

func testDataFilename(agentID core.AgentID) string {
	name := sanitizeFilePart(string(agentID))
	if name == "" {
		name = "tester"
	}
	return name + "_test_data.md"
}

func sanitizeFilePart(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var builder strings.Builder
	lastSep := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastSep = false
		case r == '_' || r == '-' || r == ' ':
			if !lastSep {
				builder.WriteRune(rune('_'))
				lastSep = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}

func buildFallbackTestDataContent(task core.TaskMetaData, inputs testDataInputs) string {
	pairedCoder := extractMetadataValue(inputs.testerTask.Content, "paired_coder_agent")
	if pairedCoder == "" {
		pairedCoder = extractMetadataValue(inputs.moduleTask.Content, "coder_agent")
	}
	moduleID := extractMetadataValue(inputs.moduleTask.Content, "module_id")
	if moduleID == "" {
		moduleID = moduleNameFromArtifactURI(inputs.moduleTask.URI)
	}

	var builder strings.Builder
	builder.WriteString("# Test Data\n\n")
	builder.WriteString("## Document Basis\n\n")
	builder.WriteString("- tester_task_uri: ")
	builder.WriteString(inputs.testerTask.URI)
	builder.WriteString("\n- module_task_uri: ")
	builder.WriteString(inputs.moduleTask.URI)
	builder.WriteString("\n\n")
	builder.WriteString("## Pairing Metadata\n\n")
	builder.WriteString("- tester_agent: ")
	builder.WriteString(string(task.AgentID))
	builder.WriteString("\n- paired_coder_agent: ")
	builder.WriteString(pairedCoder)
	builder.WriteString("\n- module_id: ")
	builder.WriteString(moduleID)
	builder.WriteString("\n\n")
	builder.WriteString("## Unit Test Data\n\n")
	builder.WriteString("- happy_path: valid inputs that exercise the module responsibility described in the paired programmer task.\n")
	builder.WriteString("- state_transition: data that verifies expected before and after state for the module's core flow.\n")
	builder.WriteString("- contract_case: inputs and expected outputs for each public interface or integration contract described by the task.\n\n")
	builder.WriteString("## Boundary And Error Data\n\n")
	builder.WriteString("- empty_input: empty or missing required values should be handled explicitly.\n")
	builder.WriteString("- invalid_input: malformed values should produce deterministic validation behavior.\n")
	builder.WriteString("- duplicate_or_conflict: repeated entities or conflicting state should not corrupt module state.\n")
	builder.WriteString("- limit_case: minimum, maximum, and near-boundary values should be covered.\n\n")
	builder.WriteString("## Acceptance Matrix\n\n")
	builder.WriteString("| Case | Source | Expected Result |\n")
	builder.WriteString("| --- | --- | --- |\n")
	builder.WriteString("| happy_path | module task | module completes its primary responsibility |\n")
	builder.WriteString("| boundary | tester task | module handles boundary conditions predictably |\n")
	builder.WriteString("| error | paired tasks | module returns or records clear failure behavior |\n")
	return builder.String()
}

func extractMetadataValue(content, key string) string {
	prefix := "- " + key + ":"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return ""
}

func moduleNameFromArtifactURI(uri string) string {
	base := path.Base(filepath.ToSlash(uri))
	ext := path.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSuffix(base, "_task")
}
