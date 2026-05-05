package core

import "strings"

type HandlerSpec struct {
	ID              string `json:"handler_id"`
	Version         string `json:"version,omitempty"`
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	ExecutionDriver string `json:"execution_driver,omitempty"`
	ImplRef         string `json:"impl_ref,omitempty"`
	EndpointPath    string `json:"endpoint_path,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
}

type HandlerRegistration struct {
	Spec    HandlerSpec `json:"spec"`
	Handler Handler     `json:"-"`
}

type OpRegistration struct {
	ID                 string   `json:"op_id"`
	Version            string   `json:"version,omitempty"`
	Name               string   `json:"name,omitempty"`
	Description        string   `json:"description,omitempty"`
	ImplRef            string   `json:"impl_ref,omitempty"`
	RequiredHandlerIDs []string `json:"required_handlers,omitempty"`
	AllowedHandlerIDs  []string `json:"allowed_handlers,omitempty"`
	Spec               OpSpec   `json:"-"`
}

type RoleOpBinding struct {
	Name string `json:"name"`
	OpID string `json:"op_id"`
}

type RoleSpec struct {
	ID              string          `json:"role_id"`
	Version         string          `json:"version,omitempty"`
	Name            string          `json:"name,omitempty"`
	Description     string          `json:"description,omitempty"`
	ExecutionDriver string          `json:"execution_driver,omitempty"`
	DriverRef       string          `json:"driver_ref,omitempty"`
	EndpointPath    string          `json:"endpoint_path,omitempty"`
	TimeoutSeconds  int             `json:"timeout_seconds,omitempty"`
	InteractionMode string          `json:"interaction_mode,omitempty"`
	SupportedOps    []RoleOpBinding `json:"supported_ops,omitempty"`
}

type RoleRegistration struct {
	Spec  RoleSpec `json:"spec"`
	Agent Agent    `json:"-"`
}

func NormalizeHandlerRegistration(reg HandlerRegistration) HandlerRegistration {
	reg.Spec.ID = strings.TrimSpace(reg.Spec.ID)
	reg.Spec.Name = strings.TrimSpace(reg.Spec.Name)
	reg.Spec.Description = strings.TrimSpace(reg.Spec.Description)
	if reg.Handler != nil {
		if reg.Spec.ID == "" {
			reg.Spec.ID = strings.TrimSpace(reg.Handler.Name())
		}
		if reg.Spec.Name == "" {
			reg.Spec.Name = strings.TrimSpace(reg.Handler.Name())
		}
		if reg.Spec.Description == "" {
			reg.Spec.Description = strings.TrimSpace(reg.Handler.Description())
		}
	}
	return reg
}

func NormalizeOpRegistration(reg OpRegistration) OpRegistration {
	reg.ID = strings.TrimSpace(reg.ID)
	reg.Name = strings.TrimSpace(reg.Name)
	reg.Description = strings.TrimSpace(reg.Description)
	if reg.Name == "" {
		reg.Name = strings.TrimSpace(reg.Spec.Op)
	}
	if reg.Description == "" {
		reg.Description = strings.TrimSpace(reg.Spec.OpDescription)
	}
	reg.RequiredHandlerIDs = uniqueTrimmedStrings(reg.RequiredHandlerIDs)
	reg.AllowedHandlerIDs = uniqueTrimmedStrings(reg.AllowedHandlerIDs)
	return reg
}

func NormalizeRoleRegistration(reg RoleRegistration) RoleRegistration {
	reg.Spec.ID = strings.TrimSpace(reg.Spec.ID)
	reg.Spec.Name = strings.TrimSpace(reg.Spec.Name)
	reg.Spec.Description = strings.TrimSpace(reg.Spec.Description)
	if reg.Agent != nil && reg.Spec.ID == "" {
		reg.Spec.ID = strings.TrimSpace(reg.Agent.Role())
	}
	if reg.Spec.Name == "" {
		reg.Spec.Name = reg.Spec.ID
	}
	for i := range reg.Spec.SupportedOps {
		reg.Spec.SupportedOps[i].Name = strings.TrimSpace(reg.Spec.SupportedOps[i].Name)
		reg.Spec.SupportedOps[i].OpID = strings.TrimSpace(reg.Spec.SupportedOps[i].OpID)
	}
	return reg
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
