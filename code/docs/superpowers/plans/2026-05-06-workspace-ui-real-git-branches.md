# Workspace UI Real Git Branches Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将运行工作台 UI 调整为参考图的一屏式布局，并在 Pipeline overview 中展示来自真实 DoujiaGit bag artifact 的 Git branches 信息。

**Architecture:** 前端只调整 `RunWorkspacePage` 相关布局、卡片和样式，保留现有聊天、pipeline 数据拉取、节点点击、Step 操作流程。Git branches 新增一个 Go 侧只读聚合 API，从当前 run 的 task snapshot output bags 读取 `coder_branch` 和 `merged_main_branch` artifact JSON，前端不 mock、不猜测、不生成虚假的 branch/commit/author/message/time。没有真实 artifact 时返回空数组和可展示 warnings，UI 显示空状态。

**Tech Stack:** React 19, Vite, TypeScript, Tailwind CSS, @xyflow/react, Go devflow HTTP API, DoujiaGit repository, `artifact.ScopedLocalStore`.

---

## Hard Requirements

- 不重写 pipeline、聊天、节点详情、Step、Docker 写代码流程。
- Git branches 必须全部来自真实数据：DoujiaGit snapshot output bag 中的 `coder_branch` 和 `merged_main_branch` artifact。
- 禁止前端 mock 分支、commit、author、message、time。
- 截图里的 `Author / Message / Time` 不是硬字段；如果 artifact 没有真实值就不要展示这些列。
- 允许展示真实存在的替代字段：`module_id`、`module_name`、`agent_id`、`task_id`、`snapshot_id`、`bag_id`、`logical_key`、`branch`、`commit`、`base_branch`、`base_commit`、`merged_commit`、`applied_commits`、`container_id`、`changed_files`、`result`、`test_passed`、`created_at`。
- 第二张图底部的 overview 长列表必须从默认界面移除：`Acceptance checkpoint`、`Iteration completed`、`conversation_memory`、`requirement_summary`、`phase_two_delivery_flow completed`、`pipeline_module completed`、`pipeline_global_test_data completed`、`pipeline_write_code completed`、`pipeline_write_test_data completed` 不再作为列表展示。
- 桌面宽屏 1440px 以上优先：sidebar 固定，Doujia 对话卡片约 430-480px，右侧区域占剩余宽度，右下 Pipeline overview 足够大。

## Real Git Branches Data Source

真实数据链路必须是：

```text
run_id
  -> TaskRepository.ListByRun(run_id)
  -> doujiagit.BuildTaskSnapshotDetail(run_id, task_id)
  -> snapshot.output_bags
  -> bag.versions
  -> version.logical_artifact.logical_key in ["coder_branch", "merged_main_branch"]
  -> version.objects[].storage_uri
  -> artifact.ScopedLocalStore.Read(storage_uri)
  -> parse JSON artifact
  -> API response for UI
```

当前已确认的真实 artifact 形态：

`coder_branch.json`:

```json
{
  "kind": "coder_branch",
  "module_id": "module01",
  "container_id": "c3398d08d19d573c64af8e922d4271b0512aefc6edc314afb51fa1099edba4b5",
  "repo_dir": "/workspace/repo",
  "base_branch": "main",
  "base_commit": "63d766a7a175ab6b0d00d1a6b07b5f7ff884d801",
  "branch": "feature/module01-frontend",
  "commit": "3e9292f44cfcdc6b775bf9a5610f44b93911c139",
  "worktree": "/workspace/worktrees/module01",
  "changed_files": ["index.html"],
  "test_command": "echo \"frontend seed tests pending\" && exit 0",
  "result": "kok",
  "test_passed": true
}
```

`merged_main_branch.json`:

```json
{
  "kind": "merged_main_branch",
  "base_branch": "main",
  "container_id": "c3398d08d19d573c64af8e922d4271b0512aefc6edc314afb51fa1099edba4b5",
  "merged_commit": "3697c8327d647eff207b1cdfd1c391027617dd8a",
  "applied_commits": [
    "3e9292f44cfcdc6b775bf9a5610f44b93911c139",
    "cc2b1627628705b2bface7015ab119c58ec53b0a"
  ],
  "modules": [
    {
      "base_branch": "main",
      "base_commit": "63d766a7a175ab6b0d00d1a6b07b5f7ff884d801",
      "branch": "feature/module01-frontend",
      "commit": "3e9292f44cfcdc6b775bf9a5610f44b93911c139",
      "module_id": "module01",
      "module_name": "Snake Game Frontend"
    }
  ],
  "repo_dir": "/workspace/repo",
  "result": "kok",
  "schema_version": 2
}
```

这些 JSON 只是当前环境中真实 artifact 的例子，代码必须按实际 artifact 字段解析；如果某个字段缺失，响应中保持空值或省略，不补假值。

## File Structure

Backend:

- Modify: `D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia\internal\app\api.go`
  - 新增 Git branches view structs。
  - 新增 `GET /api/runs/{run_id}/git-branches` route。
  - 新增 `handleGitBranches` 和只读聚合函数。
- Modify: `D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia\internal\app\http_test.go`
  - 增加接口级测试：有真实 artifact 时返回真实字段；没有 artifact 时返回空数组。

Frontend shared/API:

- Modify: `D:\project\Doujia-project-package-2026-05-06\code\shared\devflow-api.ts`
  - 新增 `DevflowGitBranchesView`、`DevflowGitModuleBranchView`、`DevflowGitMergeView`、`DevflowGitBranchesWarningView`。
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\api\devflow-client.ts`
  - 新增 `getDevflowGitBranches(runId)`。

Frontend UI:

- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\RunWorkspacePage.tsx`
  - 将页面主体改为窄 Doujia 对话卡片 + 右侧主列。
  - 右侧主列上方渲染 Main pipeline，下方渲染新的 Pipeline overview。
  - 移除默认页面中的 `WorkspaceTabs` overview 长列表展示。
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\PipelineView.tsx`
  - 将 Main pipeline 卡片高度、边框、头部、统计条调整为参考图风格。
  - 保留现有 graph、节点点击、节点详情、Step 行为。
- Create: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\PipelineOverviewPanel.tsx`
  - 外层 `Pipeline overview` 卡片。
  - 内部左侧 `Current pipeline overview`，右侧 `Git branches`。
- Create: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\GitBranchesPanel.tsx`
  - 渲染真实 Git branches 表格和简化分支图。
  - 没有真实数据时渲染空状态。

---

## Task 1: Backend API Contract for Real Git Branches

**Files:**
- Modify: `D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia\internal\app\api.go`

- [ ] **Step 1: Add response view structs near existing API view structs**

Add these structs in `api.go` near the other `*View` structs:

```go
type gitBranchesView struct {
	RunID          core.RunID             `json:"run_id"`
	BaseBranch    string                 `json:"base_branch,omitempty"`
	BaseCommit    string                 `json:"base_commit,omitempty"`
	ContainerID   string                 `json:"container_id,omitempty"`
	ModuleBranches []gitModuleBranchView `json:"module_branches"`
	Merge         *gitMergeView          `json:"merge,omitempty"`
	Warnings      []gitBranchesWarningView `json:"warnings,omitempty"`
}

type gitModuleBranchView struct {
	TaskID       core.TaskID `json:"task_id,omitempty"`
	AgentID      core.AgentID `json:"agent_id,omitempty"`
	SnapshotID   string `json:"snapshot_id,omitempty"`
	BagID        string `json:"bag_id,omitempty"`
	LogicalKey   string `json:"logical_key,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	ModuleID     string `json:"module_id,omitempty"`
	ModuleName   string `json:"module_name,omitempty"`
	Branch       string `json:"branch,omitempty"`
	Commit       string `json:"commit,omitempty"`
	BaseBranch   string `json:"base_branch,omitempty"`
	BaseCommit   string `json:"base_commit,omitempty"`
	ContainerID  string `json:"container_id,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Result       string `json:"result,omitempty"`
	TestPassed   *bool `json:"test_passed,omitempty"`
}

type gitMergeView struct {
	TaskID         core.TaskID `json:"task_id,omitempty"`
	AgentID        core.AgentID `json:"agent_id,omitempty"`
	SnapshotID     string `json:"snapshot_id,omitempty"`
	BagID          string `json:"bag_id,omitempty"`
	LogicalKey     string `json:"logical_key,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	BaseBranch    string `json:"base_branch,omitempty"`
	BaseCommit    string `json:"base_commit,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	MergedCommit  string `json:"merged_commit,omitempty"`
	AppliedCommits []string `json:"applied_commits,omitempty"`
	Modules       []gitModuleBranchView `json:"modules,omitempty"`
	Result        string `json:"result,omitempty"`
}

type gitBranchesWarningView struct {
	TaskID     core.TaskID `json:"task_id,omitempty"`
	BagID      string `json:"bag_id,omitempty"`
	LogicalKey string `json:"logical_key,omitempty"`
	Message    string `json:"message"`
}
```

If `gofmt` later aligns fields differently, accept the formatter output.

- [ ] **Step 2: Add artifact JSON helper structs**

Add private structs in the same file:

```go
type coderBranchArtifact struct {
	Kind         string   `json:"kind"`
	ModuleID     string   `json:"module_id"`
	ModuleName   string   `json:"module_name"`
	ContainerID  string   `json:"container_id"`
	RepoDir      string   `json:"repo_dir"`
	BaseBranch   string   `json:"base_branch"`
	BaseCommit   string   `json:"base_commit"`
	Branch       string   `json:"branch"`
	Commit       string   `json:"commit"`
	Worktree     string   `json:"worktree"`
	ChangedFiles []string `json:"changed_files"`
	Result       string   `json:"result"`
	TestPassed   *bool    `json:"test_passed"`
}

type mergedMainBranchArtifact struct {
	Kind           string                `json:"kind"`
	BaseBranch     string                `json:"base_branch"`
	BaseCommit     string                `json:"base_commit"`
	ContainerID    string                `json:"container_id"`
	MergedCommit   string                `json:"merged_commit"`
	AppliedCommits []string              `json:"applied_commits"`
	Modules        []coderBranchArtifact `json:"modules"`
	RepoDir         string                `json:"repo_dir"`
	Result         string                `json:"result"`
	SchemaVersion  int                   `json:"schema_version"`
}
```

- [ ] **Step 3: Add route**

In `NewHTTPHandler` route switch for `/api/runs/...`, add a GET case near `pipeline-graph` and `nodes`:

```go
case len(parts) == 3 && parts[2] == "git-branches":
	requireMethod(w, r, http.MethodGet, func() {
		b.handleGitBranches(w, r, runID)
	})
```

- [ ] **Step 4: Add handler**

Add:

```go
func (b *Bootstrap) handleGitBranches(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	view, err := b.buildGitBranchesView(r.Context(), runID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, view)
}
```

- [ ] **Step 5: Implement real bag artifact aggregation**

Add this function. It must read only DoujiaGit bag objects; do not scan `runtime/workspaces` directories as the primary source.

```go
func (b *Bootstrap) buildGitBranchesView(ctx context.Context, runID core.RunID) (gitBranchesView, error) {
	run, err := b.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return gitBranchesView{}, err
	}
	tasks, err := b.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		return gitBranchesView{}, err
	}
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: run.ProjectDir,
	}
	view := gitBranchesView{
		RunID:           runID,
		ModuleBranches: make([]gitModuleBranchView, 0),
		Warnings:       make([]gitBranchesWarningView, 0),
	}

	for _, task := range tasks {
		snapshot, err := doujiagit.BuildTaskSnapshotDetail(ctx, b.Internals.DoujiaGitRepository, runID, task.ID)
		if err != nil {
			continue
		}
		for _, bag := range snapshot.OutputBags {
			for _, version := range bag.Versions {
				logicalKey := strings.TrimSpace(version.LogicalArtifact.LogicalKey)
				if logicalKey != "coder_branch" && logicalKey != "merged_main_branch" {
					continue
				}
				for _, object := range version.Objects {
					if strings.TrimSpace(object.StorageURI) == "" {
						view.Warnings = append(view.Warnings, gitBranchesWarningView{
							TaskID:     snapshot.TaskID,
							BagID:      bag.BagID,
							LogicalKey: logicalKey,
							Message:    "artifact object has no storage_uri",
						})
						continue
					}
					content, err := store.Read(ctx, object.StorageURI)
					if err != nil {
						view.Warnings = append(view.Warnings, gitBranchesWarningView{
							TaskID:     snapshot.TaskID,
							BagID:      bag.BagID,
							LogicalKey: logicalKey,
							Message:    err.Error(),
						})
						continue
					}
					switch logicalKey {
					case "coder_branch":
						var payload coderBranchArtifact
						if err := json.Unmarshal(content, &payload); err != nil {
							view.Warnings = append(view.Warnings, gitBranchesWarningView{TaskID: snapshot.TaskID, BagID: bag.BagID, LogicalKey: logicalKey, Message: err.Error()})
							continue
						}
						row := gitModuleBranchView{
							TaskID:       snapshot.TaskID,
							AgentID:      snapshot.AgentID,
							SnapshotID:   snapshot.SnapshotID,
							BagID:        bag.BagID,
							LogicalKey:   logicalKey,
							CreatedAt:    snapshot.CreatedAt,
							ModuleID:     payload.ModuleID,
							ModuleName:   payload.ModuleName,
							Branch:       payload.Branch,
							Commit:       payload.Commit,
							BaseBranch:   payload.BaseBranch,
							BaseCommit:   payload.BaseCommit,
							ContainerID:  payload.ContainerID,
							ChangedFiles: append([]string(nil), payload.ChangedFiles...),
							Result:       payload.Result,
							TestPassed:   payload.TestPassed,
						}
						view.ModuleBranches = append(view.ModuleBranches, row)
						if view.BaseBranch == "" {
							view.BaseBranch = row.BaseBranch
						}
						if view.BaseCommit == "" {
							view.BaseCommit = row.BaseCommit
						}
						if view.ContainerID == "" {
							view.ContainerID = row.ContainerID
						}
					case "merged_main_branch":
						var payload mergedMainBranchArtifact
						if err := json.Unmarshal(content, &payload); err != nil {
							view.Warnings = append(view.Warnings, gitBranchesWarningView{TaskID: snapshot.TaskID, BagID: bag.BagID, LogicalKey: logicalKey, Message: err.Error()})
							continue
						}
						merge := gitMergeView{
							TaskID:         snapshot.TaskID,
							AgentID:        snapshot.AgentID,
							SnapshotID:     snapshot.SnapshotID,
							BagID:          bag.BagID,
							LogicalKey:      logicalKey,
							CreatedAt:       snapshot.CreatedAt,
							BaseBranch:      payload.BaseBranch,
							BaseCommit:      payload.BaseCommit,
							ContainerID:     payload.ContainerID,
							MergedCommit:    payload.MergedCommit,
							AppliedCommits: append([]string(nil), payload.AppliedCommits...),
							Result:          payload.Result,
							Modules:        make([]gitModuleBranchView, 0, len(payload.Modules)),
						}
						for _, module := range payload.Modules {
							merge.Modules = append(merge.Modules, gitModuleBranchView{
								TaskID:      snapshot.TaskID,
								AgentID:     snapshot.AgentID,
								SnapshotID:  snapshot.SnapshotID,
								BagID:       bag.BagID,
								LogicalKey:  logicalKey,
								CreatedAt:   snapshot.CreatedAt,
								ModuleID:    module.ModuleID,
								ModuleName:  module.ModuleName,
								Branch:      module.Branch,
								Commit:      module.Commit,
								BaseBranch:  module.BaseBranch,
								BaseCommit:  module.BaseCommit,
								ContainerID: payload.ContainerID,
							})
						}
						view.Merge = &merge
						if view.BaseBranch == "" {
							view.BaseBranch = merge.BaseBranch
						}
						if view.BaseCommit == "" {
							view.BaseCommit = merge.BaseCommit
						}
						if view.ContainerID == "" {
							view.ContainerID = merge.ContainerID
						}
					}
				}
			}
		}
	}
	return view, nil
}
```

- [ ] **Step 6: Run formatter**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
gofmt -w internal/app/api.go
```

Expected: no output.

---

## Task 2: Backend Tests for Real Git Branches

**Files:**
- Modify: `D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia\internal\app\http_test.go`

- [ ] **Step 1: Add test for empty real data**

Add this test near the other API route tests:

```go
func TestBootstrapHTTPHandlerServesEmptyGitBranchesForRunWithoutArtifacts(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_git_branches_empty")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_git_branches_empty/git-branches", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET git branches status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{`"run_id":"run_git_branches_empty"`, `"module_branches":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET empty git branches body missing %s:\n%s", want, body)
		}
	}
}
```

- [ ] **Step 2: Add seed helper for real bag artifacts**

Add a helper that creates real DoujiaGit logical artifacts, objects, versions, bags and snapshots. Use existing repository APIs in `seedAppHTTPGraph` as the local pattern. The helper writes artifact JSON to the run project directory and stores its `storage_uri` on `doujiagit.ArtifactObject`.

```go
func seedAppHTTPGitBranches(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: run.ProjectDir,
	}
	writeJSON := func(uri string, value any) []byte {
		t.Helper()
		content, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal artifact: %v", err)
		}
		if err := store.Write(ctx, uri, content); err != nil {
			t.Fatalf("write artifact %s: %v", uri, err)
		}
		return content
	}

	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	modulePayload := coderBranchArtifact{
		Kind:         "coder_branch",
		ModuleID:     "module01",
		ModuleName:   "Snake Game Frontend",
		ContainerID:  "container-real-01",
		BaseBranch:   "main",
		BaseCommit:   "basecommit123",
		Branch:       "feature/module01-frontend",
		Commit:       "commitmodule01",
		ChangedFiles: []string{"index.html"},
		Result:       "kok",
	}
	testPassed := true
	modulePayload.TestPassed = &testPassed
	moduleURI := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "coder01", "artifacts", "task_write_code", "coder_branch.json"))
	moduleContent := writeJSON(moduleURI, modulePayload)
	moduleObjectID := doujiagit.StableObjectID(moduleContent)
	moduleLogicalID := doujiagit.StableLogicalArtifactID(runID, "coder01", "coder_branch")
	moduleVersionID := doujiagit.StableArtifactVersionID(moduleLogicalID, []string{moduleObjectID})
	moduleSnapshotID := doujiagit.StableSnapshotID(runID, "task_write_code", now)
	moduleBagID := doujiagit.StableBagID(runID, moduleSnapshotID, "coder_branch")

	repository := bootstrap.Internals.DoujiaGitRepository
	if err := repository.CreateLogicalArtifact(ctx, doujiagit.LogicalArtifact{LogicalArtifactID: moduleLogicalID, RunID: runID, Namespace: "coder01", LogicalKey: "coder_branch", CreatedAt: now}); err != nil {
		t.Fatalf("create module logical artifact: %v", err)
	}
	if err := repository.CreateObject(ctx, doujiagit.ArtifactObject{ObjectID: moduleObjectID, ObjectType: doujiagit.ObjectTypeBlob, StorageURI: moduleURI, CreatedAt: now}); err != nil {
		t.Fatalf("create module object: %v", err)
	}
	if err := repository.CreateArtifactVersion(ctx, doujiagit.ArtifactVersion{ArtifactVersionID: moduleVersionID, LogicalArtifactID: moduleLogicalID, ObjectIDs: []string{moduleObjectID}, CreatedAt: now}); err != nil {
		t.Fatalf("create module version: %v", err)
	}
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{BagID: moduleBagID, RunID: runID, ArtifactVersionIDs: []string{moduleVersionID}, CreatedAt: now}); err != nil {
		t.Fatalf("create module bag: %v", err)
	}
	if err := repository.CreateSnapshot(ctx, doujiagit.TaskSnapshot{SnapshotID: moduleSnapshotID, RunID: runID, TaskID: "task_write_code", AgentRole: "coder", AgentID: "coder01", Op: "write_code", Result: core.TaskResultOK, OutputBagIDs: []string{moduleBagID}, CreatedAt: now}); err != nil {
		t.Fatalf("create module snapshot: %v", err)
	}

	mergePayload := mergedMainBranchArtifact{
		Kind:           "merged_main_branch",
		BaseBranch:     "main",
		BaseCommit:     "basecommit123",
		ContainerID:    "container-real-01",
		MergedCommit:   "mergedcommit123",
		AppliedCommits: []string{"commitmodule01"},
		Modules:        []coderBranchArtifact{modulePayload},
		Result:         "kok",
		SchemaVersion:  2,
	}
	mergeURI := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "architect01", "artifacts", "task_merge_code", "merged_main_branch.json"))
	mergeContent := writeJSON(mergeURI, mergePayload)
	mergeObjectID := doujiagit.StableObjectID(mergeContent)
	mergeLogicalID := doujiagit.StableLogicalArtifactID(runID, "architect01", "merged_main_branch")
	mergeVersionID := doujiagit.StableArtifactVersionID(mergeLogicalID, []string{mergeObjectID})
	mergeSnapshotID := doujiagit.StableSnapshotID(runID, "task_merge_code", now.Add(time.Minute))
	mergeBagID := doujiagit.StableBagID(runID, mergeSnapshotID, "merged_main_branch")
	if err := repository.CreateLogicalArtifact(ctx, doujiagit.LogicalArtifact{LogicalArtifactID: mergeLogicalID, RunID: runID, Namespace: "architect01", LogicalKey: "merged_main_branch", CreatedAt: now}); err != nil {
		t.Fatalf("create merge logical artifact: %v", err)
	}
	if err := repository.CreateObject(ctx, doujiagit.ArtifactObject{ObjectID: mergeObjectID, ObjectType: doujiagit.ObjectTypeBlob, StorageURI: mergeURI, CreatedAt: now}); err != nil {
		t.Fatalf("create merge object: %v", err)
	}
	if err := repository.CreateArtifactVersion(ctx, doujiagit.ArtifactVersion{ArtifactVersionID: mergeVersionID, LogicalArtifactID: mergeLogicalID, ObjectIDs: []string{mergeObjectID}, CreatedAt: now}); err != nil {
		t.Fatalf("create merge version: %v", err)
	}
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{BagID: mergeBagID, RunID: runID, ArtifactVersionIDs: []string{mergeVersionID}, CreatedAt: now}); err != nil {
		t.Fatalf("create merge bag: %v", err)
	}
	if err := repository.CreateSnapshot(ctx, doujiagit.TaskSnapshot{SnapshotID: mergeSnapshotID, RunID: runID, TaskID: "task_merge_code", AgentRole: "architect", AgentID: "architect01", Op: "merge_code", Result: core.TaskResultOK, OutputBagIDs: []string{mergeBagID}, CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("create merge snapshot: %v", err)
	}
}
```

- [ ] **Step 3: Ensure imports compile**

The helper uses `artifact.ScopedLocalStore`, so add this import if missing:

```go
"devflow/internal/artifact"
```

- [ ] **Step 4: Add test for real artifact response**

```go
func TestBootstrapHTTPHandlerServesRealGitBranchesFromDoujiaGitBags(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_git_branches_real")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	if err := bootstrap.Internals.TaskRepository.Create(ctx, core.Task{ID: "task_write_code", RunID: runID, AgentID: "coder01", Status: core.TaskStatusDone, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("create write task: %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, core.Task{ID: "task_merge_code", RunID: runID, AgentID: "architect01", Status: core.TaskStatusDone, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("create merge task: %v", err)
	}
	seedAppHTTPGitBranches(t, ctx, bootstrap, runID)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_git_branches_real/git-branches", nil)
	resp := httptest.NewRecorder()
	bootstrap.NewHTTPHandler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET git branches status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"branch":"feature/module01-frontend"`,
		`"commit":"commitmodule01"`,
		`"module_id":"module01"`,
		`"module_name":"Snake Game Frontend"`,
		`"container_id":"container-real-01"`,
		`"merged_commit":"mergedcommit123"`,
		`"applied_commits":["commitmodule01"]`,
		`"bag_id":"bag:`,
		`"logical_key":"coder_branch"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET git branches body missing %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "meilee") || strings.Contains(body, "coder-bot") || strings.Contains(body, "fix: issue") {
		t.Fatalf("GET git branches body contains screenshot/mock data:\n%s", body)
	}
}
```

- [ ] **Step 5: Run backend tests**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
go test ./internal/app
```

Expected: PASS.

If the test compile fails because `core.Task` field names differ, inspect `D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia\internal\core\*.go` and update only the test seed struct to match existing `core.Task`.

---

## Task 3: Frontend Shared Types and API Client

**Files:**
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\shared\devflow-api.ts`
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\api\devflow-client.ts`

- [ ] **Step 1: Add shared response types**

In `devflow-api.ts`, add near the DoujiaGit graph types:

```ts
export interface DevflowGitBranchesWarningView {
  task_id?: string;
  bag_id?: string;
  logical_key?: string;
  message: string;
}

export interface DevflowGitModuleBranchView {
  task_id?: string;
  agent_id?: string;
  snapshot_id?: string;
  bag_id?: string;
  logical_key?: string;
  created_at?: string;
  module_id?: string;
  module_name?: string;
  branch?: string;
  commit?: string;
  base_branch?: string;
  base_commit?: string;
  container_id?: string;
  changed_files?: string[];
  result?: string;
  test_passed?: boolean;
}

export interface DevflowGitMergeView {
  task_id?: string;
  agent_id?: string;
  snapshot_id?: string;
  bag_id?: string;
  logical_key?: string;
  created_at?: string;
  base_branch?: string;
  base_commit?: string;
  container_id?: string;
  merged_commit?: string;
  applied_commits?: string[];
  modules?: DevflowGitModuleBranchView[];
  result?: string;
}

export interface DevflowGitBranchesView {
  run_id: string;
  base_branch?: string;
  base_commit?: string;
  container_id?: string;
  module_branches: DevflowGitModuleBranchView[];
  merge?: DevflowGitMergeView;
  warnings?: DevflowGitBranchesWarningView[];
}
```

- [ ] **Step 2: Import the type in client API**

In `devflow-client.ts`, add `DevflowGitBranchesView` to the existing import list from `shared/devflow-api`.

- [ ] **Step 3: Add API function**

Add near `getDevflowPipelineGraph`:

```ts
export async function getDevflowGitBranches(
  runId: string,
): Promise<DevflowGitBranchesView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/git-branches`);
}
```

- [ ] **Step 4: Run client typecheck**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
npm run type:check:client
```

Expected: PASS.

---

## Task 4: Build Git Branches Panel from Real API Data

**Files:**
- Create: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\GitBranchesPanel.tsx`

- [ ] **Step 1: Create the component**

Create `GitBranchesPanel.tsx` with this structure:

```tsx
import type { DevflowGitBranchesView, DevflowGitModuleBranchView } from '@shared/devflow-api';

interface GitBranchesPanelProps {
  data?: DevflowGitBranchesView | null;
  loading?: boolean;
}

const branchColors = ['#7c3aed', '#2563eb', '#16a34a', '#f97316', '#dc2626'];

function shortSha(value?: string): string {
  if (!value) return '';
  return value.length > 10 ? `${value.slice(0, 10)}` : value;
}

function moduleLabel(row: DevflowGitModuleBranchView): string {
  return row.module_name || row.module_id || row.agent_id || row.task_id || 'Unknown module';
}

function resultBadgeClass(result?: string, testPassed?: boolean): string {
  if (testPassed === false || result === 'failed' || result === 'error') {
    return 'border-red-200 bg-red-50 text-red-700';
  }
  if (testPassed === true || result === 'kok' || result === 'ok' || result === 'completed') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700';
  }
  return 'border-slate-200 bg-slate-50 text-slate-600';
}

function buildRows(data?: DevflowGitBranchesView | null): DevflowGitModuleBranchView[] {
  if (!data) return [];
  const rows = [...(data.module_branches ?? [])];
  const existingCommits = new Set(rows.map((item) => item.commit).filter(Boolean));
  for (const item of data.merge?.modules ?? []) {
    if (item.commit && existingCommits.has(item.commit)) continue;
    rows.push(item);
  }
  return rows;
}

function MiniBranchGraph({ rowIndex, color }: { rowIndex: number; color: string }) {
  const lane = 16 + (rowIndex % 4) * 12;
  const branchLane = 54;
  return (
    <svg width="78" height="40" viewBox="0 0 78 40" aria-hidden="true" className="block">
      <line x1={lane} y1="0" x2={lane} y2="40" stroke={color} strokeWidth="2" />
      {rowIndex > 0 ? (
        <path
          d={`M ${lane} 20 C ${lane + 12} 20, ${branchLane - 12} 12, ${branchLane} 12`}
          fill="none"
          stroke={color}
          strokeWidth="2"
        />
      ) : null}
      <circle cx={lane} cy="20" r="4" fill={color} />
      {rowIndex > 0 ? <circle cx={branchLane} cy="12" r="3" fill="#fff" stroke={color} strokeWidth="2" /> : null}
    </svg>
  );
}

export function GitBranchesPanel({ data, loading }: GitBranchesPanelProps) {
  const rows = buildRows(data);
  const merge = data?.merge;

  return (
    <section className="flex h-full min-h-0 flex-col rounded-lg border border-slate-200 bg-white">
      <div className="flex items-center justify-between border-b border-slate-100 px-4 py-3">
        <div>
          <h3 className="text-sm font-semibold text-slate-950">Git branches</h3>
          <p className="mt-0.5 text-xs text-slate-500">From real DoujiaGit output bags</p>
        </div>
        {merge?.merged_commit ? (
          <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700">
            merged {shortSha(merge.merged_commit)}
          </span>
        ) : null}
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {loading ? (
          <div className="p-6 text-sm text-slate-500">Loading real branch data...</div>
        ) : rows.length === 0 && !merge ? (
          <div className="flex min-h-[220px] items-center justify-center px-6 text-center">
            <div>
              <div className="text-sm font-medium text-slate-800">No real branch artifacts yet</div>
              <div className="mt-1 max-w-sm text-xs leading-5 text-slate-500">
                Git branches will appear after write_code or merge_code nodes produce coder_branch or merged_main_branch bags.
              </div>
            </div>
          </div>
        ) : (
          <table className="w-full min-w-[760px] table-fixed border-collapse text-left text-xs">
            <thead className="sticky top-0 z-10 bg-slate-50 text-[11px] uppercase tracking-wide text-slate-500">
              <tr>
                <th className="w-24 border-b border-slate-200 px-4 py-2 font-semibold">Graph</th>
                <th className="w-52 border-b border-slate-200 px-3 py-2 font-semibold">Branch / Commit</th>
                <th className="w-44 border-b border-slate-200 px-3 py-2 font-semibold">Module</th>
                <th className="w-44 border-b border-slate-200 px-3 py-2 font-semibold">Container</th>
                <th className="w-32 border-b border-slate-200 px-3 py-2 font-semibold">Result</th>
                <th className="w-44 border-b border-slate-200 px-3 py-2 font-semibold">Task / Bag</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, index) => {
                const color = branchColors[index % branchColors.length];
                return (
                  <tr key={`${row.bag_id ?? row.task_id ?? index}-${row.commit ?? row.branch ?? index}`} className="border-b border-slate-100">
                    <td className="px-4 py-3 align-middle">
                      <MiniBranchGraph rowIndex={index} color={color} />
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.branch ? (
                        <span className="inline-flex max-w-full items-center rounded-md border border-blue-200 bg-blue-50 px-2 py-1 font-medium text-blue-700">
                          <span className="truncate">{row.branch}</span>
                        </span>
                      ) : null}
                      {row.commit ? <div className="mt-1 font-mono text-[11px] text-slate-600">{shortSha(row.commit)}</div> : null}
                      {row.base_branch || row.base_commit ? (
                        <div className="mt-1 text-[11px] text-slate-400">
                          base {row.base_branch || ''} {shortSha(row.base_commit)}
                        </div>
                      ) : null}
                    </td>
                    <td className="px-3 py-3 align-top">
                      <div className="font-medium text-slate-800">{moduleLabel(row)}</div>
                      {row.module_id ? <div className="mt-1 font-mono text-[11px] text-slate-400">{row.module_id}</div> : null}
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.container_id ? <div className="truncate font-mono text-[11px] text-slate-600">{shortSha(row.container_id)}</div> : <span className="text-slate-300">-</span>}
                      {row.changed_files?.length ? <div className="mt-1 text-[11px] text-slate-400">{row.changed_files.length} files</div> : null}
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.result || typeof row.test_passed === 'boolean' ? (
                        <span className={`inline-flex rounded-full border px-2 py-1 text-[11px] font-medium ${resultBadgeClass(row.result, row.test_passed)}`}>
                          {row.result || (row.test_passed ? 'test passed' : 'test failed')}
                        </span>
                      ) : (
                        <span className="text-slate-300">-</span>
                      )}
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.task_id ? <div className="truncate font-mono text-[11px] text-slate-600">{row.task_id}</div> : null}
                      {row.bag_id ? <div className="mt-1 truncate font-mono text-[11px] text-slate-400">{row.bag_id}</div> : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      <div className="flex items-center justify-between gap-3 border-t border-slate-100 px-4 py-3 text-xs text-slate-500">
        <div className="flex min-w-0 flex-wrap gap-3">
          {rows.slice(0, 5).map((row, index) => (
            <span key={`${row.branch ?? row.commit ?? index}-legend`} className="inline-flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full" style={{ backgroundColor: branchColors[index % branchColors.length] }} />
              <span className="max-w-[160px] truncate">{row.branch || row.module_id || row.agent_id || 'branch'}</span>
            </span>
          ))}
        </div>
        <div className="shrink-0 font-medium text-slate-700">
          {rows.length} branches{merge?.applied_commits?.length ? ` / ${merge.applied_commits.length} applied commits` : ''}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Verify no fake screenshot fields are present**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
Select-String -Path client/src/pages/RunWorkspacePage/GitBranchesPanel.tsx -Pattern 'meilee|coder-bot|worker-bot|fix: issue|2m ago|4m ago'
```

Expected: no matches.

---

## Task 5: Build Fixed Pipeline Overview Card

**Files:**
- Create: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\PipelineOverviewPanel.tsx`

- [ ] **Step 1: Create overview component**

Create `PipelineOverviewPanel.tsx`:

```tsx
import type {
  DevflowGitBranchesView,
  DevflowPipelineWorkspaceView,
  DevflowWorkspaceOverviewView,
} from '@shared/devflow-api';
import { GitBranchesPanel } from './GitBranchesPanel';

interface PipelineOverviewPanelProps {
  workspaceOverview?: DevflowWorkspaceOverviewView | null;
  pipelineWorkspace?: DevflowPipelineWorkspaceView | null;
  gitBranches?: DevflowGitBranchesView | null;
  gitBranchesLoading?: boolean;
}

function statusClass(status?: string): string {
  if (status === 'failed') return 'border-red-200 bg-red-50 text-red-700';
  if (status === 'completed' || status === 'done') return 'border-emerald-200 bg-emerald-50 text-emerald-700';
  if (status === 'running' || status === 'waiting_human' || status === 'dispatched') return 'border-orange-200 bg-orange-50 text-orange-700';
  return 'border-slate-200 bg-slate-50 text-slate-600';
}

function formatDate(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function elapsed(start?: string, end?: string): string {
  if (!start) return '-';
  const startMs = new Date(start).getTime();
  const endMs = end ? new Date(end).getTime() : Date.now();
  if (Number.isNaN(startMs) || Number.isNaN(endMs) || endMs < startMs) return '-';
  const seconds = Math.floor((endMs - startMs) / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  return `${String(hours).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`;
}

function FieldRow({
  label,
  value,
  badge,
}: {
  label: string;
  value?: string | number | null;
  badge?: boolean;
}) {
  return (
    <div className="grid grid-cols-[150px_minmax(0,1fr)] items-start gap-3 border-b border-slate-100 py-3 last:border-b-0">
      <div className="text-xs font-semibold text-slate-600">{label}</div>
      <div className="min-w-0 text-right text-sm text-slate-800">
        {badge ? (
          <span className={`inline-flex rounded-full border px-2 py-1 text-xs font-medium ${statusClass(String(value ?? ''))}`}>
            {value || '-'}
          </span>
        ) : (
          <span className="break-words">{value || '-'}</span>
        )}
      </div>
    </div>
  );
}

export function PipelineOverviewPanel({
  workspaceOverview,
  pipelineWorkspace,
  gitBranches,
  gitBranchesLoading,
}: PipelineOverviewPanelProps) {
  const run = workspaceOverview?.run;
  const currentIteration = workspaceOverview?.current_iteration_no ?? pipelineWorkspace?.current_iteration_no;
  const latestIteration = workspaceOverview?.iterations?.[workspaceOverview.iterations.length - 1];
  const phaseStatus = pipelineWorkspace?.status ?? run?.status;
  const currentPhase =
    pipelineWorkspace?.instances?.find((item) => item.status === 'running' || item.status === 'waiting_human')?.pipeline_id ||
    pipelineWorkspace?.instances?.[0]?.pipeline_id ||
    run?.pipeline_id;
  const conversationSummary =
    workspaceOverview?.session_artifacts?.find((item) => item.kind === 'conversation_memory')?.content ||
    workspaceOverview?.session_artifacts?.find((item) => item.kind === 'requirement_summary')?.content ||
    '-';
  const startedAt = latestIteration?.created_at || run?.created_at;
  const updatedAt = latestIteration?.updated_at || run?.updated_at;

  return (
    <section className="flex min-h-[360px] flex-col rounded-xl border border-slate-200 bg-white shadow-sm">
      <div className="border-b border-slate-100 px-5 py-4">
        <h2 className="text-base font-semibold text-slate-950">Pipeline overview</h2>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-[300px_minmax(0,1fr)] gap-4 p-4">
        <section className="rounded-lg border border-slate-200 bg-white p-4">
          <h3 className="border-b border-slate-100 pb-3 text-sm font-semibold text-slate-900">Current pipeline overview</h3>
          <div className="mt-1">
            <FieldRow label="Current iteration" value={currentIteration ? `Iteration ${currentIteration}` : '-'} />
            <FieldRow label="Iteration status" value={latestIteration?.status || run?.status || '-'} badge />
            <FieldRow label="Conversation summary" value={conversationSummary.length > 80 ? `${conversationSummary.slice(0, 80)}...` : conversationSummary} />
            <FieldRow label="Phase status" value={phaseStatus || '-'} badge />
            <FieldRow label="Current phase" value={currentPhase || '-'} />
            <FieldRow label="Elapsed time" value={elapsed(startedAt, updatedAt)} />
            <FieldRow label="Started at" value={formatDate(startedAt)} />
            <FieldRow label="Last updated" value={formatDate(updatedAt)} />
          </div>
        </section>
        <GitBranchesPanel data={gitBranches} loading={gitBranchesLoading} />
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Check the overview does not render old list labels**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
Select-String -Path client/src/pages/RunWorkspacePage/PipelineOverviewPanel.tsx -Pattern 'Acceptance checkpoint|conversation_memory|pipeline_write_code completed|pipeline_module completed'
```

Expected: no matches, except `conversation_memory` may appear only as an internal artifact kind lookup and must not be visible text.

---

## Task 6: Rework RunWorkspacePage Layout

**Files:**
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\RunWorkspacePage.tsx`

- [ ] **Step 1: Import new API and component**

Add imports:

```tsx
import { getDevflowGitBranches } from '../../api/devflow-client';
import { PipelineOverviewPanel } from './PipelineOverviewPanel';
```

If `devflow-client` imports are grouped differently, add `getDevflowGitBranches` to the existing grouped import.

- [ ] **Step 2: Add state for Git branches**

Near existing workspace/pipeline state:

```tsx
const [gitBranches, setGitBranches] = useState<DevflowGitBranchesView | null>(null);
const [gitBranchesLoading, setGitBranchesLoading] = useState(false);
```

Also import `DevflowGitBranchesView` from `shared/devflow-api`.

- [ ] **Step 3: Fetch real Git branches when run changes**

Add a `useEffect` near the other run data effects:

```tsx
useEffect(() => {
if (!runId) {
    setGitBranches(null);
    return;
  }
  let cancelled = false;
  setGitBranchesLoading(true);
  getDevflowGitBranches(runId)
    .then((data) => {
      if (!cancelled) setGitBranches(data);
    })
    .catch((error) => {
      console.error('Failed to load real git branches', error);
      if (!cancelled) setGitBranches(null);
    })
    .finally(() => {
      if (!cancelled) setGitBranchesLoading(false);
    });
  return () => {
    cancelled = true;
  };
}, [runId]);
```

This may log an error for older backend servers without the new endpoint. During final integration the backend endpoint must exist.

- [ ] **Step 4: Replace wide chat + right panel layout**

Find the current main content area that renders chat on the left and this block on the right:

```tsx
<PipelineView ... />
<WorkspaceTabs ... />
```

Replace that resizable right panel with a fixed desktop layout. Keep the existing left chat header/status/message/composer JSX, but move it inside the left card shown below. Keep the existing `PipelineView` prop names exactly as they are now:

```tsx
<div className="relative flex h-full min-h-0 overflow-hidden bg-slate-50">
  <div className="grid min-h-0 min-w-[1180px] flex-1 grid-cols-[460px_minmax(720px,1fr)] gap-4 p-4 xl:p-6">
    <section className="flex min-h-0 flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
      <div className="shrink-0 border-b border-slate-100 bg-white px-5 py-4">
        {/* move the existing Doujia title, AI assistant badge, project/run and iteration chips here */}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {/* move the existing yellow requirement card and MessageFlow here */}
      </div>
      <div className="shrink-0 border-t border-slate-100 bg-white p-4">
        {/* move the existing SessionComposer here */}
      </div>
    </section>

    <section className="grid min-h-0 grid-rows-[minmax(300px,0.9fr)_minmax(360px,1.1fr)] gap-4">
      <PipelineView
        workspace={pipelineWorkspace}
        onOpenPipeline={handleOpenPipeline}
        onOpenTaskSnapshots={handleOpenNodeSnapshots}
      />
      <PipelineOverviewPanel
        workspaceOverview={workspaceOverview}
        pipelineWorkspace={pipelineWorkspace}
        gitBranches={gitBranches}
        gitBranchesLoading={gitBranchesLoading}
      />
    </section>
  </div>
</div>
```

- [ ] **Step 5: Keep Doujia card contents but narrow them**

Inside the left `section`:

- Keep title text `Doujia 对话` and `AI 助手` badge.
- Keep project and iteration selectors.
- Keep yellow requirement status card.
- Keep existing message bubble rendering and send behavior.
- Make the message list `flex-1 min-h-0 overflow-y-auto`.
- Keep composer fixed at bottom with `border-t border-slate-100 bg-white p-4`.
- User messages remain right aligned blue bubbles.
- Doujia messages remain left aligned white bubbles.

- [ ] **Step 6: Remove default old WorkspaceTabs overview**

Remove the visible `<WorkspaceTabs ... />` call from the default lower right area. Do not delete `WorkspaceTabs.tsx` yet unless no other code imports it.

Also remove any dependency on `workspaceSummary: buildWorkspaceSummary(...)` for the visible overview. It may remain in file only if still used elsewhere, but no UI in the default page may render the old pill/list overview.

- [ ] **Step 7: Run frontend typecheck**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
npm run type:check:client
```

Expected: PASS.

---

## Task 7: Adjust Main Pipeline Card Visuals

**Files:**
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\PipelineView.tsx`

- [ ] **Step 1: Make PipelineView a bounded white card**

Update the outer wrapper classes to match:

```tsx
className="flex min-h-0 flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm"
```

If `PipelineView` already accepts `className`, merge these base classes with the incoming class.

- [ ] **Step 2: Match reference header**

Header should include:

- Left: icon bubble, title `Main pipeline`, current iteration text.
- Right: real environment status badge if already available in props/state; otherwise keep existing status source and label it exactly from real status. Do not create fake realtime state.

Use badge classes:

```tsx
const runningBadge = 'border-orange-200 bg-orange-50 text-orange-700';
const completedBadge = 'border-emerald-200 bg-emerald-50 text-emerald-700';
const failedBadge = 'border-red-200 bg-red-50 text-red-700';
```

- [ ] **Step 3: Reduce graph height**

Set the graph/canvas area to a bounded height similar to:

```tsx
className="min-h-[190px] flex-1"
```

The whole Main pipeline card should sit in the top right row and not push Pipeline overview out of view at 1440px width.

- [ ] **Step 4: Keep node detail panel separate**

The node detail panel may remain on the right side of the Main pipeline card. Default text should be `选择一个节点` with a short explanation. Do not move Pipeline overview content into Main pipeline.

- [ ] **Step 5: Keep stats bar**

Keep existing stats and labels:

```text
Stages / Completed / Running / Failed / Skipped / Duration
```

Use compact `border-t border-slate-100`, small icons or dots, and the real counts already computed by current pipeline logic.

---

## Task 8: Visual Pass for Reference Layout

**Files:**
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\RunWorkspacePage.tsx`
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\PipelineView.tsx`
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\PipelineOverviewPanel.tsx`
- Modify: `D:\project\Doujia-project-package-2026-05-06\code\client\src\pages\RunWorkspacePage\GitBranchesPanel.tsx`

- [ ] **Step 1: Standardize card style**

Use this visual vocabulary consistently:

```text
page background: bg-slate-50
card background: bg-white
card border: border border-slate-200
card radius: rounded-xl for outer cards, rounded-lg for inner cards
shadow: shadow-sm
primary: blue
running: orange
completed: emerald/green
failed: red
text: slate
```

- [ ] **Step 2: Avoid nested card clutter**

Pipeline overview outer card may contain two inner framed sections because the screenshot requires `Current pipeline overview` and `Git branches` inside one card. Do not wrap these in extra decorative cards beyond one inner border each.

- [ ] **Step 3: Desktop dimensions**

At 1440px+:

- Sidebar keeps existing width.
- Doujia card width is visibly narrow, target 430-480px.
- Right column gets the remaining width.
- Main pipeline is top-right.
- Pipeline overview is bottom-right and visually dominant.
- Git branches table has enough room and horizontal scroll only when content truly exceeds available width.

- [ ] **Step 4: Small screen fallback**

For widths below the desktop target, allow:

```tsx
className="min-w-[1180px]"
```

on the main workspace area so the desktop layout is preserved with horizontal scrolling instead of collapsing into a broken layout.

---

## Task 9: End-to-End Verification

**Files:**
- Verify all files modified above.

- [ ] **Step 1: Backend tests**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
go test ./internal/app
```

Expected: PASS.

- [ ] **Step 2: Frontend typecheck**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
npm run type:check:client
```

Expected: PASS.

- [ ] **Step 3: Frontend build**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
npm run build:client
```

Expected: PASS.

- [ ] **Step 4: API manual verification against a real run**

Start or use the existing Go devflow server, then run with a run that has completed write-code/merge-code tasks:

```powershell
Invoke-RestMethod -Uri 'http://127.0.0.1:18080/api/runs/run_429277e43682/git-branches' | ConvertTo-Json -Depth 8
```

Expected shape:

```json
{
  "run_id": "run_429277e43682",
  "base_branch": "main",
  "base_commit": "real value if present",
  "container_id": "real value if present",
  "module_branches": [
    {
      "module_id": "module01",
      "branch": "feature/module01-frontend",
      "commit": "real commit from coder_branch artifact",
      "bag_id": "bag:real id",
      "logical_key": "coder_branch"
    }
  ],
  "merge": {
    "merged_commit": "real commit from merged_main_branch artifact",
    "applied_commits": ["real commits"]
  }
}
```

The exact values depend on the run artifacts. The response must not contain screenshot sample names such as `meilee`, `coder-bot`, `worker-bot`, `fix: issue`, or relative time strings like `2m ago`.

- [ ] **Step 5: Browser visual verification**

Open the workspace page at desktop width at least 1440px.

Acceptance checks:

- Sidebar remains visible with original width.
- Doujia 对话 is a standalone narrow card.
- Main pipeline is top-right and shorter than before.
- Pipeline overview is bottom-right and contains both `Current pipeline overview` and `Git branches` in the same outer card.
- Old long overview pill/list is absent.
- Git branches table shows only true fields returned by `/git-branches`.
- If `/git-branches` returns no branch artifacts, Git branches panel shows a truthful empty state and no fake rows.

- [ ] **Step 6: Search for forbidden mock strings**

Run:

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
Select-String -Path client/src/pages/RunWorkspacePage/*.tsx,shared/devflow-api.ts,client/src/api/devflow-client.ts -Pattern 'meilee|coder-bot|worker-bot|feature/login-optimize|feature/RightFix|arch/refactor-ormexp|fix: issue|2m ago|4m ago'
```

Expected: no matches.

---

## Implementation Notes

- `GET /api/runs/{run_id}/artifacts/{artifact_id}/content` is not the right source for Git branches because artifact IDs can contain `/` and the current route shape only captures one path segment. Use `storage_uri` from DoujiaGit object and read via `artifact.ScopedLocalStore`.
- Directory scanning may be useful only as a debug check during development. The product path must use DoujiaGit bag lineage, otherwise UI cannot prove the branch belongs to the selected run/node.
- `created_at` from snapshot or artifact object is not git commit author time. If shown, label it as task/artifact time.
- Do not add author/message/time columns unless a future real artifact adds those exact fields.
- Keep warnings in the API response for observability, but the UI should not turn warnings into fake branch rows.

## Self-Review Checklist

- [ ] Every UI acceptance point maps to Tasks 4-8.
- [ ] Real Git branches source maps to Tasks 1-2 and forbids mock data.
- [ ] Missing real branch data maps to empty API arrays and empty UI state.
- [ ] Old overview list removal maps to Task 6 Step 6 and Task 9 checks.
- [ ] Verification covers backend tests, frontend typecheck/build, API response, browser layout, and mock-string search.
