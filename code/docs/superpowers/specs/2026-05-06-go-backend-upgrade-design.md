# Go 后端升级合并设计方案

**目标：** 将 `D:\project\Doujia5.0\Doujia4.0\Doujia` 中比当前项目更新的 Go 后端能力，合并到当前实际运行的 Go 后端 `D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia`。

**结论：** 当前项目已经是“前端 + Go 后端”的架构。本次不改架构，只做 Go 后端保守升级。

---

## 1. 当前项目现状

当前项目实际运行链路是：

```text
code/client -> http://127.0.0.1:18080 -> Go devflow-server
```

对应目录：

```text
前端：
D:\project\Doujia-project-package-2026-05-06\code

当前 Go 后端：
D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia

待合并的新 Go 后端：
D:\project\Doujia5.0\Doujia4.0\Doujia
```

`code/server` 目录下存在一套 NestJS 后端代码，但它不是当前主运行后端。当前 `code/vite.config.ts` 默认将 `/api` 代理到 `http://127.0.0.1:18080`，`启动说明.md` 也要求先启动 Go 后端。

---

## 2. 本次合并范围

### 2.1 合并内容

本次只合并 Go 后端源码和相关 Go 测试、后端设计文档。

重点能力包括：

- 前端预览与编辑相关能力
- front agent 增强
- preview handler
- frontend preview manifest schema
- workspace view 更新
- API handler 更新
- orchestrator / run manager 更新
- agent bootstrap / executor 更新
- pipeline spec 更新

### 2.2 不合并内容

以下内容不进入本次合并：

```text
runtime/
runtime/workspaces/
runtime/devflow/state.db
runtime/exports/
devflow-server.exe
临时运行日志
当前项目 code/server NestJS 后端
当前项目 code/client 前端主架构
```

### 2.3 不做的架构变化

本次不做：

- 不把 Go 后端迁移到 `code/server`
- 不让 NestJS 代理 Go 服务
- 不重写 Go 后端为 TypeScript
- 不修改默认前端 API 基址
- 不迁移已有 SQLite 运行数据

---

## 3. 为什么不能整目录覆盖

当前 Go 后端与 `Doujia5.0` Go 后端之间不是简单的“旧版被新版完全包含”关系。

初步差异扫描显示，在排除 `runtime`、可执行文件和运行数据后，两边仍有约 43 个有效差异。部分文件在当前项目中可能包含本地补丁，例如：

```text
internal/app/api.go
internal/app/workspace_views.go
internal/orchestrator/orchestrator.go
```

如果直接整目录覆盖，可能导致当前前端正在使用的接口丢失，或者覆盖当前项目已有修复。

因此合并策略是：

```text
新增文件：直接引入
变更文件：逐个手工合并
运行数据：完全不碰
```

---

## 4. 文件级修改方案

### 4.1 低风险新增文件

优先从 `D:\project\Doujia5.0\Doujia4.0\Doujia` 复制以下新增能力文件到当前 Go 后端：

```text
internal\agent\handler\preview_handlers.go
internal\agent\handler\preview_handlers_test.go
internal\agent\handler\preview_repair_prepare.go
internal\agent\handler\preview_repair_prepare_test.go
internal\agent\handler\preview_submit_edits.go
internal\agent\handler\preview_submit_edits_test.go

internal\agent\preview\browser_bridge.go
internal\agent\preview\console.go
internal\agent\preview\container_runner.go
internal\agent\preview\controller.go
internal\agent\preview\editor_commit.go
internal\agent\preview\inspector.go
internal\agent\preview\repair.go
internal\agent\preview\session_manager.go

internal\agent\schema\frontend_preview_manifest.go
internal\agent\schema\frontend_preview_manifest_test.go

internal\agent\spec\front\preview_edit.go
internal\agent\spec\front\preview_edit_test.go

docs\v2\frontend_preview_manifest.schema.example.json
docs\v2\前后端程序员拆分_阶段1执行方案.md
docs\v2\前后端程序员拆分_阶段2执行方案.md
```

这些文件当前项目中不存在，合并冲突风险较低。

### 4.2 需要手工合并的核心文件

以下文件两边都存在且内容不同，不能直接覆盖：

```text
docs\v2\pipeline_full_delivery.spec.json

internal\agent\bootstrap\default.go
internal\agent\bootstrap\default_test.go
internal\agent\bootstrap\plugins.go
internal\agent\core\logical_keys.go
internal\agent\executor\executor.go
internal\agent\protocolmock\agent.go
internal\agent\role\architect\split_module.go
internal\agent\role\architect\split_module_test.go
internal\agent\role\front\agent.go
internal\agent\role\front\agent_test.go
internal\agent\spec\front\write_code.go

internal\app\api.go
internal\app\http_test.go
internal\app\workspace_views.go
internal\core\types.go
internal\orchestrator\orchestrator.go
internal\orchestrator\orchestrator_test.go
internal\orchestrator\run_manager.go
internal\orchestrator\run_manager_test.go
internal\pipeline\json_loader_test.go
```

合并原则：

- 保留当前项目已有 API 路由。
- 引入 `Doujia5.0` 新增的 preview / front edit / manifest 能力。
- 保留当前项目与前端已经对齐的接口行为。
- 对状态枚举和 JSON 字段做兼容性扩展。
- 对测试按最终行为合并，不简单删除失败测试。

---

## 5. 关键模块设计

### 5.1 API 层

重点文件：

```text
internal\app\api.go
internal\app\workspace_views.go
```

设计要求：

- 当前已有 `/api/runs`、`/api/projects`、`/api/plugins`、`/api/runs/:id/session/messages` 等接口继续可用。
- 当前前端使用的 `git-branches`、pipeline graph、workspace overview、pipeline workspace 等接口不能丢失。
- 如果 `Doujia5.0` 对 preview/edit 有新增 API，则优先以兼容方式接入，不破坏旧响应结构。
- HTTP 响应错误格式维持 Go 后端现有 `{ "error": "..." }` 风格。

### 5.2 Agent 与 Preview 能力

重点文件：

```text
internal\agent\preview\*
internal\agent\handler\preview_*.go
internal\agent\schema\frontend_preview_manifest.go
internal\agent\spec\front\preview_edit.go
internal\agent\role\front\agent.go
```

设计要求：

- 新增 preview controller、browser bridge、container runner、inspector、repair、editor commit 能力。
- front agent 能识别并使用 preview edit 相关 spec。
- manifest schema 用于描述前端预览运行方式、入口、端口、检查方式和编辑上下文。
- handler 负责准备预览修复、提交编辑结果、读取预览状态。

### 5.3 Orchestrator 与 Run Manager

重点文件：

```text
internal\orchestrator\orchestrator.go
internal\orchestrator\run_manager.go
internal\core\types.go
```

设计要求：

- 保留当前 run/task/session/pipeline 生命周期。
- 引入 `Doujia5.0` 对 preview/front-agent 工作流的新增控制逻辑。
- 不迁移数据库位置，不改变默认 SQLite 使用方式。
- 新增字段要兼容已有运行数据，不能要求清空 `runtime/devflow/state.db`。

### 5.4 Pipeline Spec

重点文件：

```text
docs\v2\pipeline_full_delivery.spec.json
internal\pipeline\json_loader_test.go
```

设计要求：

- 合入 `Doujia5.0` 对完整交付流程的更新。
- 保留当前项目已经依赖的 pipeline id、stage id、op id。
- 如果新增 preview edit stage，需要保证 JSON spec 校验通过。

---

## 6. 执行顺序

### 步骤 1：确认差异清单

重新生成源文件差异清单，排除运行数据和可执行文件。

检查点：

- 明确新增文件列表。
- 明确变更文件列表。
- 明确不会触碰 `runtime`。

### 步骤 2：合并新增文件

先合入新增 preview / manifest / handler / spec 文件。

检查点：

- 新增文件路径正确。
- Go package 名称与目录一致。
- 没有引入 Windows 路径硬编码。

### 步骤 3：合并基础类型和 schema

合并：

```text
internal\core\types.go
internal\agent\schema\frontend_preview_manifest.go
internal\agent\core\logical_keys.go
```

检查点：

- 类型编译通过。
- 新增字段兼容旧数据。
- JSON tag 与 Go API 响应一致。

### 步骤 4：合并 agent/front/preview 能力

合并：

```text
internal\agent\bootstrap\*.go
internal\agent\executor\executor.go
internal\agent\protocolmock\agent.go
internal\agent\role\front\agent.go
internal\agent\spec\front\*.go
internal\agent\handler\preview_*.go
internal\agent\preview\*.go
```

检查点：

- `protocolmock` 和 `real` 模式都能编译。
- front agent 相关测试可以运行。
- preview handler 相关测试可以运行。

### 步骤 5：合并 orchestrator / run manager

合并：

```text
internal\orchestrator\orchestrator.go
internal\orchestrator\run_manager.go
```

检查点：

- run 创建、启动、任务推进逻辑不回退。
- checkpoint / acceptance checkpoint 行为不回退。
- preview edit 工作流可以被 orchestrator 识别。

### 步骤 6：合并 API 和 workspace view

合并：

```text
internal\app\api.go
internal\app\workspace_views.go
```

检查点：

- 当前前端使用的 API 仍存在。
- `Doujia5.0` 新增 API 可以访问。
- snake_case API 响应保持稳定。

### 步骤 7：合并 pipeline spec 和测试

合并：

```text
docs\v2\pipeline_full_delivery.spec.json
internal\app\http_test.go
internal\orchestrator\*_test.go
internal\agent\*_test.go
internal\pipeline\json_loader_test.go
```

检查点：

- 测试覆盖最终合并后的行为。
- 不通过删除测试来“制造通过”。

---

## 7. 验证方案

### 7.1 Go 后端完整测试

```powershell
cd D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
go test ./...
```

通过标准：

- 所有 Go package 编译通过。
- 所有单元测试通过。
- 没有 package import 缺失。

### 7.2 后端 smoke test

使用 mock/protocolmock 模式启动，避免真实 LLM 和外部环境影响基础验证：

```powershell
cd D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
go run ./cmd/devflow-server -addr 127.0.0.1:18080 -db ".\runtime\devflow\state.db" -projects ".\runtime\workspaces" -agent-mode protocolmock
```

检查接口：

```text
GET /healthz
GET /api/health
GET /api/runs
GET /api/demo-runs
GET /api/projects
GET /api/plugins/registry-state
```

### 7.3 前端 API wiring 测试

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
npm.cmd test -- --runInBand
```

重点关注：

```text
test\unit\client-devflow-wiring.spec.ts
test\unit\pipeline-graph.spec.ts
test\unit\pipeline-overview-items.spec.ts
test\unit\plugin-center.spec.ts
```

### 7.4 手动联调

启动方式保持当前项目说明：

```powershell
cd D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
go run ./cmd/devflow-server -addr 127.0.0.1:18080 -db ".\runtime\devflow\state.db" -projects ".\runtime\workspaces" -agent-mode real
```

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
npm.cmd run dev:client -- --host 127.0.0.1 --port 4173 --strictPort
```

浏览器访问：

```text
http://127.0.0.1:4173/client/
```

手动检查：

- 首页能加载 recent/demo runs。
- 创建 run 页面能提交配置。
- run detail 页面能加载消息、任务和 pipeline。
- workspace 页面能加载 pipeline workspace。
- plugin center 能加载 registry state。

---

## 8. 风险与应对

### 风险 1：核心大文件合并冲突

高风险文件：

```text
internal\app\api.go
internal\orchestrator\orchestrator.go
internal\app\http_test.go
```

应对：

- 不直接覆盖。
- 先比较函数级差异。
- 保留当前项目独有路由和测试。
- 每合并一组核心文件就运行相关 package 测试。

### 风险 2：旧 SQLite 数据不兼容

应对：

- 不修改 DB 路径。
- 新增字段必须有兼容默认值。
- smoke test 使用现有 `runtime\devflow\state.db` 前，先可用临时 DB 做一次验证。

### 风险 3：前端接口丢失

应对：

- 以 `client/src/api/devflow-client.ts` 为前端 API 清单。
- 合并 `api.go` 时逐项检查这些路径仍存在。
- 跑前端 API wiring 测试。

### 风险 4：真实 agent 模式依赖 Docker / LLM / 插件

应对：

- 基础验证先用 `agent-mode protocolmock`。
- 编译与 API 通过后，再用 `agent-mode real` 做手动验证。
- real 模式失败时区分代码问题和环境问题。

---

## 9. 回滚方案

本次只修改当前 Go 后端目录和本设计文档。

如需回滚，只恢复：

```text
D:\project\Doujia-project-package-2026-05-06\Doujia_clean_source_20260504_175507\Doujia
```

不影响：

```text
D:\project\Doujia-project-package-2026-05-06\code\client
D:\project\Doujia-project-package-2026-05-06\code\server
D:\project\Doujia-project-package-2026-05-06\run-logs
D:\project\Doujia5.0
```

实施前建议额外生成一份当前 Go 后端差异/备份记录，确保可以快速定位本次改动。

---

## 10. 验收标准

合并完成后必须满足：

- `go test ./...` 通过，或明确列出与环境相关的失败原因。
- Go 后端可以在 `127.0.0.1:18080` 启动。
- `/healthz` 和 `/api/health` 返回成功。
- 当前前端 API 清单中的主要接口仍可访问。
- `npm.cmd test -- --runInBand` 通过，或明确列出非本次合并导致的既有失败。
- 不需要修改当前启动说明中的基础运行架构。
- 不删除当前项目已有运行数据。

---

## 11. 推荐执行结论

推荐执行本方案。

执行时按以下节奏推进：

1. 只读确认差异。
2. 合并新增 preview 文件。
3. 合并类型和 schema。
4. 合并 agent/front/preview 能力。
5. 合并 orchestrator / run manager。
6. 合并 API / workspace view。
7. 合并 pipeline spec 和测试。
8. 跑 Go 测试。
9. 启动后端做 smoke test。
10. 跑前端测试和手动联调。

该方案保留当前项目架构，风险集中在 Go 后端文件级合并，可测试、可回滚、不会影响 `code/server` 或前端主结构。
