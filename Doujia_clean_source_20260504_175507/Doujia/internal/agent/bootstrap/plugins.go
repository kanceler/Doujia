package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/agent/handler"
	"devflow/internal/agent/registry"
	rolearchitect "devflow/internal/agent/role/architect"
	roleceo "devflow/internal/agent/role/ceo"
	rolecoder "devflow/internal/agent/role/coder"
	rolefront "devflow/internal/agent/role/front"
	rolepm "devflow/internal/agent/role/pm"
	roletester "devflow/internal/agent/role/tester"
	architectspec "devflow/internal/agent/spec/architect"
	ceospec "devflow/internal/agent/spec/ceo"
	coderspec "devflow/internal/agent/spec/coder"
	frontspec "devflow/internal/agent/spec/front"
	pmspec "devflow/internal/agent/spec/pm"
	testerspec "devflow/internal/agent/spec/tester"
)

type PluginPackManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

type BuiltinResolver struct{}

func NewBuiltinResolver() BuiltinResolver {
	return BuiltinResolver{}
}

func (BuiltinResolver) ResolveHandler(implRef string) (agentcore.Handler, error) {
	switch strings.TrimSpace(implRef) {
	case "builtin:artifact_read":
		return handler.NewArtifactReadHandler(), nil
	case "builtin:artifact_write":
		return handler.NewArtifactWriteHandler(), nil
	case "builtin:container_create":
		return handler.NewContainerCreateHandler(), nil
	case "builtin:container_read":
		return handler.NewContainerReadHandler(), nil
	case "builtin:container_write":
		return handler.NewContainerWriteHandler(), nil
	case "builtin:container_run":
		return handler.NewContainerRunHandler(), nil
	case "builtin:container_exec":
		return handler.NewContainerExecHandler(), nil
	case "builtin:container_git_commit":
		return handler.NewContainerGitCommitHandler(), nil
	case "builtin:container_git_cherry_pick":
		return handler.NewContainerGitCherryPickHandler(), nil
	case "builtin:container_git_worktree_prepare":
		return handler.NewContainerGitWorktreePrepareHandler(), nil
	case "builtin:task_complete":
		return handler.NewTaskCompleteHandler(), nil
	default:
		return nil, fmt.Errorf("unsupported builtin handler impl_ref %q", implRef)
	}
}

func (BuiltinResolver) ResolveOp(implRef string) (agentcore.OpSpec, error) {
	switch strings.TrimSpace(implRef) {
	case "builtin:architect.write_plan":
		return architectspec.WritePlanSpec(), nil
	case "builtin:architect.create_container":
		return architectspec.CreateContainerSpec(), nil
	case "builtin:architect.split_module":
		return architectspec.SplitModuleSpec(), nil
	case "builtin:architect.merge_code":
		return architectspec.MergeCodeSpec(), nil
	case "builtin:architect.test_data":
		return architectspec.TestDataSpec(), nil
	case "builtin:architect.test_code":
		return architectspec.TestCodeSpec(), nil
	case "builtin:ceo.write_plan":
		return ceospec.WritePlanSpec(), nil
	case "builtin:ceo.review_plan":
		return ceospec.ReviewPlanSpec(), nil
	case "builtin:coder.write_code":
		return coderspec.WriteCodeSpec(), nil
	case "builtin:coder.debug_write_code":
		return coderspec.DebugWriteCodeSpec(), nil
	case "builtin:front.write_code":
		return frontspec.WriteCodeSpec(), nil
	case "builtin:front.debug_write_code":
		return frontspec.DebugWriteCodeSpec(), nil
	case "builtin:front.preview_edit":
		return frontspec.PreviewEditSpec(), nil
	case "builtin:front.user_preview_confirm":
		return frontspec.UserPreviewConfirmSpec(), nil
	case "builtin:pm.write_plan":
		return pmspec.WritePlanSpec(), nil
	case "builtin:pm.review_plan":
		return pmspec.ReviewPlanSpec(), nil
	case "builtin:tester.test_data":
		return testerspec.TestDataSpec(), nil
	case "builtin:tester.test_code":
		return testerspec.TestCodeSpec(), nil
	default:
		return agentcore.OpSpec{}, fmt.Errorf("unsupported builtin op impl_ref %q", implRef)
	}
}

func (BuiltinResolver) ResolveRole(driverRef string) (agentcore.Agent, error) {
	switch strings.TrimSpace(driverRef) {
	case "builtin:architect":
		return rolearchitect.NewAgent(), nil
	case "builtin:ceo":
		return roleceo.NewAgent(), nil
	case "builtin:coder":
		return rolecoder.NewAgent(), nil
	case "builtin:front":
		return rolefront.NewAgent(), nil
	case "builtin:pm":
		return rolepm.NewAgent(), nil
	case "builtin:tester":
		return roletester.NewAgent(), nil
	default:
		return nil, fmt.Errorf("unsupported builtin role driver_ref %q", driverRef)
	}
}

type PluginManager struct {
	resolver DriverResolver
}

func NewPluginManager(resolver DriverResolver) *PluginManager {
	if resolver == nil {
		resolver = NewDefaultDriverResolver()
	}
	return &PluginManager{resolver: resolver}
}

func (m *PluginManager) LoadDir(root string) (*registry.PluginRegistry, error) {
	plugins := registry.NewPluginRegistry()
	if err := m.LoadInto(plugins, root); err != nil {
		return nil, err
	}
	return plugins, nil
}

func (m *PluginManager) LoadInto(plugins *registry.PluginRegistry, root string) error {
	if plugins == nil {
		return fmt.Errorf("plugin registry is required")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	root = filepath.Clean(root)
	manifestPath := filepath.Join(root, "plugin.json")
	if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
		return m.loadPack(plugins, root)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := m.loadPack(plugins, filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (m *PluginManager) loadPack(plugins *registry.PluginRegistry, packDir string) error {
	if _, err := readPluginPackManifest(filepath.Join(packDir, "plugin.json")); err != nil {
		return err
	}
	handlers, err := collectJSONFiles(filepath.Join(packDir, "handlers"), "handler.json")
	if err != nil {
		return err
	}
	for _, path := range handlers {
		reg, err := m.readHandlerRegistration(path)
		if err != nil {
			return err
		}
		if err := plugins.RegisterHandler(reg); err != nil {
			return err
		}
	}
	ops, err := collectJSONFiles(filepath.Join(packDir, "ops"), "op.json")
	if err != nil {
		return err
	}
	for _, path := range ops {
		reg, err := m.readOpRegistration(path)
		if err != nil {
			return err
		}
		if err := plugins.RegisterOp(reg); err != nil {
			return err
		}
	}
	roles, err := collectJSONFiles(filepath.Join(packDir, "roles"), "role.json")
	if err != nil {
		return err
	}
	for _, path := range roles {
		reg, err := m.readRoleRegistration(path)
		if err != nil {
			return err
		}
		if err := plugins.RegisterRole(reg); err != nil {
			return err
		}
	}
	return nil
}

func (m *PluginManager) readHandlerRegistration(path string) (agentcore.HandlerRegistration, error) {
	var reg agentcore.HandlerRegistration
	if err := readJSONFile(path, &reg.Spec); err != nil {
		return reg, err
	}
	handlerImpl, err := m.resolver.ResolveHandler(reg.Spec)
	if err != nil {
		return reg, err
	}
	reg.Handler = handlerImpl
	return reg, nil
}

func (m *PluginManager) readOpRegistration(path string) (agentcore.OpRegistration, error) {
	var reg agentcore.OpRegistration
	if err := readJSONFile(path, &reg); err != nil {
		return reg, err
	}
	spec, err := m.resolver.ResolveOp(reg)
	if err != nil {
		return reg, err
	}
	reg.Spec = spec
	return reg, nil
}

func (m *PluginManager) readRoleRegistration(path string) (agentcore.RoleRegistration, error) {
	var spec agentcore.RoleSpec
	if err := readJSONFile(path, &spec); err != nil {
		return agentcore.RoleRegistration{}, err
	}
	agentImpl, err := m.resolver.ResolveRole(spec)
	if err != nil {
		return agentcore.RoleRegistration{}, err
	}
	return agentcore.RoleRegistration{
		Spec:  spec,
		Agent: agentImpl,
	}, nil
}

func readPluginPackManifest(path string) (PluginPackManifest, error) {
	var manifest PluginPackManifest
	if err := readJSONFile(path, &manifest); err != nil {
		return manifest, err
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return manifest, fmt.Errorf("plugin manifest %s missing name", path)
	}
	return manifest, nil
}

func readJSONFile(path string, target any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func collectJSONFiles(root string, fileName string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), fileName)
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}
