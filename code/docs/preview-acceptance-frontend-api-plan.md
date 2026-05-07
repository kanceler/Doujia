# 预览验收/圈选修改宿主界面 API 对接表

本文只关注“预览验收/圈选修改”的宿主界面对应 API。  
不讨论 Pipeline overview、Git branches、需求池、后端合并策略或额外业务闭环。

依据文件：

- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\spec\front\preview_edit.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\handler\preview_handlers.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\handler\preview_submit_edits.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\handler\preview_repair_prepare.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\preview\browser_bridge.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\preview\inspector.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\preview\console.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\preview\repair.go`
- `D:\project\Doujia5.0\Doujia4.0\Doujia\internal\agent\preview\editor_commit.go`

## 1. 宿主界面主流程

`front.preview_edit` spec 已允许的 preview handlers：

```txt
preview_start
preview_browser_open
preview_inspector
preview_console
preview_submit_edits
preview_repair_prepare
```

宿主界面主流程：

```txt
点击“启动预览”
  -> preview_start
  -> 得到 session_id / preview_url

点击“打开预览”
  -> preview_browser_open
  -> 得到 preview_url / injection_script / confirm_event / confirm_handler
  -> iframe 打开 preview_url
  -> 注入 injection_script

点击“编辑/圈选”
  -> preview_inspector 或 preview_console
  -> 注入 script
  -> 开启 doujia:editor-mode
  -> 用户点击页面组件
  -> 宿主收到 doujia:selection

用户确认修改
  -> preview_submit_edits
  -> 小改动直接落盘并返回 mode=local
  -> 复杂改动后端内部进入 preview_repair_prepare
```

## 2. 前端统一调用约定

前端 UI 不直接写后端 path，只通过 `client/src/api/devflow-client.ts` 里的函数调用。

建议前端保留统一 handler adapter：

```ts
type DevflowPreviewHandlerName =
  | 'preview_start'
  | 'preview_browser_open'
  | 'preview_inspector'
  | 'preview_console'
  | 'preview_submit_edits'
  | 'preview_repair_prepare';

async function callDevflowPreviewHandler<TResponse>(
  runId: string,
  handler: DevflowPreviewHandlerName,
  args: Record<string, unknown>,
): Promise<TResponse> {
  // 真实 HTTP path 由后端同事最终确定。
  // UI 只依赖 handler 名和 args/data contract。
}
```

## 3. API 对接表

### 3.1 启动预览

宿主界面触发点：

```txt
底部工具栏 / 顶部操作区：启动预览
```

前端接口：

```ts
export async function callDevflowPreviewStart(
  runId: string,
): Promise<DevflowPreviewStartResult>;
```

对应后端 handler：

```txt
preview_start
```

后端 args：

```ts
{}
```

后端 data：

```ts
export interface DevflowPreviewStartResult {
  session_id: string;
  preview_url: string;
  port: number;
  container_id: string;
  module_id: string;
  workdir: string;
  manifest_path: string;
}
```

前端处理：

- 保存 `session_id`
- 保存 `preview_url`
- 展示 `module_id`
- 展示 `port`
- 展示 `manifest_path`
- 将界面状态置为“预览已启动”

### 3.2 打开预览并拿注入脚本

宿主界面触发点：

```txt
底部工具栏 / 顶部操作区：打开预览
```

前端接口：

```ts
export async function callDevflowPreviewBrowserOpen(
  runId: string,
  data: DevflowPreviewBrowserOpenRequest,
): Promise<DevflowPreviewBrowserOpenResult>;
```

对应后端 handler：

```txt
preview_browser_open
```

后端 args：

```ts
export interface DevflowPreviewBrowserOpenRequest {
  session_id: string;
}
```

后端 data：

```ts
export interface DevflowPreviewBrowserOpenResult {
  session_id: string;
  preview_url: string;
  injection_script: string;
  confirm_event: 'doujia:submit-edits';
  confirm_handler: 'preview_submit_edits';
}
```

前端处理：

- iframe 加载 `preview_url`
- iframe 加载完成后注入 `injection_script`
- 展示 `confirm_event`
- 展示 `confirm_handler`
- 将界面状态置为“预览已打开”

### 3.3 注入圈选脚本

宿主界面触发点：

```txt
底部工具栏：编辑 / 选择组件
```

前端接口：

```ts
export async function callDevflowPreviewInspector(
  runId: string,
): Promise<DevflowPreviewScriptResult>;
```

对应后端 handler：

```txt
preview_inspector
```

后端 args：

```ts
{}
```

后端 data：

```ts
export interface DevflowPreviewScriptResult {
  script: string;
}
```

脚本事件：

```txt
doujia:editor-mode
doujia:selection
```

开启圈选模式：

```ts
iframeWindow.dispatchEvent(
  new CustomEvent('doujia:editor-mode', {
    detail: { enabled: true },
  }),
);
```

圈选返回：

```ts
export interface DevflowPreviewSelectedNode {
  selector: string;
  tag: string;
  text: string;
  attributes: Record<string, string>;
}
```

后端脚本只会选择满足以下条件的组件：

```txt
[data-doujia-id][data-doujia-file]
```

前端处理：

- 展示蓝色选框
- 展示悬浮编辑框
- 展示：
  - `selector`
  - `tag`
  - `text`
  - `attributes.data-doujia-id`
  - `attributes.data-doujia-file`

### 3.4 注入组合 visual editor 脚本

宿主界面触发点：

```txt
底部工具栏：进入可视化编辑
```

前端接口：

```ts
export async function callDevflowPreviewConsole(
  runId: string,
): Promise<DevflowPreviewScriptResult>;
```

对应后端 handler：

```txt
preview_console
```

后端 args：

```ts
{}
```

后端 data：

```ts
export interface DevflowPreviewScriptResult {
  script: string;
}
```

脚本对象：

```ts
interface DoujiaVisualEditor {
  getState(): {
    sessionID: string;
    selection: DevflowPreviewSelectedNode | null;
    operations: DevflowPreviewEditorOperation[];
  };
  setOperations(operations: DevflowPreviewEditorOperation[]): void;
  submitSession(payload?: {
    session_id?: string;
    selected_node?: DevflowPreviewSelectedNode;
    operations?: DevflowPreviewEditorOperation[];
  }): void;
}
```

脚本事件：

```txt
doujia:submit-edits
```

前端处理：

- 注入 `script`
- 悬浮编辑框修改时调用 `setOperations`
- 点击确定时可以：
  - 直接调用前端 `callDevflowPreviewSubmitEdits`
  - 或调用 iframe 内 `submitSession`，再由宿主监听 `doujia:submit-edits` 后转调 `preview_submit_edits`

推荐用第一种：宿主前端直接调用 `preview_submit_edits`，更容易测试。

### 3.5 提交圈选修改

宿主界面触发点：

```txt
悬浮编辑框：确定
底部工具栏：提交
```

前端接口：

```ts
export async function callDevflowPreviewSubmitEdits(
  runId: string,
  data: DevflowPreviewSubmitEditsRequest,
): Promise<DevflowPreviewSubmitEditsResult>;
```

对应后端 handler：

```txt
preview_submit_edits
```

后端 args：

```ts
export interface DevflowPreviewSubmitEditsRequest {
  session_id: string;
  selected_node: DevflowPreviewSelectedNode;
  operations: DevflowPreviewEditorOperation[];
}
```

operation 类型：

```ts
export type DevflowPreviewOperationType =
  | 'set_text'
  | 'set_style'
  | 'set_layout'
  | 'delete_node'
  | 'apply_variant';

export interface DevflowPreviewEditorOperation {
  type: DevflowPreviewOperationType;
  target?: string;
  property?: string;
  value?: string;
  variant?: string;
  css?: Record<string, string>;
}
```

#### set_text

前端表单：

```txt
新文案
```

payload：

```ts
{
  session_id,
  selected_node,
  operations: [
    {
      type: 'set_text',
      target: selected_node.selector,
      value: '新的文案'
    }
  ]
}
```

#### set_style

前端表单：

```txt
CSS 属性
CSS 值
```

payload：

```ts
{
  session_id,
  selected_node,
  operations: [
    {
      type: 'set_style',
      target: selected_node.selector,
      property: 'color',
      value: '#2563eb'
    }
  ]
}
```

或：

```ts
{
  session_id,
  selected_node,
  operations: [
    {
      type: 'set_style',
      target: selected_node.selector,
      css: {
        color: '#2563eb',
        backgroundColor: '#eff6ff'
      }
    }
  ]
}
```

#### set_layout

前端表单：

```txt
布局属性
布局值
```

payload：

```ts
{
  session_id,
  selected_node,
  operations: [
    {
      type: 'set_layout',
      target: selected_node.selector,
      property: 'padding',
      value: '24px'
    }
  ]
}
```

#### delete_node

payload：

```ts
{
  session_id,
  selected_node,
  operations: [
    {
      type: 'delete_node',
      target: selected_node.selector
    }
  ]
}
```

后端行为：

```txt
delete_node requires structural source repair
```

会进入 repair fallback。

#### apply_variant

payload：

```ts
{
  session_id,
  selected_node,
  operations: [
    {
      type: 'apply_variant',
      target: selected_node.selector,
      variant: 'primary'
    }
  ]
}
```

后端行为：

```txt
apply_variant requires template source repair
```

会进入 repair fallback。

## 4. 提交返回处理

### 4.1 本地安全修改成功

后端 data：

```ts
export interface DevflowPreviewSubmitEditsLocalResult {
  status: 'kok';
  mode: 'local';
  next_action: 'refresh_preview';
  changed_files: string[];
  [key: string]: unknown;
}
```

前端展示：

```txt
修改已写回
changed_files
刷新预览
```

前端动作：

- 刷新 iframe
- 清空当前 operations
- 保留 selected_node 或重新进入圈选模式

### 4.2 复杂修改进入 repair

后端 data：

```ts
export interface DevflowPreviewSubmitEditsAgentResult {
  status: string;
  mode: 'agent';
  next_action: string;
  repair_instruction: string;
  repair_instruction_path: string;
  repair_task: unknown;
  repair_result: unknown;
  issue_path?: string;
  [key: string]: unknown;
}
```

前端展示：

```txt
已进入修复流程
status
next_action
repair_instruction_path
issue_path
```

前端动作：

- `next_action === 'refresh_preview'`：刷新 iframe
- `next_action === 'upstream_repair_required'`：提示交回后续修复
- `next_action === 'inspect_failure'`：展示失败信息

## 5. `preview_repair_prepare` 的前端定位

对应后端 handler：

```txt
preview_repair_prepare
```

后端 args：

```ts
export interface DevflowPreviewRepairPrepareRequest {
  repair_instruction: string;
}
```

后端 data：

```ts
export interface DevflowPreviewRepairPrepareResult {
  repair_instruction_path: string;
  repair_task: unknown;
  repair_bundle: unknown;
}
```

前端接口：

```ts
export async function callDevflowPreviewRepairPrepare(
  runId: string,
  data: DevflowPreviewRepairPrepareRequest,
): Promise<DevflowPreviewRepairPrepareResult>;
```

宿主界面默认不主动调用它。

原因：

- `preview_submit_edits` 在遇到不支持本地落盘的操作时，会在后端内部调用 `preview_repair_prepare`。
- 宿主界面只需要展示 `preview_submit_edits` 返回的 `repair_instruction_path / repair_result / next_action`。

只有后端后续要求前端分步调试 repair 时，才把它暴露为调试按钮。

## 6. 前端最小类型集合

建议写入：

```txt
shared/devflow-api.ts
```

```ts
export interface DevflowPreviewStartResult {
  session_id: string;
  preview_url: string;
  port: number;
  container_id: string;
  module_id: string;
  workdir: string;
  manifest_path: string;
}

export interface DevflowPreviewBrowserOpenRequest {
  session_id: string;
}

export interface DevflowPreviewBrowserOpenResult {
  session_id: string;
  preview_url: string;
  injection_script: string;
  confirm_event: 'doujia:submit-edits';
  confirm_handler: 'preview_submit_edits';
}

export interface DevflowPreviewScriptResult {
  script: string;
}

export interface DevflowPreviewSelectedNode {
  selector: string;
  tag: string;
  text: string;
  attributes: Record<string, string>;
}

export type DevflowPreviewOperationType =
  | 'set_text'
  | 'set_style'
  | 'set_layout'
  | 'delete_node'
  | 'apply_variant';

export interface DevflowPreviewEditorOperation {
  type: DevflowPreviewOperationType;
  target?: string;
  property?: string;
  value?: string;
  variant?: string;
  css?: Record<string, string>;
}

export interface DevflowPreviewSubmitEditsRequest {
  session_id: string;
  selected_node: DevflowPreviewSelectedNode;
  operations: DevflowPreviewEditorOperation[];
}

export interface DevflowPreviewSubmitEditsLocalResult {
  status: 'kok';
  mode: 'local';
  next_action: 'refresh_preview';
  changed_files: string[];
  [key: string]: unknown;
}

export interface DevflowPreviewSubmitEditsAgentResult {
  status: string;
  mode: 'agent';
  next_action: string;
  repair_instruction: string;
  repair_instruction_path: string;
  repair_task: unknown;
  repair_result: unknown;
  issue_path?: string;
  [key: string]: unknown;
}

export type DevflowPreviewSubmitEditsResult =
  | DevflowPreviewSubmitEditsLocalResult
  | DevflowPreviewSubmitEditsAgentResult;

export interface DevflowPreviewRepairPrepareRequest {
  repair_instruction: string;
}

export interface DevflowPreviewRepairPrepareResult {
  repair_instruction_path: string;
  repair_task: unknown;
  repair_bundle: unknown;
}
```

## 7. 前端最小 API 函数集合

建议写入：

```txt
client/src/api/devflow-client.ts
```

```ts
export async function callDevflowPreviewStart(
  runId: string,
): Promise<DevflowPreviewStartResult>;

export async function callDevflowPreviewBrowserOpen(
  runId: string,
  data: DevflowPreviewBrowserOpenRequest,
): Promise<DevflowPreviewBrowserOpenResult>;

export async function callDevflowPreviewInspector(
  runId: string,
): Promise<DevflowPreviewScriptResult>;

export async function callDevflowPreviewConsole(
  runId: string,
): Promise<DevflowPreviewScriptResult>;

export async function callDevflowPreviewSubmitEdits(
  runId: string,
  data: DevflowPreviewSubmitEditsRequest,
): Promise<DevflowPreviewSubmitEditsResult>;

export async function callDevflowPreviewRepairPrepare(
  runId: string,
  data: DevflowPreviewRepairPrepareRequest,
): Promise<DevflowPreviewRepairPrepareResult>;
```

## 8. 宿主界面需要展示的字段

启动/打开区：

```txt
session_id
preview_url
port
container_id
module_id
workdir
manifest_path
confirm_event
confirm_handler
```

圈选区：

```txt
selected_node.selector
selected_node.tag
selected_node.text
selected_node.attributes.data-doujia-id
selected_node.attributes.data-doujia-file
```

提交区：

```txt
operations[].type
operations[].target
operations[].property
operations[].value
operations[].variant
operations[].css
```

结果区：

```txt
status
mode
next_action
changed_files
repair_instruction_path
issue_path
```

## 9. 不属于本界面 API 范围

以下内容不在本宿主界面 API 范围内：

- `preview-status`
- `session/status`
- `session/close`
- 需求池写入
- `backend_required`
- `ImprovementItemRecord`
- Pipeline overview 数据
- Git branches 数据
- Snapshots 数据

