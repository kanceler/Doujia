package real

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	agentbootstrap "devflow/internal/agent/bootstrap"
	agentcore "devflow/internal/agent/core"
	agentexecutor "devflow/internal/agent/executor"
	"devflow/internal/agent/llm"
	"devflow/internal/core"
	"devflow/internal/runtime"
)

type Agent struct {
	role           core.AgentRole
	runtimeOptions agentbootstrap.RuntimeOptions
	init           runtime.AgentInit
	deps           runtime.AgentDeps
	executor       *agentexecutor.Runtime
	executorErr    error
}

func NewFactory(role core.AgentRole) runtime.Agent {
	return NewFactoryWithRuntimeOptions(role, agentbootstrap.RuntimeOptions{})
}

func NewFactoryWithRuntimeOptions(role core.AgentRole, options agentbootstrap.RuntimeOptions) runtime.Agent {
	return &Agent{
		role:           role,
		runtimeOptions: agentbootstrap.NormalizeRuntimeOptions(options),
	}
}

func (a *Agent) Create(init runtime.AgentInit, deps runtime.AgentDeps) runtime.Agent {
	options := a.runtimeOptions
	if hasRunLLMConfig(init.RunConfig.LLM) {
		options.LLMConfig = llmConfigFromRunConfig(init.RunConfig.LLM)
	}
	if deps.Logger != nil {
		options.LLMTracef = func(format string, args ...any) {
			_ = deps.Logger.Log(init.RunID, "LLM", fmt.Sprintf("agent_id=%s "+format, append([]any{init.AgentID}, args...)...))
		}
	}
	executor, err := agentbootstrap.NewRuntimeWithOptions(options)
	return &Agent{
		role:           a.role,
		runtimeOptions: options,
		init:           init,
		deps:           deps,
		executor:       executor,
		executorErr:    err,
	}
}

func llmConfigFromRunConfig(config core.LLMRunConfig) llm.OpenAIAdapterConfig {
	return llm.OpenAIAdapterConfig{
		BaseURL:  config.BaseURL,
		APIKey:   config.APIKey,
		Model:    config.Model,
		APIStyle: config.APIStyle,
	}
}

func hasRunLLMConfig(config core.LLMRunConfig) bool {
	return firstNonEmpty(config.BaseURL, config.APIKey, config.Model, config.APIStyle) != ""
}

func (a *Agent) Execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.executorErr != nil {
		return core.TaskMetaData{}, a.executorErr
	}
	if task.Direction != core.TaskDirectionDispatch {
		return core.TaskMetaData{}, fmt.Errorf("real agent only executes dispatch tasks")
	}
	if a.executor == nil {
		return core.TaskMetaData{}, fmt.Errorf("real agent executor is not configured")
	}

	bundle := a.toAgentBundle(task)
	result, err := a.executor.RunAgent(ctx, agentcore.Task{
		TaskID:        string(task.TaskID),
		RunID:         string(task.RunID),
		AgentID:       string(task.AgentID),
		Role:          string(a.role),
		Op:            task.Op,
		ExecutionMode: string(task.ExecutionMode),
	}, bundle)
	if err != nil {
		return core.TaskMetaData{}, err
	}

	outputs := toCoreOutputs(result.Outputs)
	producedBags := cloneProducedBags(result.ProducedBags)
	feedback := core.TaskMetaData{
		Direction:     core.TaskDirectionFeedback,
		RunID:         task.RunID,
		TaskID:        task.TaskID,
		ParentID:      task.ParentID,
		DependsOnIDs:  append([]core.TaskID(nil), task.DependsOnIDs...),
		AgentID:       task.AgentID,
		Op:            task.Op,
		ArtifactURIs:  artifactURIsFromOutputs(outputs),
		InputBagIDs:   append([]string(nil), task.InputBagIDs...),
		ExecutionMode: task.ExecutionMode,
		Result:        core.TaskResultCode(result.Result),
		Control:       resultControls(result),
	}
	if feedback.Result == "" {
		feedback.Result = core.TaskResultCodeFail
	}

	if len(outputs) == 0 && len(producedBags) == 0 {
		if a.deps.OutputCommitter != nil && feedback.Result != core.TaskResultCodeOK {
			feedback.Commit = &core.CommitReceipt{
				Result:          feedback.Result,
				Control:         append([]core.Control(nil), feedback.Control...),
				DiagnosticsJSON: diagnosticsJSON(result),
			}
		}
		return feedback, nil
	}
	if a.deps.OutputCommitter == nil {
		return feedback, nil
	}
	receipt, err := a.deps.OutputCommitter.CommitOutputs(ctx, runtime.OutputCommitRequest{
		RunID:           task.RunID,
		AgentID:         task.AgentID,
		TaskID:          task.TaskID,
		Op:              task.Op,
		Result:          feedback.Result,
		Outputs:         outputs,
		ProducedBags:    producedBags,
		Control:         feedback.Control,
		DiagnosticsJSON: diagnosticsJSON(result),
	})
	if err != nil {
		return core.TaskMetaData{}, err
	}
	feedback.Commit = &receipt
	return feedback, nil
}

func (a *Agent) toAgentBundle(task core.TaskMetaData) agentcore.AgentInputBundle {
	outputDir := filepath.Join(a.init.WorkspacePath, "artifacts", sanitizePathPart(string(task.TaskID)))
	outputURIBase := path.Join("projects", string(task.RunID), "agents", string(task.AgentID), "artifacts", sanitizePathPart(string(task.TaskID)))
	bundle := agentcore.AgentInputBundle{
		InputDir:      filepath.Join(a.init.WorkspacePath, "inputs", sanitizePathPart(string(task.TaskID))),
		OutputDir:     outputDir,
		OutputURIBase: outputURIBase,
	}
	if task.InputBundle == nil {
		bundle.Inputs = inputsFromArtifactURIs(a.init.RunRoot, task.ArtifactURIs)
		bundle.Inputs = aliasModuleScopedInputs(bundle.Inputs)
		return bundle
	}
	bundle.Bags = toAgentBags(task.InputBundle.Bags)
	bundle.Versions = toAgentVersions(a.init.RunRoot, task.InputBundle.Versions)
	bundle.Inputs = toAgentInputs(a.init.RunRoot, task.InputBundle.Inputs, task.InputBundle.Versions)
	if len(bundle.Inputs) == 0 {
		bundle.Inputs = inputsFromVersions(a.init.RunRoot, task.InputBundle.Versions)
	}
	bundle.Inputs = mergeAgentInputs(bundle.Inputs, inputsFromArtifactURIs(a.init.RunRoot, task.ArtifactURIs))
	bundle.Inputs = aliasModuleScopedInputs(bundle.Inputs)
	bundle.PreviousOutputs = toAgentPreviousOutputs(a.init.RunRoot, task.InputBundle.PreviousOutputs)
	return bundle
}

func toAgentBags(items []core.AgentInputBag) []agentcore.AgentInputBag {
	out := make([]agentcore.AgentInputBag, 0, len(items))
	for _, item := range items {
		out = append(out, agentcore.AgentInputBag{
			Name:               item.Name,
			BagID:              item.BagID,
			Indexes:            cloneStringMap(item.Indexes),
			ArtifactVersionIDs: append([]string(nil), item.ArtifactVersionIDs...),
		})
	}
	return out
}

func toAgentVersions(runRoot string, items []core.AgentInputVersion) []agentcore.AgentInputVersion {
	out := make([]agentcore.AgentInputVersion, 0, len(items))
	for _, item := range items {
		storageURI := firstNonEmpty(item.StorageURI, firstObjectStorageURI(item.Objects))
		out = append(out, agentcore.AgentInputVersion{
			ArtifactVersionID: item.ArtifactVersionID,
			LogicalArtifactID: item.LogicalArtifactID,
			LogicalKey:        item.LogicalKey,
			ObjectType:        firstNonEmpty(item.ObjectType, firstObjectType(item.Objects)),
			ContentType:       item.ContentType,
			Encoding:          item.Encoding,
			ObjectIDs:         append([]string(nil), item.ObjectIDs...),
			StorageURI:        storageURI,
			LocalPath:         absoluteArtifactPath(runRoot, firstNonEmpty(item.LocalPath, storageURI)),
		})
	}
	return out
}

func toAgentInputs(runRoot string, inputs []core.InputArtifact, versions []core.AgentInputVersion) []agentcore.InputArtifact {
	versionByID := make(map[string]core.AgentInputVersion, len(versions))
	for _, version := range versions {
		versionByID[version.ArtifactVersionID] = version
	}
	out := make([]agentcore.InputArtifact, 0, len(inputs))
	for _, input := range inputs {
		version := versionByID[input.ArtifactVersionID]
		storageURI := firstNonEmpty(version.StorageURI, firstObjectStorageURI(version.Objects))
		logicalKey := firstNonEmpty(input.LogicalKey, version.LogicalKey, inferLogicalKeyFromURI(storageURI))
		if logicalKey == "" {
			continue
		}
		out = append(out, agentcore.InputArtifact{
			LogicalKey:        logicalKey,
			Path:              absoluteArtifactPath(runRoot, firstNonEmpty(input.Path, version.LocalPath, storageURI)),
			ArtifactVersionID: input.ArtifactVersionID,
			LogicalArtifactID: firstNonEmpty(input.LogicalArtifactID, version.LogicalArtifactID),
			ObjectType:        firstNonEmpty(input.ObjectType, version.ObjectType, firstObjectType(version.Objects)),
			ContentType:       firstNonEmpty(input.ContentType, version.ContentType),
			Encoding:          firstNonEmpty(input.Encoding, version.Encoding),
			Description:       input.Description,
		})
	}
	return out
}

func inputsFromVersions(runRoot string, versions []core.AgentInputVersion) []agentcore.InputArtifact {
	out := make([]agentcore.InputArtifact, 0, len(versions))
	for _, version := range versions {
		storageURI := firstNonEmpty(version.StorageURI, firstObjectStorageURI(version.Objects))
		logicalKey := firstNonEmpty(version.LogicalKey, inferLogicalKeyFromURI(storageURI))
		if logicalKey == "" || storageURI == "" {
			continue
		}
		out = append(out, agentcore.InputArtifact{
			LogicalKey:        logicalKey,
			Path:              absoluteArtifactPath(runRoot, firstNonEmpty(version.LocalPath, storageURI)),
			ArtifactVersionID: version.ArtifactVersionID,
			LogicalArtifactID: version.LogicalArtifactID,
			ObjectType:        firstNonEmpty(version.ObjectType, firstObjectType(version.Objects)),
			ContentType:       version.ContentType,
			Encoding:          version.Encoding,
		})
	}
	return out
}

func inputsFromArtifactURIs(runRoot string, uris []string) []agentcore.InputArtifact {
	out := make([]agentcore.InputArtifact, 0, len(uris))
	for _, uri := range uris {
		logicalKey := inferLogicalKeyFromURI(uri)
		if logicalKey == "" {
			continue
		}
		out = append(out, agentcore.InputArtifact{
			LogicalKey: logicalKey,
			Path:       absoluteArtifactPath(runRoot, uri),
		})
	}
	return out
}

func toAgentPreviousOutputs(runRoot string, items []core.PreviousOutputRef) []agentcore.PreviousOutputRef {
	out := make([]agentcore.PreviousOutputRef, 0, len(items))
	for _, item := range items {
		out = append(out, agentcore.PreviousOutputRef{
			LogicalKey:        item.LogicalKey,
			ArtifactVersionID: item.ArtifactVersionID,
			ArtifactURI:       item.ArtifactURI,
			LogicalArtifactID: item.LogicalArtifactID,
			ObjectType:        item.ObjectType,
			ContentType:       item.ContentType,
			Encoding:          item.Encoding,
			Path:              absoluteArtifactPath(runRoot, firstNonEmpty(item.Path, item.ArtifactURI)),
			Description:       item.Description,
		})
	}
	return out
}

func toCoreOutputs(items []agentcore.AgentOutput) []core.AgentOutput {
	out := make([]core.AgentOutput, 0, len(items))
	for _, item := range items {
		out = append(out, core.AgentOutput{
			LogicalKey:        item.LogicalKey,
			ObjectType:        item.ObjectType,
			ContentType:       item.ContentType,
			Encoding:          item.Encoding,
			Status:            item.Status,
			Path:              item.Path,
			ArtifactURI:       filepath.ToSlash(item.ArtifactURI),
			ArtifactVersionID: item.ArtifactVersionID,
			Description:       item.Description,
		})
	}
	return out
}

func artifactURIsFromOutputs(outputs []core.AgentOutput) []string {
	out := make([]string, 0, len(outputs))
	for _, output := range outputs {
		if strings.TrimSpace(output.Status) != "produced" {
			continue
		}
		if uri := strings.TrimSpace(output.ArtifactURI); uri != "" {
			out = append(out, filepath.ToSlash(uri))
		}
	}
	return uniqueStrings(out)
}

func cloneProducedBags(defs []core.ProducedBagManifest) []core.ProducedBagManifest {
	out := make([]core.ProducedBagManifest, 0, len(defs))
	for _, def := range defs {
		next := core.ProducedBagManifest{
			Name:    def.Name,
			Indexes: cloneStringMap(def.Indexes),
			Members: append([]core.ProducedBagMember(nil), def.Members...),
		}
		out = append(out, next)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func resultControls(result agentcore.AgentResult) []core.Control {
	controls := make([]core.Control, 0, len(result.Control)+len(result.Controls))
	controls = append(controls, result.Control...)
	controls = append(controls, result.Controls...)
	return controls
}

func moduleIDsFromAgentOutputs(outputs []agentcore.AgentOutput) []string {
	keys := make(map[string]agentcore.AgentOutput, len(outputs))
	for _, output := range outputs {
		keys[output.LogicalKey] = output
	}
	return moduleIDsFromOutputs(keys)
}

func moduleIDsFromOutputs(outputs map[string]agentcore.AgentOutput) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for key := range outputs {
		if !strings.HasPrefix(key, "module") || !strings.HasSuffix(key, "_spec") {
			continue
		}
		moduleID := strings.TrimSuffix(key, "_spec")
		if seen[moduleID] {
			continue
		}
		seen[moduleID] = true
		out = append(out, moduleID)
	}
	sortStrings(out)
	return out
}

func inputVersionIDsByLogicalKey(bundle agentcore.AgentInputBundle) map[string][]string {
	out := make(map[string][]string)
	for _, version := range bundle.Versions {
		if version.LogicalKey == "" || version.ArtifactVersionID == "" {
			continue
		}
		out[version.LogicalKey] = append(out[version.LogicalKey], version.ArtifactVersionID)
	}
	for _, input := range bundle.Inputs {
		if input.LogicalKey == "" || input.ArtifactVersionID == "" {
			continue
		}
		out[input.LogicalKey] = append(out[input.LogicalKey], input.ArtifactVersionID)
	}
	for key, ids := range out {
		out[key] = uniqueStrings(ids)
	}
	return out
}

func agentRoleFromTask(task core.TaskMetaData) string {
	id := strings.ToLower(string(task.AgentID))
	switch {
	case strings.HasPrefix(id, "pm"):
		return "pm"
	case strings.HasPrefix(id, "ceo"):
		return "ceo"
	case strings.HasPrefix(id, "architect"):
		return "architect"
	case strings.HasPrefix(id, "tester"):
		return "tester"
	case strings.HasPrefix(id, "coder"):
		return "coder"
	default:
		return ""
	}
}

func absoluteArtifactPath(runRoot string, uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if filepath.IsAbs(uri) {
		return filepath.Clean(uri)
	}
	return filepath.Join(runRoot, filepath.FromSlash(normalizeArtifactURI(uri)))
}

func normalizeArtifactURI(uri string) string {
	uri = filepath.ToSlash(strings.TrimSpace(uri))
	parts := strings.Split(uri, "/")
	if len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" {
		return strings.Join(parts[2:], "/")
	}
	return uri
}

func inferLogicalKeyFromURI(uri string) string {
	normalized := strings.ToLower(filepath.ToSlash(strings.TrimSpace(uri)))
	switch {
	case strings.Contains(normalized, "requirement"):
		return agentcore.LKRequirement
	case strings.Contains(normalized, "plan"):
		return agentcore.LKPMPlan
	case strings.Contains(normalized, "architecture"):
		return agentcore.LKArchitecturePlan
	case strings.Contains(normalized, "environment_spec"):
		return agentcore.LKEnvironmentSpec
	case strings.Contains(normalized, "container_context"):
		return agentcore.LKContainerContext
	case strings.Contains(normalized, "run_delivery_config"):
		return agentcore.LKRunDeliveryConfig
	default:
		return ""
	}
}

func mergeAgentInputs(primary []agentcore.InputArtifact, extra []agentcore.InputArtifact) []agentcore.InputArtifact {
	if len(extra) == 0 {
		return primary
	}
	out := append([]agentcore.InputArtifact(nil), primary...)
	seen := make(map[string]bool, len(out))
	for _, input := range out {
		seen[input.LogicalKey+"|"+filepath.Clean(input.Path)] = true
	}
	for _, input := range extra {
		if strings.TrimSpace(input.LogicalKey) == "" || strings.TrimSpace(input.Path) == "" {
			continue
		}
		key := input.LogicalKey + "|" + filepath.Clean(input.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, input)
	}
	return out
}

func aliasModuleScopedInputs(inputs []agentcore.InputArtifact) []agentcore.InputArtifact {
	out := append([]agentcore.InputArtifact(nil), inputs...)
	seen := make(map[string]bool, len(out))
	for _, input := range out {
		if strings.TrimSpace(input.LogicalKey) != "" {
			seen[input.LogicalKey] = true
		}
	}
	for _, input := range inputs {
		alias := genericModuleInputKey(input.LogicalKey)
		if alias == "" || seen[alias] {
			continue
		}
		next := input
		next.LogicalKey = alias
		seen[alias] = true
		out = append(out, next)
	}
	return out
}

func genericModuleInputKey(logicalKey string) string {
	parts := strings.SplitN(strings.TrimSpace(logicalKey), "_", 2)
	if len(parts) != 2 || !isModuleID(parts[0]) {
		return ""
	}
	switch parts[1] {
	case "spec":
		return agentcore.LKModuleSpec
	case "coder_task":
		return agentcore.LKCoderTask
	case "tester_task":
		return agentcore.LKTesterTask
	case "contract":
		return agentcore.LKModuleContract
	case "seed_tests":
		return agentcore.LKSeedTests
	case "coder_branch":
		return agentcore.LKCoderBranch
	case "module_test_report":
		return agentcore.LKModuleTestReport
	default:
		return ""
	}
}

func isModuleID(value string) bool {
	if !strings.HasPrefix(value, "module") || len(value) <= len("module") {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "module") {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func firstObjectStorageURI(objects []core.AgentInputObject) string {
	for _, object := range objects {
		if strings.TrimSpace(object.StorageURI) != "" {
			return object.StorageURI
		}
	}
	return ""
}

func firstObjectType(objects []core.AgentInputObject) string {
	for _, object := range objects {
		if strings.TrimSpace(object.ObjectType) != "" {
			return object.ObjectType
		}
	}
	return ""
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func sortStrings(items []string) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "task"
	}
	return out
}

func diagnosticsJSON(result agentcore.AgentResult) string {
	raw, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	return string(raw)
}
