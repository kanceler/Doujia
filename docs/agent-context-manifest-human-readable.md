# Agent 插件上下文改造简明版

## 一句话目标

把现在“把输入产物信息直接塞进 prompt”的方式，改成“先告诉 Agent 有哪些资料、每份资料是干什么的，再让 Agent 通过工具按需读取”。

这样做以后，Agent 不需要一开始就看到一堆原始文件内容，而是先看到一张清晰的上下文地图。

## 为什么要改

当前 agent 流程已经有 `artifact_read(logical_key)`，安全边界是对的：大模型不能随便读路径，只能通过 `logical_key` 读取已注册产物。

但现在 prompt 里的输入说明还偏“文件列表”：

- 只告诉模型有哪些 `logical_key`
- 对每个文件的业务含义解释不够强
- 模型容易不知道哪个文件该先读、哪个只是参考
- 后续接 RAG 时，没有一个统一的“资料索引层”

我们要加的不是简单的 prompt 文案，而是一层结构化的上下文清单：`ContextManifest`。

## 改造后的 Agent 工作方式

原来的感觉：

```text
这里有 requirement、module_spec、test_report。
你可以用 artifact_read 读取。
开始干活。
```

改造后：

```text
当前任务：coder / write_code。

可用资料：
1. module_spec
   这是架构师拆分出来的模块规格，用于确定本次实现边界。
   必读。

2. module_contract
   这是模块验收约束，用于检查输入输出和交付格式。
   必读。

3. previous_test_report
   这是上一轮测试报告，只在修复或对比问题时读取。
   可选。

如果需要正文内容，请调用 artifact_read(logical_key)。
不要猜测文件内容。
```

也就是说，prompt 负责告诉 Agent “资料地图”，工具负责读取“资料内容”。

## 核心新增概念：ContextManifest

每个可读产物会变成一条 manifest 记录：

```json
{
  "logical_key": "module_spec",
  "source_kind": "input",
  "source_role": "architect",
  "purpose": "模块规格，用于确定当前编码任务的实现范围",
  "usage_hint": "编码前优先读取",
  "read_priority": "required",
  "required_for_task": true,
  "artifact_version_id": "...",
  "logical_artifact_id": "...",
  "object_type": "json",
  "retrieval_query": "module_spec 模块规格 实现范围 architect",
  "tags": ["module", "spec", "json"]
}
```

这里最重要的是：

- `purpose`：这份资料是干什么的
- `usage_hint`：什么时候读它
- `read_priority`：必读、可选、背景资料
- `source_role`：大概是谁产出的，比如 `pm`、`architect`、`tester`
- `retrieval_query` / `tags`：为后续 RAG 预留

## 新增工具：artifact_list

新增一个工具：

```text
artifact_list()
```

作用是返回当前任务可读取的上下文清单，但不返回文件内容，也不暴露本地路径。

Agent 可以先调用：

```text
artifact_list()
```

再决定：

```text
artifact_read("module_spec")
artifact_read("module_contract")
```

这比让模型盲目读取所有输入更自然，也更接近未来 RAG 的交互方式。

## 保留现有安全模型

这次改造不改变安全边界：

- 仍然只能通过 `artifact_read(logical_key)` 读取内容
- 不能让模型传 `path`
- 不能让模型传 `output_dir`
- 不能让模型传 `file_name`
- prompt 和 `artifact_list` 都不暴露本地文件路径

换句话说，这次是“上下文表达能力增强”，不是“放开文件权限”。

## 代码改动范围

主要改这几块：

| 模块 | 改动 |
| --- | --- |
| `internal/agent/core/types.go` | 给 `AgentInputBundle` 增加 `ContextManifest` 字段 |
| `internal/agent/manifest/` | 新增 manifest 构建逻辑 |
| `internal/agent/executor/executor.go` | 在 prompt 编译前注入 manifest |
| `internal/agent/prompt/compiler.go` | prompt 改成展示语义上下文清单 |
| `internal/agent/handler/artifact_list.go` | 新增 `artifact_list` 工具 |
| `internal/agent/handler/artifact_read.go` | 读取结果附带对应 manifest 元数据 |
| `internal/agent/bootstrap/plugins.go` | 注册新的内置工具 |
| `internal/agent/spec/*` | 给关键 op 开启 `artifact_list` |

## 第一阶段交付边界

第一阶段只做这几件事：

1. 建立 `ContextManifest`
2. 改 prompt 的输入上下文表达
3. 新增 `artifact_list`
4. 让 `artifact_read` 返回上下文元数据
5. 给主要 agent op 开启 `artifact_list`
6. 补测试，确保不泄露路径

第一阶段不做：

- 不接向量数据库
- 不做 embedding
- 不实现完整 RAG 检索
- 不重写现有 pipeline 机制
- 不改变 artifact 权限模型

## 后续如何接 RAG

有了 `ContextManifest` 以后，未来可以自然加一个工具：

```text
artifact_search(query, tags)
```

它可以基于 manifest 里的：

- `logical_key`
- `purpose`
- `tags`
- `retrieval_query`

去本地文件、索引库、向量库里找片段。

重要的是：未来接 RAG 时，不需要再大改 prompt 结构，因为 Agent 已经习惯了“先看资料地图，再按需读取或搜索”的模式。

## 预期效果

改完后，Agent 会更像一个真实团队成员：

- 先知道自己在什么任务场景里
- 知道每份上游资料的用途
- 知道哪些必须读，哪些只是参考
- 不会把文件名当成内容乱猜
- 后续可以自然升级到 RAG

这是一次底层能力改造，不只是 prompt 优化。它会让 Doujia 的多 Agent 协作更稳，也更容易解释给评审看。
