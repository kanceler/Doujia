package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	agentcore "devflow/internal/agent/core"
)

func WriteBuiltinPluginPack(root string) (string, error) {
	packDir := filepath.Join(root, "builtin")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(packDir, "plugin.json"), PluginPackManifest{
		Name:        "builtin",
		Version:     "v1",
		Description: "Builtin Doujia agent handlers, ops, and roles",
	}); err != nil {
		return "", err
	}
	for _, reg := range builtinHandlerRegistrations() {
		if err := writeHandlerPack(filepath.Join(packDir, "handlers"), reg); err != nil {
			return "", err
		}
	}
	for _, reg := range builtinOpRegistrations() {
		if err := writeOpPack(filepath.Join(packDir, "ops"), reg); err != nil {
			return "", err
		}
	}
	for _, reg := range builtinRoleRegistrations() {
		if err := writeRolePack(filepath.Join(packDir, "roles"), reg); err != nil {
			return "", err
		}
	}
	return packDir, nil
}

func writeHandlerPack(root string, reg agentcore.HandlerRegistration) error {
	reg = agentcore.NormalizeHandlerRegistration(reg)
	dir := filepath.Join(root, reg.Spec.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "handler.json"), reg.Spec)
}

func writeOpPack(root string, reg agentcore.OpRegistration) error {
	reg = agentcore.NormalizeOpRegistration(reg)
	dir := filepath.Join(root, reg.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	type fileShape struct {
		ID                 string            `json:"op_id"`
		Version            string            `json:"version,omitempty"`
		Name               string            `json:"name,omitempty"`
		Description        string            `json:"description,omitempty"`
		ImplRef            string            `json:"impl_ref,omitempty"`
		RequiredHandlerIDs []string          `json:"required_handlers,omitempty"`
		AllowedHandlerIDs  []string          `json:"allowed_handlers,omitempty"`
		Spec               *agentcore.OpSpec `json:"-"`
	}
	return writeJSON(filepath.Join(dir, "op.json"), fileShape{
		ID:                 reg.ID,
		Version:            reg.Version,
		Name:               reg.Name,
		Description:        reg.Description,
		ImplRef:            builtinOpImplRef(reg.ID),
		RequiredHandlerIDs: reg.RequiredHandlerIDs,
		AllowedHandlerIDs:  reg.AllowedHandlerIDs,
	})
}

func writeRolePack(root string, reg agentcore.RoleRegistration) error {
	reg = agentcore.NormalizeRoleRegistration(reg)
	dir := filepath.Join(root, reg.Spec.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	spec := reg.Spec
	if spec.DriverRef == "" {
		spec.DriverRef = builtinRoleDriverRef(spec.ID)
	}
	if spec.ExecutionDriver == "" {
		spec.ExecutionDriver = "builtin_role"
	}
	if spec.InteractionMode == "" {
		spec.InteractionMode = "task"
	}
	return writeJSON(filepath.Join(dir, "role.json"), spec)
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

func builtinOpImplRef(opID string) string {
	return "builtin:" + opID
}

func builtinRoleDriverRef(roleID string) string {
	return "builtin:" + roleID
}
