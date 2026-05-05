package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type RunDeliveryConfig struct {
	BackendModuleCount int `json:"backend_module_count"`
}

func (c RunDeliveryConfig) Normalized() RunDeliveryConfig {
	if c.BackendModuleCount <= 0 {
		c.BackendModuleCount = 1
	}
	return c
}

func (c RunDeliveryConfig) Validate() error {
	c = c.Normalized()
	if c.BackendModuleCount < 1 {
		return fmt.Errorf("backend_module_count must be >= 1")
	}
	if c.BackendModuleCount > 4 {
		return fmt.Errorf("backend_module_count must be <= 4")
	}
	return nil
}

func ReadRunDeliveryConfigFile(path string) (RunDeliveryConfig, error) {
	var value RunDeliveryConfig
	absPath, err := filepath.Abs(path)
	if err != nil {
		return value, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(content, &value); err != nil {
		return value, err
	}
	value = value.Normalized()
	return value, value.Validate()
}
