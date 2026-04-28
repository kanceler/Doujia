package coder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
)

type implementationBrief struct {
	Content string
	Path    string
	Refs    string
}

type moduleContractSummary struct {
	ModuleName              string          `json:"module_name"`
	DeliveryProfile         string          `json:"delivery_profile"`
	RequiredFiles           []string        `json:"required_files"`
	AllowedFiles            []string        `json:"allowed_files"`
	ForbiddenFiles          []string        `json:"forbidden_files"`
	PublicAPI               json.RawMessage `json:"public_api"`
	EntryFiles              []string        `json:"entry_files"`
	ModuleSystem            string          `json:"module_system"`
	Notes                   json.RawMessage `json:"notes"`
	OfficialSeedTestCommand string          `json:"official_seed_test_command"`
}

func (a *Agent) prepareImplementationBrief(
	_ context.Context,
	worktreePath string,
	inputs writeCodeInputs,
	branchInfo common.BranchArtifact,
	seedBundle common.TestFileBundle,
) (implementationBrief, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return implementationBrief{}, fmt.Errorf("worktree path is required")
	}
	contract, err := parseModuleContractSummary(inputs.contractDoc.Content)
	if err != nil {
		return implementationBrief{}, err
	}
	sections := parseMarkdownSections(inputs.moduleDoc.Content)
	moduleName := firstNonEmpty(contract.ModuleName, markdownTitle(inputs.moduleDoc.Content), moduleNameFromURI(inputs.moduleDoc.URI))
	testCommand := firstNonEmpty(seedBundle.TestCommand, contract.OfficialSeedTestCommand, branchInfo.TestCommand)

	contextDir := filepath.Join(worktreePath, ".devflow", "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return implementationBrief{}, fmt.Errorf("create context dir: %w", err)
	}

	refs := buildContextRefs(a.runRoot, inputs)
	refsContent, err := common.MarshalJSONArtifact(refs)
	if err != nil {
		return implementationBrief{}, fmt.Errorf("marshal context refs: %w", err)
	}
	refsPath := filepath.Join(contextDir, "context_refs.json")
	if err := os.WriteFile(refsPath, refsContent, 0o644); err != nil {
		return implementationBrief{}, fmt.Errorf("write context_refs.json: %w", err)
	}

	content := buildImplementationBriefContent(moduleName, sections, contract, seedBundle, testCommand)
	briefPath := filepath.Join(contextDir, "implementation_brief.md")
	if err := os.WriteFile(briefPath, []byte(content), 0o644); err != nil {
		return implementationBrief{}, fmt.Errorf("write implementation_brief.md: %w", err)
	}
	return implementationBrief{Content: content, Path: briefPath, Refs: refsPath}, nil
}

func (a *Agent) prepareDebugBrief(
	_ context.Context,
	worktreePath string,
	inputs debugInputs,
	branchInfo common.CoderBranchArtifact,
	testCommand string,
) (implementationBrief, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return implementationBrief{}, fmt.Errorf("worktree path is required")
	}
	contract, err := parseModuleContractSummary(inputs.contractDoc.Content)
	if err != nil {
		return implementationBrief{}, err
	}
	moduleName := firstNonEmpty(contract.ModuleName, markdownTitle(inputs.moduleDoc.Content), moduleNameFromURI(inputs.moduleDoc.URI))
	testCommand = firstNonEmpty(testCommand, branchInfo.TestCommand, contract.OfficialSeedTestCommand)

	contextDir := filepath.Join(worktreePath, ".devflow", "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return implementationBrief{}, fmt.Errorf("create context dir: %w", err)
	}

	sources := map[string]map[string]string{}
	if err := writeDebugContextDoc(contextDir, sources, "module_task", "module_task.md", inputs.moduleDoc); err != nil {
		return implementationBrief{}, err
	}
	if err := writeDebugContextDoc(contextDir, sources, "coder_branch", "coder_branch.json", inputs.branchDoc); err != nil {
		return implementationBrief{}, err
	}
	if err := writeDebugContextDoc(contextDir, sources, "module_contract", "module_contract.json", inputs.contractDoc); err != nil {
		return implementationBrief{}, err
	}
	if err := writeDebugContextDoc(contextDir, sources, "seed_tests", "seed_tests.json", inputs.seedDoc); err != nil {
		return implementationBrief{}, err
	}
	if strings.TrimSpace(inputs.fullTestsDoc.URI) != "" || strings.TrimSpace(inputs.fullTestsDoc.Content) != "" {
		if err := writeDebugContextDoc(contextDir, sources, "full_tests", "full_test_files.json", inputs.fullTestsDoc); err != nil {
			return implementationBrief{}, err
		}
	}
	if err := writeDebugContextDoc(contextDir, sources, "failure_report", "failure_report.md", inputs.failureDoc); err != nil {
		return implementationBrief{}, err
	}

	refs := map[string]any{
		"schema_version": 1,
		"kind":           "coder_debug_refs",
		"sources":        sources,
	}
	refsContent, err := common.MarshalJSONArtifact(refs)
	if err != nil {
		return implementationBrief{}, fmt.Errorf("marshal debug refs: %w", err)
	}
	refsPath := filepath.Join(contextDir, "debug_refs.json")
	if err := os.WriteFile(refsPath, refsContent, 0o644); err != nil {
		return implementationBrief{}, fmt.Errorf("write debug_refs.json: %w", err)
	}

	content := buildDebugBriefContent(moduleName, branchInfo, contract, inputs.failureDoc.Content, testCommand)
	briefPath := filepath.Join(contextDir, "debug_brief.md")
	if err := os.WriteFile(briefPath, []byte(content), 0o644); err != nil {
		return implementationBrief{}, fmt.Errorf("write debug_brief.md: %w", err)
	}
	return implementationBrief{Content: content, Path: briefPath, Refs: refsPath}, nil
}

func writeDebugContextDoc(contextDir string, sources map[string]map[string]string, key string, filename string, doc agentengine.ArtifactDocument) error {
	path := filepath.Join(contextDir, filename)
	if err := os.WriteFile(path, []byte(doc.Content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	sources[key] = map[string]string{
		"artifact_uri": doc.URI,
		"path":         path,
	}
	return nil
}

func parseModuleContractSummary(content string) (moduleContractSummary, error) {
	var contract moduleContractSummary
	if err := json.Unmarshal([]byte(content), &contract); err != nil {
		return moduleContractSummary{}, fmt.Errorf("parse module contract json: %w", err)
	}
	return contract, nil
}

func buildDebugBriefContent(moduleName string, branchInfo common.CoderBranchArtifact, contract moduleContractSummary, failureReport string, testCommand string) string {
	var builder strings.Builder
	builder.WriteString("# Debug Brief\n\n")
	builder.WriteString("## Repair Target\n\n")
	writeBriefValue(&builder, "module_name", moduleName)
	writeBriefValue(&builder, "branch", branchInfo.Branch)
	writeBriefValue(&builder, "commit", branchInfo.Commit)
	writeBriefValue(&builder, "worktree", branchInfo.Worktree)
	writeBriefJSON(&builder, "changed_files", branchInfo.ChangedFiles)

	builder.WriteString("## Branch Summary\n\n")
	summary := strings.TrimSpace(branchInfo.Summary)
	if summary == "" {
		summary = "MISSING"
	}
	builder.WriteString(summary)
	builder.WriteString("\n\n")

	builder.WriteString("## Failure Context\n\n")
	writeBriefValue(&builder, "test_command", testCommand)
	writeBriefValue(&builder, "failure_report", failureReport)

	builder.WriteString("## Contract Boundaries\n\n")
	writeBriefJSON(&builder, "allowed_files", contract.AllowedFiles)
	writeBriefJSON(&builder, "forbidden_files", contract.ForbiddenFiles)
	writeBriefRawJSON(&builder, "public_api", contract.PublicAPI)
	writeBriefValue(&builder, "delivery_profile", contract.DeliveryProfile)
	writeBriefJSON(&builder, "required_files", contract.RequiredFiles)

	builder.WriteString("## Verification\n\n")
	writeBriefValue(&builder, "required_test_command", testCommand)

	builder.WriteString("## Detail References\n\n")
	builder.WriteString(".devflow/context/debug_refs.json\n")
	return builder.String()
}

func buildImplementationBriefContent(moduleName string, sections map[string]string, contract moduleContractSummary, seedBundle common.TestFileBundle, testCommand string) string {
	var builder strings.Builder
	builder.WriteString("# Implementation Brief\n\n")
	writeBriefValue(&builder, "module_name", moduleName)
	writeBriefValue(&builder, "goal", sections["goal"])
	writeBriefValue(&builder, "scope", sections["module scope"])
	writeBriefValue(&builder, "deliverables", sections["deliverables"])
	writeBriefJSON(&builder, "allowed_files", contract.AllowedFiles)
	writeBriefJSON(&builder, "must_not_modify", contract.ForbiddenFiles)
	writeBriefRawJSON(&builder, "required_api", contract.PublicAPI)
	writeBriefInputOutputContract(&builder, contract)
	writeBriefValue(&builder, "test_focus", seedTestFocus(seedBundle))
	writeBriefValue(&builder, "test_command", testCommand)
	if strings.TrimSpace(contract.DeliveryProfile) != "" || len(contract.RequiredFiles) > 0 {
		builder.WriteString("## Delivery Requirements\n\n")
		writeBriefValue(&builder, "delivery_profile", contract.DeliveryProfile)
		writeBriefJSON(&builder, "required_files", contract.RequiredFiles)
		if contract.DeliveryProfile == "frontend_web" {
			builder.WriteString("- frontend_web must produce a complete browser app entry.\n")
			builder.WriteString("- index.html must load code from src.\n")
			builder.WriteString("- README.md must contain real run instructions.\n")
			builder.WriteString("- The result must not be only module functions.\n\n")
		}
	}
	builder.WriteString("## Detail References\n\n")
	builder.WriteString(".devflow/context/context_refs.json\n")
	return builder.String()
}

func writeBriefInputOutputContract(builder *strings.Builder, contract moduleContractSummary) {
	payload := map[string]any{
		"public_api":    rawJSONOrMissing(contract.PublicAPI),
		"entry_files":   contract.EntryFiles,
		"module_system": firstNonEmpty(contract.ModuleSystem, "MISSING"),
		"notes":         rawJSONOrMissing(contract.Notes),
	}
	writeBriefJSON(builder, "input_output_contract", payload)
}

func writeBriefValue(builder *strings.Builder, title string, value string) {
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	value = strings.TrimSpace(value)
	if value == "" {
		value = "MISSING"
	}
	builder.WriteString(value)
	builder.WriteString("\n\n")
}

func writeBriefJSON(builder *strings.Builder, title string, value any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil || string(raw) == "null" {
		raw = []byte("[]")
	}
	writeBriefValue(builder, title, string(raw))
}

func writeBriefRawJSON(builder *strings.Builder, title string, raw json.RawMessage) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		value = "MISSING"
	}
	writeBriefValue(builder, title, value)
}

func rawJSONOrMissing(raw json.RawMessage) any {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return "MISSING"
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return value
	}
	return decoded
}

func seedTestFocus(bundle common.TestFileBundle) string {
	if len(bundle.TestFiles) == 0 {
		return "MISSING"
	}
	paths := make([]string, 0, len(bundle.TestFiles))
	for _, file := range bundle.TestFiles {
		if path := strings.TrimSpace(file.Path); path != "" {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return "MISSING"
	}
	return "Seed tests materialized at: " + strings.Join(paths, ", ")
}

func parseMarkdownSections(content string) map[string]string {
	sections := make(map[string]string)
	var current string
	var lines []string
	flush := func() {
		if current != "" {
			sections[strings.ToLower(current)] = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			lines = nil
			continue
		}
		if current != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return sections
}

func markdownTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func buildContextRefs(runRoot string, inputs writeCodeInputs) map[string]any {
	return map[string]any{
		"schema_version": 1,
		"kind":           "coder_context_refs",
		"sources": map[string]any{
			"module_task":     contextRefFor(runRoot, inputs.moduleDoc),
			"main_branch":     contextRefFor(runRoot, inputs.branchDoc),
			"module_contract": contextRefFor(runRoot, inputs.contractDoc),
			"seed_tests":      contextRefFor(runRoot, inputs.seedDoc),
		},
	}
}

func contextRefFor(runRoot string, doc agentengine.ArtifactDocument) map[string]string {
	return map[string]string{
		"artifact_uri": doc.URI,
		"path":         artifactURIPath(runRoot, doc.URI),
	}
}

func artifactURIPath(runRoot string, uri string) string {
	runRoot = strings.TrimSpace(runRoot)
	uri = strings.TrimSpace(uri)
	if runRoot == "" || uri == "" || filepath.IsAbs(uri) {
		return ""
	}
	normalized := filepath.ToSlash(uri)
	parts := strings.Split(normalized, "/")
	if len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" {
		normalized = strings.Join(parts[2:], "/")
	}
	return filepath.Clean(filepath.Join(runRoot, filepath.FromSlash(normalized)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
