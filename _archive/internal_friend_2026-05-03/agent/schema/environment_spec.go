package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type EnvironmentSpec struct {
	Runtime            string            `json:"runtime"`
	Image              string            `json:"image"`
	PackageManager     string            `json:"package_manager"`
	SystemPackages     []string          `json:"system_packages"`
	CheckCommands      []string          `json:"check_commands"`
	RepoInitFiles      map[string]string `json:"repo_init_files"`
	SetupCommands      []string          `json:"setup_commands"`
	DefaultTestCommand string            `json:"default_test_command"`
}

func (s EnvironmentSpec) Validate() error {
	required := map[string]string{
		"runtime":              s.Runtime,
		"image":                ChooseContainerImage(s),
		"package_manager":      s.PackageManager,
		"default_test_command": s.DefaultTestCommand,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing_required_field: %s", field)
		}
	}
	if len(s.CheckCommands) == 0 {
		return fmt.Errorf("missing_required_field: check_commands")
	}
	for _, pkg := range s.SystemPackages {
		if !AllowedSystemPackage(pkg) {
			return fmt.Errorf("unsupported_system_package: %s", pkg)
		}
	}
	if len(s.SetupCommands) > 0 {
		return fmt.Errorf("unsupported_setup_command: setup_commands must be empty")
	}
	for _, command := range s.CheckCommands {
		if !AllowedCheckCommand(command) {
			return fmt.Errorf("unsupported_check_command: %s", command)
		}
	}
	if image := ChooseContainerImage(s); !AllowedContainerImage(image) {
		return fmt.Errorf("image_not_allowed: %s", image)
	}
	for name := range s.RepoInitFiles {
		if !SafeRepoInitFile(name) {
			return fmt.Errorf("invalid_create_container_input: unsafe repo_init_files path: %s", name)
		}
	}
	return nil
}

func ReadEnvironmentSpecFile(path string) (EnvironmentSpec, error) {
	var value EnvironmentSpec
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
	return value, nil
}

func ChooseContainerImage(s EnvironmentSpec) string {
	if image := strings.TrimSpace(s.Image); image != "" {
		return image
	}
	switch strings.ToLower(strings.TrimSpace(s.Runtime)) {
	case "python":
		return "python:3.12-bookworm"
	case "go", "golang":
		return "golang:1.23-bookworm"
	default:
		return "node:20-bookworm"
	}
}

func AllowedSystemPackage(pkg string) bool {
	switch strings.ToLower(strings.TrimSpace(pkg)) {
	case "", "git", "curl", "ca-certificates":
		return true
	default:
		return false
	}
}

func AllowedContainerImage(image string) bool {
	switch strings.TrimSpace(image) {
	case "node:20-bookworm", "python:3.12-bookworm", "golang:1.23-bookworm", "golang:1.22-bookworm":
		return true
	default:
		return false
	}
}

func AllowedCheckCommand(command string) bool {
	allowed := map[string]bool{
		"node --version":    true,
		"npm --version":     true,
		"git --version":     true,
		"python --version":  true,
		"python3 --version": true,
		"pip --version":     true,
		"go version":        true,
		"curl --version":    true,
	}
	return allowed[strings.TrimSpace(command)]
}

func SafeRepoInitFile(name string) bool {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(name))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return false
	}
	return !strings.HasPrefix(cleaned, ".git/")
}

func EnvironmentSpecPromptContract() string {
	return strings.TrimSpace(`environment_spec.json 必须是合法 JSON，并严格遵守以下结构约束：
- 必填字段：
  - "runtime": 字符串，例如 "node"
  - "image": 字符串，必须使用允许的镜像之一：node:20-bookworm、python:3.12-bookworm、golang:1.23-bookworm、golang:1.22-bookworm
  - "package_manager": 字符串，例如 "npm"
  - "check_commands": 字符串数组，至少 1 项；每一项必须是允许命令之一：node --version、npm --version、git --version、python --version、python3 --version、pip --version、go version、curl --version
  - "default_test_command": 字符串
- 可选字段：
  - "system_packages": 字符串数组；只允许 git、curl、ca-certificates
  - "repo_init_files": 对象，key 为仓库内相对路径，value 为文件内容；路径不能是绝对路径，不能逃逸到上级目录，不能写入 .git/
  - "setup_commands": 必须为空数组，或省略
- 不要输出注释，不要输出 Markdown 代码块，只输出 JSON 文本。

推荐模板：
{
  "runtime": "node",
  "image": "node:20-bookworm",
  "package_manager": "npm",
  "system_packages": ["git", "curl", "ca-certificates"],
  "check_commands": ["node --version", "npm --version", "git --version"],
  "repo_init_files": {
    "README.md": "# Project\n"
  },
  "setup_commands": [],
  "default_test_command": "cd server && npm test"
}`)
}
