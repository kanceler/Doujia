# Real Agent Full-Delivery 测试总结

> Date: 2026-05-09
>
> Scope: 记录使用现有 `pipeline_full_delivery` + real agent runtime 进行全链路 smoke 的过程、真实阻塞点、已修复项和当前状态。

## 1. 测试目标

本轮目标不是重构 pipeline 语义，而是沿用当前系统方式尽快跑真实链路：

- pipeline registry: `internal/orchestrator/testdata/full_delivery/pipeline_full_delivery.spec.json`
- runner: `cmd/doujiagit-step-runner`
- agent mode: `real`
- LLM base URL: `https://ark.cn-beijing.volces.com/api/v3`
- model: `ep-20260423222531-dnqtj`
- API Key: 已在测试环境变量中使用，本文档不记录明文密钥

测试策略：

1. 从 `ceo_write_requirement` seed 开始。
2. 每个 task 用 `agent` 跑真实 agent，再用 `apply` 把 feedback 交给 orchestrator。
3. 遇到真实阻塞点就定位、补回归测试、最小修复，再继续 smoke。
4. Docker 相关步骤需要提升权限运行，否则会被 Windows Docker config / pipe 权限拦截。

## 2. 最终 clean smoke 结果

最终 clean run:

- DB: `runtime/real-smoke-full-delivery-finalfix.db`
- run id: `run_real_full_delivery_finalfix`
- status: `awaiting_acceptance`
- next task: `acceptance_iter_01`
- next task status: `waiting_external`

这表示自动交付链路已经跑完，并进入 CEO 外部验收 checkpoint。

已通过主链路：

- `ceo_write_requirement`
- `pm_write_product_plan`
- `ceo_review_product_plan`
- `architect_write_architecture`
- `pm_review_architecture`
- `architect_create_container`
- `architect_split_modules`
- front branch:
  - `front.write_code`
  - tester `test_data`
  - tester `test_code`
  - `front.preview_edit`
  - `front.user_preview_confirm`
- backend branch:
  - coder `write_code`
  - tester `test_data`
  - tester `test_code`
- global test data branch:
  - architect `test_data`
- root join:
  - `architect.merge_code`
  - architect `test_code`

最终全局测试结果：

- `global_test_report.result`: `kok`
- `test_passed`: `true`
- `tested_branch`: `main`
- `tested_commit`: `64470e91fa1091c697debf710e6b1c0927caf208`

## 3. 真实阻塞点与修复

### 3.1 `product_plan` bag 丢失

现象：

- `ceo_review_product_plan` 收到了 `product_plan`
- 但下一步 `architect_write_architecture` 被 dispatch 时没有 `input_bag_ids` / `input_bags`
- real agent 报：`missing required input bag "product_plan"`

修复：

- 在 task snapshot runtime context 中记录 `InputBags`
- root active-ref 调度下一步时，组合当前 snapshot 输入、历史可用 bag 和 legacy next-stage bag 解析结果
- 保留 forwarded review bag 语义，不把它伪装成新产物

测试：

- `TestFullDeliveryLegacyPrefixDispatchesArchitectureWriteWithProductPlanBag`
- `TestLegacyOutputBagBindingsFromTaskTreatsForwardedReviewPlanBagAsOutput`
- `TestLegacyNextTaskInputBagsCanReadDeclaredForwardedReviewPlanOutput`

### 3.2 PM review artifact URI 路径解析失败

现象：

- `pm_review_architecture` 返回 `kfail`
- deterministic review 用 `os.ReadFile(path)` 读取 `projects/<run>/...` 形式 artifact URI 时找不到真实文件

修复：

- PM agent 读取 bundle artifact 时解析 `projects/<run>/...` 到真实 run workspace 路径
- 同时修复 logical key 推断中 `environment_spec` 被 `architecture` 路径前缀误命中的问题

测试：

- `TestPMAgentReviewPlanResolvesProjectArtifactURIInputs`
- `TestPMAgentReviewPlanAcceptsRealSmokeEnvironmentSpecShape`
- `TestInferLogicalKeyFromURIRecognizesEnvironmentSpecBeforeArchitecturePath`

### 3.3 `environment_spec` prompt contract 不够硬

现象：

- 一次真实输出中出现 `*.css` / `*.js`
- schema 正确拒绝 unsafe path policy

修复：

- 在 `EnvironmentSpecPromptContract()` 中明确禁止根文件通配符
- 根文件必须列具体文件名，目录范围才能使用 `/**`

测试：

- `TestEnvironmentSpecPromptContractIncludesRequiredFields`

### 3.4 split module 输出与 full-delivery spec 不匹配

现象：

- split 原本输出旧的 `pipeline_module` / `module_input`
- 当前 full-delivery spec 需要 `run_front_module` / `run_backend_module_group` / `run_global_test_data`

修复：

- `architect.split_module` 改为输出当前 full-delivery 三路 start controls
- produced bags 改为 `front_module_input` / `backend_module_input` / `global_test_input`
- orchestrator 接受普通 call transition 的 start controls

测试：

- `TestFullDeliveryJSONDispatchesThreeWayFanoutAfterSplit`
- `TestArchitectSplitModuleRepairsSemanticPlanOnce`
- `TestBuiltinSplitModuleOpResolvesModuleAndGlobalProducedBags`

### 3.5 backend group foreach 找不到 indexed collection items

现象：

- parent 有 `backend_module_input[module_key=module02]`
- child backend group 只得到非 indexed `module_input`
- foreach 需要 `module_input[module_key=module02]`

修复：

- `resolveControlInputBagListBindings()` 在 source bag -> target bag 映射时保留 indexed aliases

测试：

- `TestStartPipelineControlPreservesIndexedCollectionInputBagAliases`

### 3.6 缺失 front-only ops

现象：

- front branch 跑到 `front.preview_edit` 时失败：`unknown_op: op not registered`
- 补完后继续跑到 `front.user_preview_confirm`，再次失败：`unknown_op: op not registered`

修复：

- 新增并注册 `front.preview_edit`
- 新增并注册 `front.user_preview_confirm`
- 当前实现是 unattended smoke 所需的最小 deterministic 版本：
  - `preview_edit` 写出 `preview_edit.json`
  - `user_preview_confirm` 自动通过，不做真实交互

测试：

- `TestAgentRunsPreviewEditByWritingPreviewArtifact`
- `TestAgentRunsUserPreviewConfirmAsAutoApproval`
- `TestNewBuiltinPluginRegistryBuildsDefaultRolesAndOps`

### 3.7 merge_code input bag contract 不匹配

现象：

- root 派发给 merge 的 bags 是：
  - `front_tested_module`
  - `backend_tested_module`
  - `front_code_bag`
  - `backend_code_bag`
- 但 `architect.merge_code` op spec 仍要求旧的 `tested_module` / `code_bag`

修复：

- `MergeCodeSpec()` 接受 full-delivery 的 front/backend alias bags
- role 读取逻辑兼容旧 `tested_module` / `code_bag` 与新 aliases
- frontend module 在 `module_specs` 中是 `module01`，但 root 返回索引是 `front`，merge 读取时同时接受 frontend module 的 `module01` 和 `front`

测试：

- `TestMergeCodeSpecAcceptsFullDeliveryFrontBackendAliases`
- `TestArchitectMergeCodeReadsFullDeliveryFrontBackendAliasBags`
- `TestArchitectMergeCodeReadsIndexedGenericModuleBags`

### 3.8 global test_code input bag contract 不匹配

现象：

- root 派发给 final test 的 bags 是 `merged_code` / `global_test_data` / `container_context`
- 但 `architect.test_code` op spec 仍要求旧的 `global_test_code_input`

修复：

- `TestCodeSpec()` 改为要求 full-delivery 的三个真实输入 bag
- 旧 `global_test_code_input` 保留为兼容用 optional bag

测试：

- `TestTestCodeSpecAcceptsFullDeliveryInputBags`

## 4. 验证命令与结果

已通过的关键回归：

```powershell
go test ./internal/orchestrator -run 'TestStartPipelineControlPreservesIndexedCollectionInputBagAliases|TestFullDeliveryJSONBackendGroupReturnsOnlyAfterAllModuleChildrenComplete|TestFullDeliveryJSONDispatchesThreeWayFanoutAfterSplit|TestFullDeliveryLegacyPrefixDispatchesArchitectureWriteWithProductPlanBag' -count=1
```

结果：PASS

```powershell
go test ./internal/agent/role/front ./internal/agent/bootstrap -run 'TestAgentRunsPreviewEditByWritingPreviewArtifact|TestAgentRunsUserPreviewConfirmAsAutoApproval|TestNewBuiltinPluginRegistryBuildsDefaultRolesAndOps' -count=1
```

结果：PASS

```powershell
go test ./internal/agent/spec/architect ./internal/agent/role/architect -run 'TestMergeCodeSpecAcceptsFullDeliveryFrontBackendAliases|TestArchitectMergeCodeReadsFullDeliveryFrontBackendAliasBags|TestArchitectMergeCodeReadsIndexedGenericModuleBags|TestTestCodeSpecAcceptsFullDeliveryInputBags' -count=1
```

结果：PASS

Clean real smoke:

```powershell
run_id = run_real_full_delivery_finalfix
final_status = awaiting_acceptance
next_task = acceptance_iter_01
next_task_status = waiting_external
global_test_report.result = kok
global_test_report.test_passed = true
```

接手后追加验证：

```powershell
go run ./cmd/doujiagit-step-runner next -db runtime/real-smoke-full-delivery-finalfix.db -projects runtime/workspaces -run run_real_full_delivery_finalfix -pipeline-registry internal/orchestrator/testdata/full_delivery/pipeline_full_delivery.spec.json -agent-mode real
```

结果：确认 `run_status = awaiting_acceptance`，`next_task.task_id = acceptance_iter_01`，`next_task.status = waiting_external`。

```powershell
go test ./internal/orchestrator ./internal/agent/role/front ./internal/agent/bootstrap ./internal/agent/spec/architect ./internal/agent/role/architect ./internal/agent/role/pm ./internal/agent/schema ./internal/agent/real ./internal/agent/spec/front -count=1
```

结果：PASS

接手后补充修复：

- 将 `internal/orchestrator` 中一组 legacy full-delivery 测试夹具更新到当前 registry 路径与 current split fanout/repair 命名。
- 将 `protocolmock` 的 split / preview / confirm 输出契约同步到 current full-delivery spec，使 package-level stub full flow 也能跑到 `awaiting_acceptance`。
- 一个旧 `module01/module02` root global-test 重复测试已标记 skip；current-shape 等价覆盖在 `orchestrator_internal_test` 中保留并随 package-level 测试通过。

## 5. 当前状态

已确认：

- real LLM 配置可用
- full-delivery 自动链路已从 seed 跑到外部验收 checkpoint
- front-only preview/confirm op 的最小真实实现可用
- split fanout、backend group foreach、三路 join、merge、global test 都已在 clean run 中通过
- Docker 相关步骤必须提升权限运行；非提升权限下会出现 Windows Docker config / pipe permission denied
- 更宽的 orchestrator/agent package-level Go tests 已补跑通过

剩余事项：

- `front.preview_edit` / `front.user_preview_confirm` 目前是 unattended smoke 的最小 deterministic 实现，还不是完整交互式前端预览流程
- 当前 run 停在 `acceptance_iter_01` 的 `waiting_external`，这符合外部验收 checkpoint 语义
- 后续可按提交前标准再跑 `go test ./...`，并决定是否提交
