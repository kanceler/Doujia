# 豆荚 (Doujia) 前后端接口文档

> 前端共 5 个 API 模块，调用后端 20 个有效接口（7 个需登录认证）。
> 类型定义统一在 `shared/api.interface.ts`，前后端共享。

---

## 1. Run 运行管理 (`runApi`)

| # | 方法 | 路径 | 登录 | 说明 |
|---|------|------|------|------|
| 1 | GET | `/api/demo-runs` | - | 获取演示 Run 列表 |
| 2 | GET | `/api/runs/:runId` | - | 获取 Run 详情 |
| 3 | POST | `/api/runs` | ✅ | 创建新 Run |
| 4 | POST | `/api/runs/validate-config` | - | 验证 Run 配置 |
| 5 | POST | `/api/runs/:runId/approve` | ✅ | 审批节点 |
| 6 | POST | `/api/runs/:runId/retry` | ✅ | 重试/拒绝节点 |
| 7 | POST | `/api/runs/:runId/clone` | ✅ | 克隆 Run |

### 1.1 获取演示 Run 列表

```
GET /api/demo-runs
```

- **响应**: `{ items: DemoRunItem[] }`
- **DemoRunItem**: `{ id, name, description, status: RunStatus, createdAt }`

### 1.2 获取 Run 详情

```
GET /api/runs/:runId
```

- **路径参数**: `runId: string`
- **响应**: `RunItem`
- **RunItem**: `{ id, name, status: RunStatus, demandSummary, config: RunConfig | null, ref, isDemo, createdAt, updatedAt }`

### 1.3 创建新 Run

```
POST /api/runs
```

- **请求体**: `CreateRunRequest`
  - `demandSummary: string` — 需求描述
  - `config: RunConfig` — 运行配置
  - `templateId: string` — 流程模板 ID
- **RunConfig**: `{ modelProvider, modelName, apiKey, baseUrl, targetRepo, templateId, enableWebInject, enableObservability }`
- **响应**: `{ id: string }`

### 1.4 验证 Run 配置

```
POST /api/runs/validate-config
```

- **请求体**: `ValidateConfigRequest` — `{ config: RunConfig }`
- **响应**: `ValidateConfigResponse` — `{ valid: boolean, errors: string[] }`

### 1.5 审批节点

```
POST /api/runs/:runId/approve
```

- **路径参数**: `runId: string`
- **请求体**: `ApproveRequest`
  - `nodeId: string` — 审批节点 ID
  - `comment?: string` — 审批意见
- **响应**: `{ success: boolean }`

### 1.6 重试/拒绝节点

```
POST /api/runs/:runId/retry
```

- **路径参数**: `runId: string`
- **请求体**: `RetryRequest`
  - `nodeId: string` — 目标节点 ID
  - `action: 'reject' | 'retry'` — 拒绝或重试
  - `comment?: string` — 说明
- **响应**: `{ success: boolean }`

### 1.7 克隆 Run

```
POST /api/runs/:runId/clone
```

- **路径参数**: `runId: string`
- **请求体**: `CloneRunRequest`
  - `newName: string` — 新 Run 名称
  - `configOverrides?: Partial<RunConfig>` — 配置覆盖
- **响应**: `{ newRunId: string }`

---

## 2. Message 对话消息 (`messageApi`)

| # | 方法 | 路径 | 登录 | 说明 |
|---|------|------|------|------|
| 8 | GET | `/api/runs/:runId/messages` | - | 获取消息列表（游标分页） |
| 9 | POST | `/api/messages` | - | 发送消息 |

### 2.1 获取消息列表

```
GET /api/runs/:runId/messages?pageSize=20&cursor=xxx
```

- **路径参数**: `runId: string`
- **查询参数**: `pageSize: number`, `cursor?: string`
- **响应**: `MessageListResponse`
  - `items: MessageItem[]`
  - `nextCursor: string | null`
  - `hasMore: boolean`
- **MessageItem**: `{ id, runId, role: MessageRole, content, type: MessageType, metadata: MessageMetadata | null, createdAt }`
- **MessageRole**: `'user' | 'assistant' | 'system'`
- **MessageType**: `'text' | 'approval' | 'diff' | 'test-result' | 'mr-summary'`

### 2.2 发送消息

```
POST /api/messages
```

- **请求体**: `SendMessageRequest`
  - `sessionId: string` — 会话 ID
  - `content: string` — 消息内容
- **响应**: `{ id: string }`

---

## 3. Pipeline 节点 (`pipelineNodeApi`)

| # | 方法 | 路径 | 登录 | 说明 |
|---|------|------|------|------|
| 10 | GET | `/api/runs/:runId/pipeline-nodes` | - | 获取 Run 的节点列表 |
| 11 | GET | `/api/pipeline-nodes/:nodeId` | - | 获取节点详情 |
| 12 | GET | `/api/runs/:runId/pipeline-history` | - | 获取 Pipeline 执行历史 |

### 3.1 获取节点列表

```
GET /api/runs/:runId/pipeline-nodes
```

- **路径参数**: `runId: string`
- **响应**: `{ items: PipelineNodeItem[] }`
- **PipelineNodeItem**: `{ id, runId, name, type: PipelineNodeType, status: PipelineNodeStatus, parentId, position, ref, createdAt }`
- **PipelineNodeType**: `'task' | 'approval' | 'sub-pipeline'`
- **PipelineNodeStatus**: `'pending' | 'running' | 'success' | 'failed' | 'rejected' | 'recovered'`

### 3.2 获取节点详情

```
GET /api/pipeline-nodes/:nodeId
```

- **路径参数**: `nodeId: string`
- **响应**: `PipelineNodeDetail`
  - `{ id, name, type, status, snapshot: NodeSnapshot | null, artifact: NodeArtifact | null, approvalRecords: object[], ref, createdAt }`
- **NodeSnapshot**: `{ agentName, input, output, timestamp }`
- **NodeArtifact**: `{ type, path, description }`

### 3.3 获取 Pipeline 执行历史

```
GET /api/runs/:runId/pipeline-history
```

- **路径参数**: `runId: string`
- **响应**: `PipelineHistoryResponse`
  - `nodes: PipelineHistoryNode[]`
  - `edges: PipelineHistoryEdge[]`
  - `refHistory: RefHistory[]`
- **PipelineHistoryNode**: `{ id, name, type, status, position, parentId, ref }`
- **PipelineHistoryEdge**: `{ from, to }`
- **RefHistory**: `{ ref, nodeId, timestamp }`

---

## 4. 模板与注册中心 (`templateApi`)

| # | 方法 | 路径 | 登录 | 说明 |
|---|------|------|------|------|
| 13 | GET | `/api/registration-state` | - | 获取组件注册状态 |
| 14 | GET | `/api/pipeline-templates` | - | 获取模板列表 |
| 15 | GET | `/api/pipeline-templates/:templateId/dsl` | - | 获取模板 DSL |
| 16 | POST | `/api/config` | ✅ | 保存用户配置 |

### 4.1 获取组件注册状态

```
GET /api/registration-state?type=handler
```

- **查询参数**: `type?: RegistrationType` — 可选过滤
- **RegistrationType**: `'handler' | 'op' | 'agent' | 'pipeline'`
- **响应**: `{ items: RegistrationStateItem[] }`
- **RegistrationStateItem**: `{ id, type: RegistrationType, name, status: RegistrationStatus, errorMessage }`
- **RegistrationStatus**: `'registered' | 'missing' | 'error'`

### 4.2 获取模板列表

```
GET /api/pipeline-templates
```

- **响应**: `{ items: PipelineTemplateItem[] }`
- **PipelineTemplateItem**: `{ id, name, description, isDefault }`

### 4.3 获取模板 DSL

```
GET /api/pipeline-templates/:templateId/dsl
```

- **路径参数**: `templateId: string`
- **响应**: `PipelineTemplateDsl`
  - `dsl: PipelineDsl` — `{ steps: PipelineStep[] }`
  - `description: string`
- **PipelineStep**: `{ name, type, config }`

### 4.4 保存用户配置

```
POST /api/config
```

- **请求体**: `SaveConfigRequest`
  - `modelConfig: ModelConfig` — `{ modelProvider, modelName, apiKey, baseUrl }`
  - `apiConfig: ApiConfig` — `{ targetRepo, webhookUrl }`
- **响应**: `{ success: boolean }`

---

## 5. 网页注入 (`webInjectApi`)

| # | 方法 | 路径 | 登录 | 说明 |
|---|------|------|------|------|
| 17 | GET | `/api/runs/:runId/preview` | - | 获取注入预览 |
| 18 | POST | `/api/runs/:runId/inject/modify` | ✅ | 提交网页修改 |
| 19 | POST | `/api/runs/:runId/inject/submit-mr` | ✅ | 提交 MR |
| 20 | GET | `/api/runs/:runId/export` | - | 导出结果 |

### 5.1 获取注入预览

```
GET /api/runs/:runId/preview
```

- **路径参数**: `runId: string`
- **响应**: `PreviewResponse` — `{ html, baseUrl }`

### 5.2 提交网页修改

```
POST /api/runs/:runId/inject/modify
```

- **路径参数**: `runId: string`
- **请求体**: `ModifyRequest`
  - `elementSelector: string` — 目标元素选择器
  - `elementContent: string` — 元素内容
  - `modifyInstruction: string` — 修改指令
- **响应**: `ModifyResponse` — `{ success, modifiedHtml, diff }`

### 5.3 提交 MR

```
POST /api/runs/:runId/inject/submit-mr
```

- **路径参数**: `runId: string`
- **请求体**: `SubmitMrRequest`
  - `mrTitle: string`
  - `mrDescription: string`
  - `diff: string`
- **响应**: `SubmitMrResponse` — `{ success, mrUrl }`

### 5.4 导出结果

```
GET /api/runs/:runId/export
```

- **路径参数**: `runId: string`
- **响应**: `{ downloadUrl: string }`

---

## 前端调用与页面映射

| 页面路由 | 使用的 API 模块 | 调用接口 |
|----------|----------------|----------|
| `/` 首页 | `runApi` | #1 获取 Demo Run 列表 |
| `/run/create` 创建页 | `runApi`, `templateApi` | #3 创建 Run, #4 验证配置, #14 获取模板列表, #15 获取模板 DSL |
| `/run/:runId/workspace` 工作台 | `runApi`, `messageApi`, `pipelineNodeApi`, `webInjectApi` | #2 Run 详情, #5 审批, #6 重试, #8 消息列表, #9 发送消息, #10 节点列表, #11 节点详情, #12 执行历史, #17 预览, #18 修改, #19 提交 MR |
| `/run/:runId/inject` 注入模式 | `webInjectApi` | #17 预览, #18 修改, #19 提交 MR |
| `/run/:runId/detail` 详情页 | `runApi`, `messageApi`, `pipelineNodeApi` | #2 Run 详情, #8 消息列表, #10 节点列表, #12 执行历史 |
| `/templates` 模板中心 | `templateApi` | #13 注册状态, #14 模板列表, #15 模板 DSL, #16 保存配置 |
| `/demo` Demo 入口 | `runApi` | #1 获取 Demo Run 列表 |

---

## 数据库表对应关系

| 数据库表 | 主要关联接口 | 说明 |
|----------|-------------|------|
| `run` | #1 ~ #7, #17 ~ #20 | 核心运行记录 |
| `message` | #8, #9 | 对话消息 |
| `pipeline_node` | #10 ~ #12, #5, #6 | Pipeline 执行节点 |
| `pipeline_template` | #14, #15 | 流程模板定义 |
| `registration_state` | #13 | 组件注册状态 |

---

*文档生成时间：2026-05-03 | 类型定义源文件：`shared/api.interface.ts`*
