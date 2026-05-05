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
	PathPolicy         PathPolicy        `json:"path_policy,omitempty"`
}

type PathPolicy struct {
	FrontendRoots  []string `json:"frontend_roots,omitempty"`
	BackendRoots   []string `json:"backend_roots,omitempty"`
	PackagingRoots []string `json:"packaging_roots,omitempty"`
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
	if err := s.PathPolicy.Validate(); err != nil {
		return err
	}
	return nil
}

func (s EnvironmentSpec) EffectivePathPolicy() PathPolicy {
	policy := PathPolicy{
		FrontendRoots:  append([]string{}, s.PathPolicy.FrontendRoots...),
		BackendRoots:   append([]string{}, s.PathPolicy.BackendRoots...),
		PackagingRoots: append([]string{}, s.PathPolicy.PackagingRoots...),
	}
	if len(policy.FrontendRoots) == 0 {
		policy.FrontendRoots = []string{
			"frontend/**",
			"src/**",
			"client/**",
			"public/**",
			"static/**",
			"miniprogram/**",
			"electron/**",
			"pages/**",
			"components/**",
			"utils/**",
			"assets/**",
			"services/**",
			"store/**",
		}
	}
	if len(policy.BackendRoots) == 0 {
		policy.BackendRoots = []string{"server/**", "data/**"}
	}
	if len(policy.PackagingRoots) == 0 {
		policy.PackagingRoots = []string{"electron/**", "package/**", "build/**"}
	}
	return policy.Normalized()
}

func (p PathPolicy) Validate() error {
	for field, values := range map[string][]string{
		"frontend_roots":  p.FrontendRoots,
		"backend_roots":   p.BackendRoots,
		"packaging_roots": p.PackagingRoots,
	} {
		for _, value := range values {
			if !safePathPolicyEntry(value) {
				return fmt.Errorf("invalid_path_policy: path_policy.%s contains unsafe entry %q", field, value)
			}
		}
	}
	return nil
}

func (p PathPolicy) Normalized() PathPolicy {
	return PathPolicy{
		FrontendRoots:  normalizePathPolicyEntries(p.FrontendRoots),
		BackendRoots:   normalizePathPolicyEntries(p.BackendRoots),
		PackagingRoots: normalizePathPolicyEntries(p.PackagingRoots),
	}
}

func normalizePathPolicyEntries(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		normalized := normalizePathPolicyEntry(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizePathPolicyEntry(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	for strings.HasPrefix(value, "./") {
		value = strings.TrimPrefix(value, "./")
	}
	if strings.HasSuffix(value, "/") {
		value += "**"
	}
	return value
}

func safePathPolicyEntry(value string) bool {
	value = normalizePathPolicyEntry(value)
	if value == "" || strings.ContainsAny(strings.TrimSuffix(value, "/**"), "*?[]{}") {
		return false
	}
	return isSafeRelativePattern(value)
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
  - "path_policy": 对象，用来声明本项目的路径语义，供后续模块拆分判断哪些路径属于前端、后端或打包配置
    - "frontend_roots": 字符串数组，前端可拥有的仓库相对路径；可写具体文件如 "index.html"，也可写目录 glob 如 "src/**"、"electron/**"、"desktop/**"
    - "backend_roots": 字符串数组，后端可拥有的仓库相对路径；例如 "server/**"、"api/**"、"data/**"
    - "packaging_roots": 字符串数组，前端交付/桌面壳/打包配置可拥有的仓库相对路径；例如 "electron/**"、"package/**"、"build/**"
- path_policy 中的路径必须是仓库内相对路径，不能是绝对路径，不能逃逸到上级目录，不能写入 .git/；目录范围必须写成以 "/**" 结尾的 glob。
- 请根据 architecture_v1.md 的实际项目结构选择 path_policy，不要把所有示例路径都机械照抄。
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
  "default_test_command": "npm test",
  "path_policy": {
    "frontend_roots": ["src/**", "public/**", "index.html", "style.css", "game.js"],
    "backend_roots": ["server/**", "data/**"],
    "packaging_roots": ["electron/**", "package/**", "build/**"]
  }
}`)
}
