# 项目生成完成卡片接入说明

本文档用于复刻本次改动：当 Doujia 项目生成完成后，在左侧对话区展示一张“项目已经生成，点击查看”的卡片；用户点击卡片后，右侧工作区切到“预览验收”页。

效果图：

`D:\project\Doujia-project-package-2026-05-06\run-logs\generated-project-card-preview-20260507-004452.png`

## 目标效果

- 项目未完成时，对话流保持原样。
- 项目完成后，对话流末尾出现一张完成卡片：
  - 标题：`项目已经生成`
  - 状态：`已完成`
  - 描述：`代码、测试和合并结果已经完成，可以进入预览验收查看生成效果。`
  - 按钮：`点击查看`
- 点击卡片或按钮后，右侧 tab 切到 `预览验收`。

## 前端实现步骤

### 1. 扩展消息类型

文件：

`shared/api.interface.ts`

在 `MessageType` 里增加：

```ts
| 'project-generated'
```

在 `MessageMetadata` 里增加：

```ts
projectGenerated?: {
  title?: string;
  description?: string;
  actionLabel?: string;
  targetTabId?: string;
  previewUrl?: string;
  artifactId?: string;
};
```

### 2. 新增完成卡片组件

文件：

`client/src/pages/RunWorkspacePage/ProjectGeneratedCard.tsx`

核心组件接口：

```ts
interface ProjectGeneratedCardProps {
  message: MessageItem;
  onOpen: () => void;
}
```

组件读取：

```ts
message.metadata?.projectGenerated
```

并渲染标题、描述、按钮。点击外层卡片或按钮都调用：

```ts
onOpen()
```

### 3. 在消息流中渲染新类型

文件：

`client/src/pages/RunWorkspacePage/MessageFlow.tsx`

引入组件：

```ts
import { ProjectGeneratedCard } from './ProjectGeneratedCard';
```

给 `MessageFlowProps` 增加：

```ts
onProjectGeneratedClick: () => void;
```

在 `messageTypeLabelMap` 增加：

```ts
'project-generated': '项目生成',
```

在 `MessageContent` 的 switch 中增加：

```tsx
case 'project-generated':
  return <ProjectGeneratedCard message={message} onOpen={onProjectGeneratedClick} />;
```

### 4. 在工作区页面接入点击行为

文件：

`client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx`

增加点击处理：

```ts
const handleOpenGeneratedProject = () => {
  setActiveTabId('preview-acceptance');
};
```

传给 `MessageFlow`：

```tsx
<MessageFlow
  messages={visibleMessages}
  onActionComplete={handleActionComplete}
  onProjectGeneratedClick={handleOpenGeneratedProject}
  messagesEndRef={messagesEndRef}
/>
```

### 5. 将后端消息转换为前端卡片

文件：

`client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx`

在 `toDialogMessages` 中识别后端消息：

```ts
const isProjectGenerated = message.message_type === 'project_generated';
```

并把它转换成：

```ts
type: 'project-generated'
```

metadata 映射建议：

```ts
projectGenerated: {
  title: typeof message.metadata?.title === 'string' ? message.metadata.title : undefined,
  description:
    typeof message.metadata?.description === 'string' ? message.metadata.description : undefined,
  actionLabel:
    typeof message.metadata?.action_label === 'string' ? message.metadata.action_label : undefined,
  targetTabId:
    typeof message.metadata?.target_tab_id === 'string'
      ? message.metadata.target_tab_id
      : 'preview-acceptance',
  previewUrl:
    typeof message.metadata?.preview_url === 'string' ? message.metadata.preview_url : undefined,
  artifactId:
    typeof message.metadata?.artifact_id === 'string' ? message.metadata.artifact_id : undefined,
}
```

### 6. 兼容只有 run 完成态的情况

如果后端暂时不发 `project_generated` 消息，前端可根据 run 完成态自动合成卡片。

在 `toDialogMessages` 返回前增加：

```ts
const hasPersistedProjectGeneratedMessage = chatMessages.some(
  (message) => message.type === 'project-generated',
);

const generatedProjectMessage: MessageItem[] =
  run?.status === 'completed' && !hasPersistedProjectGeneratedMessage
    ? [
        {
          id: 'generated-project-card',
          runId: run.run_id,
          role: 'assistant',
          type: 'project-generated',
          content: '项目已经生成，可以查看生成结果并进行预览验收。',
          metadata: {
            projectGenerated: {
              title: '项目已经生成',
              description: '代码、测试和合并结果已经完成，可以进入预览验收查看生成效果。',
              actionLabel: '点击查看',
              targetTabId: 'preview-acceptance',
            },
          },
          createdAt: run.updated_at || new Date().toISOString(),
        },
      ]
    : [];
```

最终返回：

```ts
return [...chatMessages, ...taskMessages, ...acceptanceMessage, ...generatedProjectMessage];
```

注意：`toDialogMessages` 需要新增 `run: DevflowRunView | null` 参数，调用处也要传入 `run`。

## 后端接入方式

### 最小接入

只要接口：

```http
GET /api/runs/:runId
```

返回：

```json
{
  "status": "completed"
}
```

前端就会自动显示卡片。

### 推荐接入

在：

```http
GET /api/runs/:runId/session/messages
```

返回列表中追加一条消息：

```json
{
  "message_id": "project_generated_001",
  "run_id": "run_4cbe8f4e96e5",
  "iteration_no": 1,
  "role": "assistant",
  "message_type": "project_generated",
  "content": "项目已经生成，可以查看生成结果并进行预览验收。",
  "metadata": {
    "title": "项目已经生成",
    "description": "代码、测试和合并结果已经完成，可以进入预览验收查看生成效果。",
    "action_label": "点击查看",
    "target_tab_id": "preview-acceptance",
    "preview_url": "可选",
    "artifact_id": "可选"
  },
  "created_at": "2026-05-07T00:00:00Z"
}
```

建议后端同时保持：

```json
{
  "status": "completed"
}
```

这样前端即使消息接口延迟，也能显示默认完成卡片。

## 验证命令

在项目目录：

```powershell
cd D:\project\Doujia-project-package-2026-05-06\code
```

运行新增契约测试：

```powershell
npm test -- client-devflow-wiring.spec.ts --runInBand -t "shows a clickable generated-project card"
```

运行类型检查：

```powershell
npm run type:check:client
```

运行前端构建：

```powershell
npm run build:client
```

本次验证结果：

- 新增契约测试通过。
- `npm run type:check:client` 通过。
- `npm run build:client` 通过。
- 完整 `client-devflow-wiring.spec.ts` 当前仍有 4 条历史断言失败，和本次完成卡片改动无关。

## 本次涉及文件

- `shared/api.interface.ts`
- `client/src/pages/RunWorkspacePage/ProjectGeneratedCard.tsx`
- `client/src/pages/RunWorkspacePage/MessageFlow.tsx`
- `client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx`
- `client/src/pages/RunDetailPage/MessageFlow.tsx`
- `test/unit/client-devflow-wiring.spec.ts`

