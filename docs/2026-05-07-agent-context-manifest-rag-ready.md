# Agent Context Manifest Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an agent input context manifest so prompts describe what each artifact is for, let LLM agents choose which artifacts to read through tools, and leave a clean path for future RAG/search integration.

**Architecture:** Keep the current `artifact_read(logical_key)` permission model, but add a structured manifest layer that explains each readable artifact's role, purpose, priority, and usage hint. The executor will enrich each `AgentInputBundle` with this manifest before compiling prompts and before scoped handlers run; prompts and a new `artifact_list` tool will expose the manifest without exposing filesystem paths.

**Tech Stack:** Go 1.25, existing `internal/agent` runtime, plugin registry handlers, tool loop, prompt compiler, unit tests with `go test`.

---

## Current Code Map

- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/core/types.go`
  Owns shared agent data contracts. Add manifest entry types to `AgentInputBundle`.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/manifest/manifest.go`
  New focused package. Builds context manifest entries from `Task`, `AgentInputBundle`, and `OpSpec`.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/manifest/manifest_test.go`
  New unit tests for manifest enrichment, required/optional classification, repair mode rules, duplicate handling, and scenario summary.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/executor/executor.go`
  Enrich the bundle after `OpSpec` resolution and before prompt compilation and scoped handler creation.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/prompt/compiler.go`
  Replace the flat logical-key list with an input context manifest section and a scenario summary.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/prompt/compiler_test.go`
  Update prompt tests so they assert semantic artifact descriptions, read strategy, scenario summary, and no path leakage.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_list.go`
  New handler/tool. Returns the manifest as structured JSON so future agents can inspect the input map before reading.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_list_test.go`
  New handler tests for path-free output, manifest fields, and scoped bundle fallback.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_read.go`
  Include manifest metadata in `artifact_read` responses without changing allowed keys or exposing new paths.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/bootstrap/plugins.go`
  Register the new builtin `artifact_list` handler.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/*`
  Add `artifact_list` to selected op specs in a safe first wave: PM, CEO, architect plan/split/test-data/test-code, coder/front write-code, tester test-data/test-code.
- `Doujia_clean_source_20260504_175507/Doujia/internal/agent/bootstrap/plugin_pack_builtin.go`
  No logic change expected after handler registration; builtin pack export should include the new handler automatically.

---

## Task 1: Add Manifest Types To Core Contracts

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/core/types.go`

- [ ] **Step 1: Add manifest fields to `AgentInputBundle`**

Insert `ContextManifest` after `PreviousOutputs`:

```go
type AgentInputBundle struct {
	InputDir        string                 `json:"input_dir"`
	OutputDir       string                 `json:"output_dir"`
	OutputURIBase   string                 `json:"output_uri_base,omitempty"`
	Inputs          []InputArtifact        `json:"inputs"`
	PreviousOutputs []PreviousOutputRef    `json:"previous_outputs,omitempty"`
	ContextManifest []ArtifactContextEntry `json:"context_manifest,omitempty"`
	Bags            []AgentInputBag        `json:"bags,omitempty"`
	Versions        []AgentInputVersion    `json:"versions,omitempty"`
}
```

- [ ] **Step 2: Add `ArtifactContextEntry` type**

Place it after `PreviousOutputRef`:

```go
type ArtifactContextEntry struct {
	LogicalKey        string   `json:"logical_key"`
	SourceKind        string   `json:"source_kind,omitempty"`
	SourceRole        string   `json:"source_role,omitempty"`
	Purpose           string   `json:"purpose,omitempty"`
	UsageHint         string   `json:"usage_hint,omitempty"`
	ReadPriority      string   `json:"read_priority,omitempty"`
	RequiredForTask   bool     `json:"required_for_task,omitempty"`
	ArtifactVersionID string   `json:"artifact_version_id,omitempty"`
	LogicalArtifactID string   `json:"logical_artifact_id,omitempty"`
	ObjectType        string   `json:"object_type,omitempty"`
	ContentType       string   `json:"content_type,omitempty"`
	Encoding          string   `json:"encoding,omitempty"`
	RetrievalQuery    string   `json:"retrieval_query,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}
```

- [ ] **Step 3: Run core package compile check**

Run:

```powershell
go test ./internal/agent/core
```

Expected: package compiles; if Go reports no test files, that is acceptable.

- [ ] **Step 4: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/core/types.go
git commit -m "feat(agent): add artifact context manifest types"
```

---

## Task 2: Build Manifest Enrichment Package

**Files:**
- Create: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/manifest/manifest.go`
- Create: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/manifest/manifest_test.go`

- [ ] **Step 1: Write failing tests for manifest enrichment**

Create `manifest_test.go`:

```go
package manifest

import (
	"testing"

	"devflow/internal/agent/core"
)

func TestEnrichBuildsSemanticManifestFromOpSpec(t *testing.T) {
	t.Parallel()

	bundle := core.AgentInputBundle{
		Inputs: []core.InputArtifact{
			{
				LogicalKey:        core.LKRequirement,
				ArtifactVersionID: "ver-requirement",
				LogicalArtifactID: "logical-requirement",
				ObjectType:        "markdown",
			},
			{
				LogicalKey:        core.LKArchitecturePlan,
				ArtifactVersionID: "ver-architecture",
				LogicalArtifactID: "logical-architecture",
				ObjectType:        "markdown",
			},
		},
	}
	spec := core.OpSpec{
		Role: "pm",
		Op:   "write_plan",
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKRequirement, Description: "Product requirement written by CEO."},
		},
		BaseOptionalInputs: []core.InputRequirement{
			{LogicalKey: core.LKArchitecturePlan, Description: "Architecture context for reference only."},
		},
	}

	enriched := Enrich(core.Task{Role: "pm", Op: "write_plan"}, bundle, spec)

	if len(enriched.ContextManifest) != 2 {
		t.Fatalf("manifest count = %d, want 2", len(enriched.ContextManifest))
	}
	required := findEntry(enriched.ContextManifest, core.LKRequirement)
	if required == nil {
		t.Fatalf("missing requirement manifest entry")
	}
	if !required.RequiredForTask {
		t.Fatalf("requirement should be required")
	}
	if required.ReadPriority != ReadPriorityRequired {
		t.Fatalf("requirement priority = %q, want %q", required.ReadPriority, ReadPriorityRequired)
	}
	if required.Purpose != "Product requirement written by CEO." {
		t.Fatalf("requirement purpose = %q", required.Purpose)
	}
	if required.SourceRole != "ceo" {
		t.Fatalf("requirement source role = %q, want ceo", required.SourceRole)
	}
	optional := findEntry(enriched.ContextManifest, core.LKArchitecturePlan)
	if optional == nil {
		t.Fatalf("missing architecture manifest entry")
	}
	if optional.RequiredForTask {
		t.Fatalf("architecture should be optional")
	}
	if optional.ReadPriority != ReadPriorityOptional {
		t.Fatalf("architecture priority = %q, want %q", optional.ReadPriority, ReadPriorityOptional)
	}
}

func TestEnrichIncludesRepairModeInputsAsRequired(t *testing.T) {
	t.Parallel()

	bundle := core.AgentInputBundle{
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKRepairInstruction, ArtifactVersionID: "ver-repair"},
		},
		PreviousOutputs: []core.PreviousOutputRef{
			{LogicalKey: core.LKModuleTestReport, ArtifactVersionID: "ver-previous"},
		},
	}
	spec := core.OpSpec{
		Role: "tester",
		Op:   "test_code",
		ModeInputRules: map[string]core.ModeInputRule{
			core.ExecutionModeRepair: {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction, Description: "Repair instruction for this retry."},
				},
				ExtraOptionalInputs: []core.InputRequirement{
					{LogicalKey: core.LKModuleTestReport, Description: "Previous test report to reuse or repair."},
				},
			},
		},
	}

	enriched := Enrich(core.Task{Role: "tester", Op: "test_code", ExecutionMode: core.ExecutionModeRepair}, bundle, spec)

	repair := findEntry(enriched.ContextManifest, core.LKRepairInstruction)
	if repair == nil || !repair.RequiredForTask || repair.ReadPriority != ReadPriorityRequired {
		t.Fatalf("repair entry = %#v, want required repair input", repair)
	}
	previous := findEntry(enriched.ContextManifest, core.LKModuleTestReport)
	if previous == nil {
		t.Fatalf("missing previous output entry")
	}
	if previous.SourceKind != SourceKindPreviousOutput {
		t.Fatalf("previous source kind = %q", previous.SourceKind)
	}
}

func TestScenarioSummaryNamesRoleOpAndReadStrategy(t *testing.T) {
	t.Parallel()

	bundle := Enrich(
		core.Task{Role: "coder", Op: "write_code"},
		core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKModuleSpec},
				{LogicalKey: core.LKModuleContract},
			},
		},
		core.OpSpec{
			BaseRequiredInputs: []core.InputRequirement{
				{LogicalKey: core.LKModuleSpec, Description: "Module implementation boundaries."},
				{LogicalKey: core.LKModuleContract, Description: "Acceptance constraints."},
			},
		},
	)

	summary := ScenarioSummary(core.Task{Role: "coder", Op: "write_code"}, bundle)
	for _, want := range []string{"coder", "write_code", core.LKModuleSpec, core.LKModuleContract, "artifact_read"} {
		if !contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}

func findEntry(entries []core.ArtifactContextEntry, key string) *core.ArtifactContextEntry {
	for i := range entries {
		if entries[i].LogicalKey == key {
			return &entries[i]
		}
	}
	return nil
}

func contains(value, want string) bool {
	return len(value) >= len(want) && (value == want || containsAt(value, want))
}

func containsAt(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```powershell
cd D:\Doujia5.0\Doujia-main-workspace\Doujia_clean_source_20260504_175507\Doujia
go test ./internal/agent/manifest
```

Expected: FAIL because `Enrich`, constants, and `ScenarioSummary` are undefined.

- [ ] **Step 3: Implement manifest builder**

Create `manifest.go`:

```go
package manifest

import (
	"fmt"
	"strings"

	"devflow/internal/agent/core"
)

const (
	SourceKindInput          = "input"
	SourceKindPreviousOutput = "previous_output"

	ReadPriorityRequired = "required"
	ReadPriorityOptional = "optional"
	ReadPriorityContext  = "context"
)

func Enrich(task core.Task, bundle core.AgentInputBundle, spec core.OpSpec) core.AgentInputBundle {
	required, optional := requirementMaps(task, spec)
	entries := make([]core.ArtifactContextEntry, 0, len(bundle.Inputs)+len(bundle.PreviousOutputs))
	seen := map[string]int{}

	add := func(entry core.ArtifactContextEntry) {
		key := strings.TrimSpace(entry.LogicalKey)
		if key == "" {
			return
		}
		entry.LogicalKey = key
		entry.SourceRole = inferSourceRole(key)
		entry.Tags = tagsForKey(key, entry.ObjectType)
		entry.RetrievalQuery = retrievalQuery(entry)
		if pos, ok := seen[key]; ok {
			entries[pos] = mergeEntry(entries[pos], entry)
			return
		}
		seen[key] = len(entries)
		entries = append(entries, entry)
	}

	for _, input := range bundle.Inputs {
		purpose, requiredForTask, priority := purposeFor(input.LogicalKey, input.Description, required, optional)
		add(core.ArtifactContextEntry{
			LogicalKey:        input.LogicalKey,
			SourceKind:        SourceKindInput,
			Purpose:           purpose,
			UsageHint:         usageHint(task, input.LogicalKey, requiredForTask),
			ReadPriority:      priority,
			RequiredForTask:   requiredForTask,
			ArtifactVersionID: input.ArtifactVersionID,
			LogicalArtifactID: input.LogicalArtifactID,
			ObjectType:        input.ObjectType,
			ContentType:       input.ContentType,
			Encoding:          input.Encoding,
		})
	}

	for _, previous := range bundle.PreviousOutputs {
		purpose, requiredForTask, priority := purposeFor(previous.LogicalKey, previous.Description, required, optional)
		add(core.ArtifactContextEntry{
			LogicalKey:        previous.LogicalKey,
			SourceKind:        SourceKindPreviousOutput,
			Purpose:           purpose,
			UsageHint:         usageHint(task, previous.LogicalKey, requiredForTask),
			ReadPriority:      priority,
			RequiredForTask:   requiredForTask,
			ArtifactVersionID: previous.ArtifactVersionID,
			LogicalArtifactID: previous.LogicalArtifactID,
			ObjectType:        previous.ObjectType,
			ContentType:       previous.ContentType,
			Encoding:          previous.Encoding,
		})
	}

	bundle.ContextManifest = entries
	return bundle
}

func ScenarioSummary(task core.Task, bundle core.AgentInputBundle) string {
	required := make([]string, 0)
	optional := make([]string, 0)
	for _, entry := range bundle.ContextManifest {
		if entry.RequiredForTask || entry.ReadPriority == ReadPriorityRequired {
			required = append(required, entry.LogicalKey)
			continue
		}
		optional = append(optional, entry.LogicalKey)
	}
	if len(required) == 0 && len(optional) == 0 {
		return fmt.Sprintf("Current task context: you are role=%s running op=%s. No upstream artifacts are registered; use the declared tools only when needed.", task.Role, task.Op)
	}
	return fmt.Sprintf(
		"Current task context: you are role=%s running op=%s. Required artifacts to inspect with artifact_read when needed: %s. Optional/context artifacts: %s. Read only the artifacts that are useful for this task; do not guess their contents.",
		task.Role,
		task.Op,
		strings.Join(required, ", "),
		strings.Join(optional, ", "),
	)
}

func requirementMaps(task core.Task, spec core.OpSpec) (map[string]string, map[string]string) {
	required := map[string]string{}
	optional := map[string]string{}
	for _, item := range spec.BaseRequiredInputs {
		required[item.LogicalKey] = item.Description
	}
	for _, item := range spec.BaseOptionalInputs {
		optional[item.LogicalKey] = item.Description
	}
	mode := core.NormalizeExecutionMode(task.ExecutionMode)
	if rule, ok := spec.ModeInputRules[mode]; ok {
		for _, item := range rule.ExtraRequiredInputs {
			required[item.LogicalKey] = item.Description
		}
		for _, item := range rule.ExtraOptionalInputs {
			optional[item.LogicalKey] = item.Description
		}
	}
	return required, optional
}

func purposeFor(key, existing string, required map[string]string, optional map[string]string) (string, bool, string) {
	if value := strings.TrimSpace(required[key]); value != "" {
		return value, true, ReadPriorityRequired
	}
	if value := strings.TrimSpace(optional[key]); value != "" {
		return value, false, ReadPriorityOptional
	}
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing, false, ReadPriorityContext
	}
	return "Registered upstream artifact available for this task.", false, ReadPriorityContext
}

func usageHint(task core.Task, key string, required bool) string {
	if key == core.LKRepairInstruction {
		return "Read this first in repair mode; it defines the exact issue to fix."
	}
	if required {
		return "Read this before producing final outputs for the current task."
	}
	if task.Role == "coder" || task.Role == "front" {
		return "Read this when implementation details or constraints are unclear."
	}
	return "Read this when it helps validate assumptions or fill missing context."
}

func inferSourceRole(key string) string {
	switch {
	case strings.Contains(key, "requirement"):
		return "ceo"
	case strings.Contains(key, "pm_plan"):
		return "pm"
	case strings.Contains(key, "architecture"), strings.Contains(key, "module"), strings.Contains(key, "container"), strings.Contains(key, "global"):
		return "architect"
	case strings.Contains(key, "coder"):
		return "coder"
	case strings.Contains(key, "test"):
		return "tester"
	default:
		return ""
	}
}

func tagsForKey(key, objectType string) []string {
	tags := []string{}
	if objectType = strings.TrimSpace(objectType); objectType != "" {
		tags = append(tags, objectType)
	}
	for _, token := range strings.Split(strings.ReplaceAll(key, "-", "_"), "_") {
		token = strings.TrimSpace(token)
		if token != "" {
			tags = append(tags, token)
		}
	}
	return tags
}

func retrievalQuery(entry core.ArtifactContextEntry) string {
	parts := []string{entry.LogicalKey, entry.Purpose, entry.UsageHint, entry.SourceRole}
	return strings.Join(nonEmpty(parts), " ")
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func mergeEntry(existing, next core.ArtifactContextEntry) core.ArtifactContextEntry {
	if next.SourceKind != "" {
		existing.SourceKind = next.SourceKind
	}
	if next.Purpose != "" {
		existing.Purpose = next.Purpose
	}
	if next.UsageHint != "" {
		existing.UsageHint = next.UsageHint
	}
	if next.ReadPriority == ReadPriorityRequired || existing.ReadPriority == "" {
		existing.ReadPriority = next.ReadPriority
	}
	existing.RequiredForTask = existing.RequiredForTask || next.RequiredForTask
	if next.ArtifactVersionID != "" {
		existing.ArtifactVersionID = next.ArtifactVersionID
	}
	if next.LogicalArtifactID != "" {
		existing.LogicalArtifactID = next.LogicalArtifactID
	}
	if next.ObjectType != "" {
		existing.ObjectType = next.ObjectType
	}
	if next.ContentType != "" {
		existing.ContentType = next.ContentType
	}
	if next.Encoding != "" {
		existing.Encoding = next.Encoding
	}
	if next.RetrievalQuery != "" {
		existing.RetrievalQuery = next.RetrievalQuery
	}
	if len(next.Tags) > 0 {
		existing.Tags = next.Tags
	}
	return existing
}
```

- [ ] **Step 4: Run manifest tests**

Run:

```powershell
go test ./internal/agent/manifest
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/manifest
git commit -m "feat(agent): build semantic artifact manifests"
```

---

## Task 3: Wire Manifest Enrichment Into Executor

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/executor/executor.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/executor/executor_test.go`

- [ ] **Step 1: Write failing executor test**

Add this test near other `RunAgent` prompt tests:

```go
func TestRuntimeEnrichesBundleWithContextManifestBeforePrompt(t *testing.T) {
	t.Parallel()

	agent := &recordingAgent{role: "tester"}
	agents := registry.NewAgentRegistry()
	agents.Register(agent)
	ops := registry.NewOpRegistry()
	ops.Register(core.OpSpec{
		Role:            "tester",
		Op:              "test_code",
		RoleDescription: "tester role",
		OpDescription:   "test code",
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: "module_spec", Description: "Module spec used to know test scope."},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: "module_test_report", ObjectType: "json", Required: true},
		},
	})
	handlers := registry.NewHandlerRegistry()
	runtime := NewRuntime(agents, ops, handlers)

	_, err := runtime.RunAgent(context.Background(), core.Task{Role: "tester", Op: "test_code"}, core.AgentInputBundle{
		Inputs: []core.InputArtifact{
			{LogicalKey: "module_spec", ArtifactVersionID: "ver-module", ObjectType: "json"},
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(agent.calls))
	}
	if len(agent.calls[0].Bundle.ContextManifest) != 1 {
		t.Fatalf("manifest count = %d, want 1", len(agent.calls[0].Bundle.ContextManifest))
	}
	if got := agent.calls[0].Bundle.ContextManifest[0].Purpose; got != "Module spec used to know test scope." {
		t.Fatalf("manifest purpose = %q", got)
	}
	if !strings.Contains(agent.calls[0].Prompt, "Input context manifest") {
		t.Fatalf("prompt missing manifest section:\n%s", agent.calls[0].Prompt)
	}
}
```

Use the existing recording agent/test helper names if they differ; do not create duplicate helper types with the same names.

- [ ] **Step 2: Run failing executor test**

Run:

```powershell
go test ./internal/agent/executor -run TestRuntimeEnrichesBundleWithContextManifestBeforePrompt
```

Expected: FAIL because `ContextManifest` remains empty or prompt section is missing.

- [ ] **Step 3: Import manifest package**

In `executor.go`, add:

```go
"devflow/internal/agent/manifest"
```

- [ ] **Step 4: Enrich bundle before validation, prompt, and scoped handlers**

In `RunAgent`, after resolving expected outputs and before reuse handling, insert:

```go
bundle = manifest.Enrich(effectiveTask, bundle, spec)
```

The block should be:

```go
resolvedOutputs, err := spec.ResolveExpectedOutputs(bundle)
if err != nil {
	return failResult("invalid_op_spec", err.Error()), nil
}
spec.ExpectedOutputs = resolvedOutputs
bundle = manifest.Enrich(effectiveTask, bundle, spec)
```

- [ ] **Step 5: Run executor tests**

Run:

```powershell
go test ./internal/agent/executor
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/executor
git commit -m "feat(agent): enrich bundles before execution"
```

---

## Task 4: Replace Flat Prompt Input List With Semantic Manifest

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/prompt/compiler.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/prompt/compiler_test.go`

- [ ] **Step 1: Update prompt tests for semantic manifest**

Replace `TestCompileUsesLogicalMetadataWithoutExposingPathsAsPrimaryInput` with:

```go
func TestCompileUsesContextManifestWithoutExposingPaths(t *testing.T) {
	t.Parallel()

	text := Compile(
		core.Task{Role: "tester", Op: "test_code", ExecutionMode: "normal"},
		core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{
					LogicalKey:        "module_spec",
					Path:              "/tmp/module_spec.json",
					ArtifactVersionID: "ver-input",
					LogicalArtifactID: "la-input",
					ObjectType:        "json",
				},
			},
			PreviousOutputs: []core.PreviousOutputRef{
				{
					LogicalKey:        "module_test_report",
					Path:              "/tmp/module_test_report.json",
					ArtifactVersionID: "ver-previous",
					LogicalArtifactID: "la-previous",
					ObjectType:        "json",
				},
			},
			ContextManifest: []core.ArtifactContextEntry{
				{
					LogicalKey:        "module_spec",
					SourceKind:        "input",
					SourceRole:        "architect",
					Purpose:           "Machine-readable module spec used to determine test scope.",
					UsageHint:         "Read this before writing test outputs.",
					ReadPriority:      "required",
					RequiredForTask:   true,
					ArtifactVersionID: "ver-input",
					LogicalArtifactID: "la-input",
					ObjectType:        "json",
					RetrievalQuery:    "module spec test scope",
					Tags:              []string{"json", "module", "spec"},
				},
				{
					LogicalKey:        "module_test_report",
					SourceKind:        "previous_output",
					SourceRole:        "tester",
					Purpose:           "Previous test report available for repair or reuse.",
					UsageHint:         "Read this when comparing prior test output.",
					ReadPriority:      "optional",
					ArtifactVersionID: "ver-previous",
					LogicalArtifactID: "la-previous",
					ObjectType:        "json",
				},
			},
		},
		core.OpSpec{
			Role:            "tester",
			Op:              "test_code",
			RoleDescription: "role description",
			OpDescription:   "op description",
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "module_test_report", ObjectType: "json", Required: true},
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_read", Description: "read"},
				{Name: "artifact_write", Description: "write"},
			},
		},
	)

	for _, want := range []string{
		"Input context manifest",
		"Current task context",
		"module_spec",
		"Machine-readable module spec used to determine test scope.",
		"Read this before writing test outputs.",
		"read_priority: required",
		"source_role: architect",
		"retrieval_query: module spec test scope",
		"module_test_report",
		"previous_output",
		"ver-input",
		"ver-previous",
		"la-input",
		"la-previous",
		"object_type: json",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Compile() missing %q in prompt:\n%s", want, text)
		}
	}
	if strings.Contains(text, "/tmp/module_spec.json") || strings.Contains(text, "/tmp/module_test_report.json") {
		t.Fatalf("Compile() exposed artifact paths in prompt:\n%s", text)
	}
}
```

- [ ] **Step 2: Run prompt test and verify it fails**

Run:

```powershell
go test ./internal/agent/prompt -run TestCompileUsesContextManifestWithoutExposingPaths
```

Expected: FAIL because prompt does not yet have `Input context manifest`.

- [ ] **Step 3: Import manifest package in compiler**

Add:

```go
"devflow/internal/agent/manifest"
```

- [ ] **Step 4: Replace flat input list with manifest rendering**

In `Compile`, replace the current `可读取... logical_key` and `PreviousOutputs` blocks with:

```go
writeInputContextManifest(&b, task, bundle)
```

Add these functions near the bottom:

```go
func writeInputContextManifest(b *strings.Builder, task core.Task, bundle core.AgentInputBundle) {
	entries := bundle.ContextManifest
	if len(entries) == 0 {
		b.WriteString("Input context manifest:\n")
		b.WriteString("- No registered upstream artifacts.\n\n")
		return
	}
	b.WriteString("Input context manifest:\n")
	b.WriteString("These entries describe what each readable artifact is for. Do not assume file contents from this manifest. Use artifact_read(logical_key) to inspect content only when useful for the task.\n")
	for i, entry := range entries {
		fmt.Fprintf(b, "%d. logical_key: %s\n", i+1, entry.LogicalKey)
		if entry.SourceKind != "" {
			fmt.Fprintf(b, "   source_kind: %s\n", entry.SourceKind)
		}
		if entry.SourceRole != "" {
			fmt.Fprintf(b, "   source_role: %s\n", entry.SourceRole)
		}
		if entry.Purpose != "" {
			fmt.Fprintf(b, "   purpose: %s\n", entry.Purpose)
		}
		if entry.UsageHint != "" {
			fmt.Fprintf(b, "   usage_hint: %s\n", entry.UsageHint)
		}
		if entry.ReadPriority != "" {
			fmt.Fprintf(b, "   read_priority: %s\n", entry.ReadPriority)
		}
		fmt.Fprintf(b, "   required_for_task: %t\n", entry.RequiredForTask)
		if entry.ArtifactVersionID != "" {
			fmt.Fprintf(b, "   artifact_version_id: %s\n", entry.ArtifactVersionID)
		}
		if entry.LogicalArtifactID != "" {
			fmt.Fprintf(b, "   logical_artifact_id: %s\n", entry.LogicalArtifactID)
		}
		if entry.ObjectType != "" {
			fmt.Fprintf(b, "   object_type: %s\n", entry.ObjectType)
		}
		if entry.RetrievalQuery != "" {
			fmt.Fprintf(b, "   retrieval_query: %s\n", entry.RetrievalQuery)
		}
		if len(entry.Tags) > 0 {
			fmt.Fprintf(b, "   tags: %s\n", strings.Join(entry.Tags, ", "))
		}
	}
	b.WriteString("\n")
	b.WriteString(manifest.ScenarioSummary(task, bundle))
	b.WriteString("\n\n")
}
```

- [ ] **Step 5: Update tool rules**

Keep the existing rule that says only `artifact_read(logical_key)` can read artifacts. Add one new rule:

```go
b.WriteString("- Use the input context manifest as a map of available evidence; read artifacts selectively instead of reading every artifact by default.\n")
```

- [ ] **Step 6: Run prompt tests**

Run:

```powershell
go test ./internal/agent/prompt
```

Expected: PASS.

- [ ] **Step 7: Run executor tests because prompt shape changed**

Run:

```powershell
go test ./internal/agent/executor
```

Expected: PASS.

- [ ] **Step 8: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/prompt Doujia_clean_source_20260504_175507/Doujia/internal/agent/executor
git commit -m "feat(agent): render semantic input manifests in prompts"
```

---

## Task 5: Add `artifact_list` Tool

**Files:**
- Create: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_list.go`
- Create: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_list_test.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/bootstrap/plugins.go`

- [ ] **Step 1: Write failing handler tests**

Create `artifact_list_test.go`:

```go
package handler

import (
	"context"
	"testing"

	"devflow/internal/agent/core"
)

func TestArtifactListReturnsManifestWithoutPaths(t *testing.T) {
	t.Parallel()

	handler := NewArtifactListHandler()
	resp, err := handler.Handle(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: "module_spec", Path: "/secret/module_spec.json"},
			},
			ContextManifest: []core.ArtifactContextEntry{
				{
					LogicalKey:        "module_spec",
					Purpose:           "Module boundaries.",
					UsageHint:         "Read before coding.",
					ReadPriority:      "required",
					RequiredForTask:   true,
					ArtifactVersionID: "ver-module",
					ObjectType:        "json",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	items, ok := resp.Data["items"].([]core.ArtifactContextEntry)
	if !ok {
		t.Fatalf("items type = %T, want []core.ArtifactContextEntry", resp.Data["items"])
	}
	if len(items) != 1 {
		t.Fatalf("items count = %d, want 1", len(items))
	}
	if items[0].LogicalKey != "module_spec" || items[0].Purpose != "Module boundaries." {
		t.Fatalf("unexpected item = %#v", items[0])
	}
	if _, ok := resp.Data["path"]; ok {
		t.Fatalf("artifact_list must not expose path")
	}
}

func TestScopedArtifactListFallsBackToScopedBundle(t *testing.T) {
	t.Parallel()

	base := NewArtifactListHandler()
	scoped := base.WithScope(core.AgentInputBundle{
		ContextManifest: []core.ArtifactContextEntry{
			{LogicalKey: "requirement", Purpose: "PRD from CEO."},
		},
	})
	resp, err := scoped.Handle(context.Background(), core.HandlerRequest{})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	items := resp.Data["items"].([]core.ArtifactContextEntry)
	if len(items) != 1 || items[0].LogicalKey != "requirement" {
		t.Fatalf("items = %#v", items)
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```powershell
go test ./internal/agent/handler -run ArtifactList
```

Expected: FAIL because `NewArtifactListHandler` is undefined.

- [ ] **Step 3: Implement `artifact_list` handler**

Create `artifact_list.go`:

```go
package handler

import (
	"context"

	"devflow/internal/agent/core"
)

type ArtifactListHandler struct{}

type scopedArtifactListHandler struct {
	bundle core.AgentInputBundle
}

func NewArtifactListHandler() *ArtifactListHandler {
	return &ArtifactListHandler{}
}

func (h *ArtifactListHandler) Name() string {
	return "artifact_list"
}

func (h *ArtifactListHandler) Description() string {
	return "List readable input artifacts with semantic purpose, read priority, and RAG-ready metadata."
}

func (h *ArtifactListHandler) ToolSpec() core.ToolSpec {
	return artifactListToolSpec()
}

func (h *ArtifactListHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleArtifactList(ctx, req)
}

func (h *ArtifactListHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedArtifactListHandler{bundle: bundle}
}

func (h *scopedArtifactListHandler) Name() string {
	return "artifact_list"
}

func (h *scopedArtifactListHandler) Description() string {
	return "List readable input artifacts with semantic purpose, read priority, and RAG-ready metadata."
}

func (h *scopedArtifactListHandler) ToolSpec() core.ToolSpec {
	return artifactListToolSpec()
}

func (h *scopedArtifactListHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if len(req.Bundle.ContextManifest) == 0 && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	return handleArtifactList(ctx, req)
}

func artifactListToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        "artifact_list",
		Description: "List readable artifact logical_keys and semantic usage metadata without reading file contents.",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		},
	}
}

func handleArtifactList(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	items := append([]core.ArtifactContextEntry(nil), req.Bundle.ContextManifest...)
	return core.HandlerResponse{
		Data: map[string]any{
			"items": items,
			"count": len(items),
		},
	}, nil
}
```

- [ ] **Step 4: Register builtin handler**

In `bootstrap/plugins.go`, add the handler registration where builtin handlers are listed:

```go
handler.NewArtifactListHandler()
```

Use the existing registration helper pattern. The final list must contain both `artifact_list` and `artifact_read`.

- [ ] **Step 5: Run handler and registry tests**

Run:

```powershell
go test ./internal/agent/handler ./internal/agent/bootstrap ./internal/agent/registry
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler Doujia_clean_source_20260504_175507/Doujia/internal/agent/bootstrap/plugins.go
git commit -m "feat(agent): add artifact list tool"
```

---

## Task 6: Include Manifest Metadata In `artifact_read`

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_read.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler/artifact_handlers_test.go` or create focused test in `artifact_read_test.go`

- [ ] **Step 1: Add failing test**

Add:

```go
func TestArtifactReadIncludesManifestMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "module_spec.json")
	if err := os.WriteFile(path, []byte(`{"module_id":"module01"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	resp, err := NewArtifactReadHandler().Handle(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: "module_spec", Path: path, ObjectType: "json"},
			},
			ContextManifest: []core.ArtifactContextEntry{
				{
					LogicalKey:      "module_spec",
					Purpose:         "Module boundaries.",
					UsageHint:       "Read before implementation.",
					ReadPriority:    "required",
					RequiredForTask: true,
				},
			},
		},
		Args: map[string]any{"logical_key": "module_spec"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	metadata, ok := resp.Data["context"].(core.ArtifactContextEntry)
	if !ok {
		t.Fatalf("context type = %T, want ArtifactContextEntry", resp.Data["context"])
	}
	if metadata.Purpose != "Module boundaries." || metadata.ReadPriority != "required" {
		t.Fatalf("context = %#v", metadata)
	}
}
```

Add imports `os`, `path/filepath`, `context`, and `devflow/internal/agent/core` as needed.

- [ ] **Step 2: Run failing test**

Run:

```powershell
go test ./internal/agent/handler -run TestArtifactReadIncludesManifestMetadata
```

Expected: FAIL because `context` is not included in response data.

- [ ] **Step 3: Add manifest lookup**

In `handleArtifactRead`, after `data := map[string]any{...}`, add:

```go
if entry, ok := contextEntryForLogicalKey(req.Bundle.ContextManifest, logicalKey); ok {
	data["context"] = entry
}
```

Add helper:

```go
func contextEntryForLogicalKey(entries []core.ArtifactContextEntry, logicalKey string) (core.ArtifactContextEntry, bool) {
	for _, entry := range entries {
		if entry.LogicalKey == logicalKey {
			return entry, true
		}
	}
	return core.ArtifactContextEntry{}, false
}
```

- [ ] **Step 4: Run handler tests**

Run:

```powershell
go test ./internal/agent/handler
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/handler
git commit -m "feat(agent): return artifact context on reads"
```

---

## Task 7: Enable `artifact_list` In First-Wave Op Specs

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/ceo/spec.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/pm/write_plan.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/pm/review_plan.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/architect/write_plan.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/architect/split_module.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/architect/test_data.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/architect/test_code.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/coder/write_code.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/front/write_code.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/tester/test_data.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/tester/test_code.go`
- Modify existing spec tests if any handler-count assertions fail.

- [ ] **Step 1: Add helper in a small shared spec file**

Create `Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec/common/tools.go` if no equivalent exists:

```go
package common

import "devflow/internal/agent/core"

func ArtifactListTool() core.ToolSpec {
	return core.ToolSpec{Name: "artifact_list", Description: "Inspect readable artifact logical_keys, purposes, priorities, and RAG-ready metadata without reading file contents."}
}
```

- [ ] **Step 2: Add `artifact_list` before `artifact_read`**

For each op spec that currently has:

```go
AllowedTools: []core.ToolSpec{
	{Name: "artifact_read", Description: "Read registered input artifact content."},
```

change it to:

```go
AllowedTools: []core.ToolSpec{
	common.ArtifactListTool(),
	{Name: "artifact_read", Description: "Read registered input artifact content."},
```

Import:

```go
"devflow/internal/agent/spec/common"
```

Use an alias if the file already imports another package named `common`.

- [ ] **Step 3: Run spec and registry tests**

Run:

```powershell
go test ./internal/agent/spec/... ./internal/agent/registry ./internal/agent/bootstrap
```

Expected: PASS.

- [ ] **Step 4: Commit**

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal/agent/spec
git commit -m "feat(agent): expose artifact manifest tool to ops"
```

---

## Task 8: End-To-End Regression Tests

**Files:**
- Modify tests only if failures expose legitimate changed expectations.

- [ ] **Step 1: Run targeted agent tests**

Run:

```powershell
cd D:\Doujia5.0\Doujia-main-workspace\Doujia_clean_source_20260504_175507\Doujia
go test ./internal/agent/...
```

Expected: PASS.

- [ ] **Step 2: Run runtime and app tests that exercise dispatch**

Run:

```powershell
go test ./internal/runtime ./internal/app
```

Expected: PASS.

- [ ] **Step 3: Run full Go test suite**

Run:

```powershell
go test ./...
```

Expected: PASS. If a package fails because Docker is unavailable, record the exact package and error, then rerun all non-Docker packages and include the Docker limitation in final notes.

- [ ] **Step 4: Verify prompt has no path leakage**

Run:

```powershell
go test ./internal/agent/prompt -run TestCompileUsesContextManifestWithoutExposingPaths
```

Expected: PASS.

- [ ] **Step 5: Commit any test expectation updates**

If no files changed, skip this commit. If files changed:

```powershell
git add Doujia_clean_source_20260504_175507/Doujia/internal
git commit -m "test(agent): verify context manifest integration"
```

---

## Task 9: Document The Agent Plugin Context Contract

**Files:**
- Create: `Doujia_clean_source_20260504_175507/Doujia/docs/agent_context_manifest.md`
- Modify: `START_HERE.md`

- [ ] **Step 1: Create context manifest docs**

Create `agent_context_manifest.md`:

```markdown
# Agent Context Manifest

Doujia agents receive upstream artifacts through a context manifest instead of raw file content in the initial prompt.

## Why

The manifest tells the agent what each artifact is for:

- which `logical_key` can be read
- which role likely produced it
- whether it is required for the current task
- how it should be used
- which retrieval tags/query can support future RAG

The actual artifact content remains behind tools. Agents should call `artifact_read(logical_key)` only when that artifact is useful for the current task.

## Tools

### artifact_list

Returns the readable artifact manifest without file contents or filesystem paths.

### artifact_read

Reads one registered artifact by `logical_key`. The response includes file content plus the matching manifest context entry.

## Prompt Contract

Prompts include an `Input context manifest` section and a `Current task context` summary. They do not expose local paths as primary inputs.

## RAG Path

Each manifest entry includes `retrieval_query` and `tags`. A future `artifact_search(query, tags)` tool can use the same manifest to retrieve snippets from local artifacts or a vector index without changing op specs.
```

- [ ] **Step 2: Link from `START_HERE.md`**

Add a short note:

```markdown
## Agent Context Manifest

Agent prompts now use a semantic input manifest. See `Doujia_clean_source_20260504_175507/Doujia/docs/agent_context_manifest.md` for the plugin/tool contract and RAG extension path.
```

- [ ] **Step 3: Commit docs**

```powershell
git add START_HERE.md Doujia_clean_source_20260504_175507/Doujia/docs/agent_context_manifest.md
git commit -m "docs(agent): describe context manifest contract"
```

---

## Final Verification Checklist

- [ ] `git status --short` is clean.
- [ ] `go test ./internal/agent/...` passes.
- [ ] `go test ./internal/runtime ./internal/app` passes.
- [ ] Prompt tests prove no local paths are exposed.
- [ ] `artifact_list` appears in builtin handler registry.
- [ ] At least one coder/front/tester op has both `artifact_list` and `artifact_read`.
- [ ] Docs explain why agents should inspect the manifest before reading artifacts.

---

## Execution Notes

Do not change the core security model:

- LLMs must still read artifact contents only through `artifact_read(logical_key)`.
- LLMs must not pass `path`, `output_dir`, or `file_name` to artifact tools.
- The manifest must not expose filesystem paths.
- RAG fields are metadata only in this iteration; do not implement embeddings or vector search yet.

