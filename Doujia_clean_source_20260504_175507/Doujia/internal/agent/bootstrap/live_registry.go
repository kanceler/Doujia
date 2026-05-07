package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/agent/registry"
	"devflow/internal/core"
	"devflow/internal/pipeline"
)

type PipelineCatalog interface {
	Replace(spec pipeline.RegistrySpec) error
	CurrentSpec() pipeline.RegistrySpec
	HasPipeline(id core.PipelineID) bool
}

type MutablePipelineCatalog struct {
	mu       sync.RWMutex
	registry pipeline.Registry
	defs     pipeline.DefinitionRegistry
	spec     pipeline.RegistrySpec
}

func NewMutablePipelineCatalog(reg pipeline.Registry, defs pipeline.DefinitionRegistry) *MutablePipelineCatalog {
	var spec pipeline.RegistrySpec
	if defs != nil {
		spec = defs.Spec()
	}
	return &MutablePipelineCatalog{
		registry: reg,
		defs:     defs,
		spec:     spec,
	}
}

func (c *MutablePipelineCatalog) Replace(spec pipeline.RegistrySpec) error {
	spec = pipeline.NormalizeRegistrySpec(spec)
	if err := pipeline.ValidateRegistrySpec(spec); err != nil {
		return err
	}
	defs, err := pipeline.NewJSONRegistry(spec)
	if err != nil {
		return err
	}
	reg, err := compileRuntimePipelineRegistry(spec)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registry = reg
	c.defs = defs
	c.spec = spec
	return nil
}

func compileRuntimePipelineRegistry(spec pipeline.RegistrySpec) (pipeline.Registry, error) {
	legacySpecs := []pipeline.PipelineSpec{pipeline.BuiltinPhaseOne()}
	seen := map[core.PipelineID]bool{
		pipeline.PipelineIDPhaseOne: true,
	}
	addCompiled := func(def pipeline.PipelineDefSpec) error {
		if seen[def.PipelineID] {
			return nil
		}
		legacy, err := pipeline.CompileLegacyPipelineDefPrefix(def)
		if err != nil {
			return err
		}
		legacySpecs = append(legacySpecs, legacy)
		seen[def.PipelineID] = true
		return nil
	}

	if entryDef, ok := spec.Pipeline(spec.EntryPipelineID); ok {
		if err := addCompiled(entryDef); err != nil {
			return nil, err
		}
	}
	if phaseTwoDef, ok := spec.Pipeline(pipeline.PipelineIDPhaseTwo); ok {
		if err := addCompiled(phaseTwoDef); err != nil {
			return nil, err
		}
	}
	for _, def := range spec.PipelineDefs {
		if seen[def.PipelineID] {
			continue
		}
		if err := addCompiled(def); err != nil {
			continue
		}
	}
	return pipeline.NewMemoryRegistry(legacySpecs...), nil
}

func (c *MutablePipelineCatalog) CurrentSpec() pipeline.RegistrySpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.spec
}

func (c *MutablePipelineCatalog) HasPipeline(id core.PipelineID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.defs == nil {
		return false
	}
	_, err := c.defs.GetDef(context.Background(), id)
	return err == nil
}

func (c *MutablePipelineCatalog) Get(ctx context.Context, id core.PipelineID) (pipeline.PipelineSpec, error) {
	c.mu.RLock()
	reg := c.registry
	c.mu.RUnlock()
	if reg == nil {
		return pipeline.PipelineSpec{}, fmt.Errorf("pipeline registry is not configured")
	}
	return reg.Get(ctx, id)
}

func (c *MutablePipelineCatalog) GetDef(ctx context.Context, id core.PipelineID) (pipeline.PipelineDefSpec, error) {
	c.mu.RLock()
	defs := c.defs
	c.mu.RUnlock()
	if defs == nil {
		return pipeline.PipelineDefSpec{}, fmt.Errorf("pipeline definitions are not configured")
	}
	return defs.GetDef(ctx, id)
}

func (c *MutablePipelineCatalog) Entry(ctx context.Context) (pipeline.PipelineDefSpec, error) {
	c.mu.RLock()
	defs := c.defs
	c.mu.RUnlock()
	if defs == nil {
		return pipeline.PipelineDefSpec{}, fmt.Errorf("pipeline definitions are not configured")
	}
	return defs.Entry(ctx)
}

func (c *MutablePipelineCatalog) Spec() pipeline.RegistrySpec {
	return c.CurrentSpec()
}

func (c *MutablePipelineCatalog) Registry() pipeline.Registry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.registry
}

func (c *MutablePipelineCatalog) Definitions() pipeline.DefinitionRegistry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defs
}

type DynamicAgentFactory func(role core.AgentRole) runtimeAgentFactory

type runtimeAgentFactory interface {
	Create(init any, deps any) any
}

type ValidationSourceType string

const (
	ValidationSourcePluginPack ValidationSourceType = "plugin_pack"
	ValidationSourcePipeline   ValidationSourceType = "pipeline_json"
)

type ValidationStatus string

const (
	ValidationStatusPending   ValidationStatus = "pending"
	ValidationStatusRunning   ValidationStatus = "running"
	ValidationStatusSucceeded ValidationStatus = "succeeded"
	ValidationStatusFailed    ValidationStatus = "failed"
)

type ValidationResult struct {
	JobID              string
	SourceType         ValidationSourceType
	Status             ValidationStatus
	FileName           string
	Errors             []string
	ActivatedHandlers  []string
	ActivatedOps       []string
	ActivatedRoles     []string
	ActivatedPipelines []string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type RegistryState struct {
	Handlers  []agentcore.HandlerSpec
	Ops       []agentcore.OpRegistration
	Roles     []agentcore.RoleSpec
	Pipelines []pipeline.PipelineDefSpec
}

type LiveRegistry struct {
	mu              sync.RWMutex
	plugins         *registry.PluginRegistry
	pipelines       *MutablePipelineCatalog
	resolver        DriverResolver
	validationJobs  map[string]ValidationResult
	pluginStorage   string
}

func NewLiveRegistry(basePlugins *registry.PluginRegistry, pipelineCatalog *MutablePipelineCatalog, resolver DriverResolver, storageRoot string) (*LiveRegistry, error) {
	if basePlugins == nil {
		return nil, fmt.Errorf("base plugin registry is required")
	}
	if pipelineCatalog == nil {
		return nil, fmt.Errorf("pipeline catalog is required")
	}
	if resolver == nil {
		resolver = NewDefaultDriverResolver()
	}
	storageRoot = filepath.Clean(strings.TrimSpace(storageRoot))
	if storageRoot == "" {
		return nil, fmt.Errorf("plugin storage root is required")
	}
	if err := os.MkdirAll(storageRoot, 0o755); err != nil {
		return nil, err
	}
	return &LiveRegistry{
		plugins:        clonePluginRegistry(basePlugins),
		pipelines:      pipelineCatalog,
		resolver:       resolver,
		validationJobs: map[string]ValidationResult{},
		pluginStorage:  storageRoot,
	}, nil
}

func (r *LiveRegistry) State() RegistryState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec := r.pipelines.CurrentSpec()
	pipelines := append([]pipeline.PipelineDefSpec(nil), spec.PipelineDefs...)
	sort.Slice(pipelines, func(i, j int) bool {
		return pipelines[i].PipelineID < pipelines[j].PipelineID
	})
	return RegistryState{
		Handlers:  append([]agentcore.HandlerSpec(nil), r.plugins.HandlerSpecs()...),
		Ops:       append([]agentcore.OpRegistration(nil), r.plugins.OpRegistrations()...),
		Roles:     append([]agentcore.RoleSpec(nil), r.plugins.RoleSpecs()...),
		Pipelines: pipelines,
	}
}

func (r *LiveRegistry) ValidationResult(jobID string) (ValidationResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.validationJobs[strings.TrimSpace(jobID)]
	if !ok {
		return ValidationResult{}, fmt.Errorf("validation job %q not found", jobID)
	}
	return result, nil
}

func (r *LiveRegistry) UploadPipelineJSON(ctx context.Context, fileName string, raw []byte) (ValidationResult, error) {
	fileName = sanitizeUploadName(fileName, "pipeline.json")
	job := r.newValidationJob(ValidationSourcePipeline, fileName)
	if err := os.MkdirAll(filepath.Join(r.pluginStorage, job.JobID), 0o755); err != nil {
		return ValidationResult{}, err
	}
	fullPath := filepath.Join(r.pluginStorage, job.JobID, fileName)
	if err := os.WriteFile(fullPath, raw, 0o644); err != nil {
		return ValidationResult{}, err
	}
	result := r.runPipelineValidation(ctx, job, fullPath)
	return result, nil
}

func (r *LiveRegistry) UploadPluginPack(ctx context.Context, fileName string, raw []byte) (ValidationResult, error) {
	fileName = sanitizeUploadName(fileName, "plugin-pack.zip")
	job := r.newValidationJob(ValidationSourcePluginPack, fileName)
	jobDir := filepath.Join(r.pluginStorage, job.JobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return ValidationResult{}, err
	}
	archivePath := filepath.Join(jobDir, fileName)
	if err := os.WriteFile(archivePath, raw, 0o644); err != nil {
		return ValidationResult{}, err
	}
	extractDir := filepath.Join(jobDir, "pack")
	if err := unzipArchive(archivePath, extractDir); err != nil {
		result := r.finishValidation(job.JobID, ValidationStatusFailed, []string{err.Error()}, nil, nil, nil, nil)
		return result, nil
	}
	result := r.runPluginPackValidation(ctx, job, extractDir)
	return result, nil
}

func (r *LiveRegistry) ReplacePluginsAndPipelines(plugins *registry.PluginRegistry, spec pipeline.RegistrySpec) error {
	if plugins == nil {
		return fmt.Errorf("plugin registry is required")
	}
	if err := r.pipelines.Replace(spec); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = clonePluginRegistry(plugins)
	return nil
}

func (r *LiveRegistry) newValidationJob(sourceType ValidationSourceType, fileName string) ValidationResult {
	now := time.Now().UTC()
	job := ValidationResult{
		JobID:      fmt.Sprintf("job_%d", now.UnixNano()),
		SourceType: sourceType,
		Status:     ValidationStatusPending,
		FileName:   fileName,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validationJobs[job.JobID] = job
	return job
}

func (r *LiveRegistry) markValidationRunning(jobID string) ValidationResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.validationJobs[jobID]
	job.Status = ValidationStatusRunning
	job.UpdatedAt = time.Now().UTC()
	r.validationJobs[jobID] = job
	return job
}

func (r *LiveRegistry) finishValidation(jobID string, status ValidationStatus, errors []string, handlers []string, ops []string, roles []string, pipelines []string) ValidationResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.validationJobs[jobID]
	job.Status = status
	job.Errors = append([]string(nil), errors...)
	job.ActivatedHandlers = append([]string(nil), handlers...)
	job.ActivatedOps = append([]string(nil), ops...)
	job.ActivatedRoles = append([]string(nil), roles...)
	job.ActivatedPipelines = append([]string(nil), pipelines...)
	job.UpdatedAt = time.Now().UTC()
	r.validationJobs[jobID] = job
	return job
}

func (r *LiveRegistry) runPipelineValidation(_ context.Context, job ValidationResult, fullPath string) ValidationResult {
	r.markValidationRunning(job.JobID)
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return r.finishValidation(job.JobID, ValidationStatusFailed, []string{err.Error()}, nil, nil, nil, nil)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var def pipeline.PipelineDefSpec
	if err := json.Unmarshal(raw, &def); err != nil {
		return r.finishValidation(job.JobID, ValidationStatusFailed, []string{fmt.Sprintf("decode pipeline JSON: %v", err)}, nil, nil, nil, nil)
	}
	spec := r.pipelines.CurrentSpec()
	nextSpec := spec
	if strings.TrimSpace(nextSpec.SchemaVersion) == "" {
		nextSpec.SchemaVersion = pipeline.RegistrySchemaVersionV04
	}
	if strings.TrimSpace(nextSpec.RegistryID) == "" {
		nextSpec.RegistryID = "dynamic_plugin_registry"
	}
	if strings.TrimSpace(string(nextSpec.EntryPipelineID)) == "" {
		nextSpec.EntryPipelineID = def.PipelineID
	}
	replaced := false
	for i := range nextSpec.PipelineDefs {
		if nextSpec.PipelineDefs[i].PipelineID == def.PipelineID {
			nextSpec.PipelineDefs[i] = def
			replaced = true
			break
		}
	}
	if !replaced {
		nextSpec.PipelineDefs = append(nextSpec.PipelineDefs, def)
	}
	if err := r.pipelines.Replace(nextSpec); err != nil {
		return r.finishValidation(job.JobID, ValidationStatusFailed, []string{err.Error()}, nil, nil, nil, nil)
	}
	return r.finishValidation(job.JobID, ValidationStatusSucceeded, nil, nil, nil, nil, []string{string(def.PipelineID)})
}

func (r *LiveRegistry) runPluginPackValidation(_ context.Context, job ValidationResult, extractDir string) ValidationResult {
	r.markValidationRunning(job.JobID)
	plugins := clonePluginRegistry(r.plugins)
	manager := NewPluginManager(r.resolver)
	if err := manager.LoadInto(plugins, extractDir); err != nil {
		return r.finishValidation(job.JobID, ValidationStatusFailed, []string{err.Error()}, nil, nil, nil, nil)
	}
	spec := r.pipelines.CurrentSpec()
	if err := r.ReplacePluginsAndPipelines(plugins, spec); err != nil {
		return r.finishValidation(job.JobID, ValidationStatusFailed, []string{err.Error()}, nil, nil, nil, nil)
	}
	return r.finishValidation(
		job.JobID,
		ValidationStatusSucceeded,
		nil,
		handlerIDs(plugins.HandlerSpecs()),
		opIDs(plugins.OpRegistrations()),
		roleIDs(plugins.RoleSpecs()),
		nil,
	)
}

func clonePluginRegistry(src *registry.PluginRegistry) *registry.PluginRegistry {
	dst := registry.NewPluginRegistry()
	for _, reg := range src.HandlerRegistrations() {
		_ = dst.RegisterHandler(reg)
	}
	for _, reg := range src.OpRegistrations() {
		_ = dst.RegisterOp(reg)
	}
	for _, reg := range src.RoleRegistrations() {
		_ = dst.RegisterRole(reg)
	}
	return dst
}

func handlerIDs(specs []agentcore.HandlerSpec) []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	sort.Strings(out)
	return out
}

func opIDs(regs []agentcore.OpRegistration) []string {
	out := make([]string, 0, len(regs))
	for _, reg := range regs {
		out = append(out, reg.ID)
	}
	sort.Strings(out)
	return out
}

func roleIDs(specs []agentcore.RoleSpec) []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	sort.Strings(out)
	return out
}

func sanitizeUploadName(value string, fallback string) string {
	value = strings.TrimSpace(filepath.Base(value))
	if value == "" || value == "." || value == string(filepath.Separator) {
		return fallback
	}
	return value
}

func unzipArchive(archivePath string, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		targetPath := filepath.Join(destDir, filepath.FromSlash(file.Name))
		targetAbs, err := filepath.Abs(targetPath)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(targetAbs, destAbs) {
			return fmt.Errorf("zip entry escapes destination: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetAbs, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.Create(targetAbs)
		if err != nil {
			src.Close()
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			return err
		}
		if err := dst.Close(); err != nil {
			src.Close()
			return err
		}
		if err := src.Close(); err != nil {
			return err
		}
	}
	return nil
}
