# UI 设计指南

> **设计类型**: App 设计（应用架构设计）
> **确认检查**: 本指南适用于可交互的应用/网站/工具。

> ℹ️ Section 1-2 为设计意图与决策上下文。Code agent 实现时以 Section 3 及之后的具体参数为准。

## 1. Design Archetype (设计原型)

### 1.1 内容理解

- **目标用户**: 比赛评审 + 技术研发人员，需要快速理解产品价值和技术亮点，预期专业、清晰、有条理的展示
- **核心目的**: 展示 Doujia（豆荚）产品价值、演示全流程能力、引导评审体验核心功能
- **期望情绪**: 专业信任、技术现代感、清晰易懂、流畅演示
- **需避免的感受**: 杂乱无章、信息过载、廉价感、模糊不清

### 1.2 设计语言

- **Aesthetic Direction**: 现代企业级科技感，参考飞书官网的清晰分屏叙事，同时兼具 ChatGPT 式的对话简洁感
- **Visual Signature**: 
  1. 深蓝主色构建专业信任感，低饱和度中性色调让数据和图表成为焦点
  2. 清晰的分区块叙事，充足留白保证比赛现场讲解可读性
  3. 技术化几何无衬线字体，数据和代码采用等宽展示
  4. 适度圆角 + 细微阴影，既现代又不失专业稳重
  5. Pipeline 图语义化配色，状态清晰可辨
- **Emotional Tone**: 专业现代 - 技术驱动的研发流程自动化，传递可靠可控的专业感
- **Design Style**: **Grid 网格 + Soft Blocks 柔色块 混合风格 — 技术产品需要清晰的网格结构展示流程，同时用柔色块降低视觉压迫，保证演示舒适度**
- **Application Type**: SaaS / 展示型工具 - 首页官网叙事 + 功能页工作台，混合模式

## 2. Design Principles (设计理念)

1. **演示优先**: 所有设计服务于比赛现场讲故事，技术亮点必须视觉可见，不藏在文字里
2. **渐进展开**: Run 创建前保持极简对话风格，创建后渐进展开专业工作台，避免信息过载
3. **清晰分层**: 三栏布局明确功能分区，左导航 → 中叙事 → 右可视化，层级不可混淆
4. **状态显性**: DoujiaGit Ref、Pipeline 节点状态、当前运行位置必须有明确视觉标记
5. **专业克制**: 技术产品不需要过度装饰，用留白和排版建立层次，让内容本身成为焦点

## 3. Color System (色彩系统)

**配色设计理由**：AI 研发工具需要建立专业信任感，选择沉稳的科技蓝作为主色，低饱和度中性色系保证长时间使用不疲劳，语义化状态色清晰区分 Pipeline 节点状态。

### 3.1 主题颜色

| 角色               | CSS 变量               | Tailwind Class            | HSL 值    
| ------------------ | ---------------------- | ------------------------- | ---------- 
| bg                 | `--background`         | `bg-background`           | `hsl(215 25% 97%)`
| card               | `--card`               | `bg-card`                 | `hsl(0 0% 100%)`
| text               | `--foreground`         | `text-foreground`         | `hsl(220 40% 12%)`
| textMuted          | `--muted-foreground`   | `text-muted-foreground`   | `hsl(220 12% 45%)`
| primary            | `--primary`            | `bg-primary`              | `hsl(212 92% 48%)`
| primary-foreground | `--primary-foreground` | `text-primary-foreground` | `hsl(0 0% 100%)`
| accent             | `--accent`             | `bg-accent`               | `hsl(212 92% 96%)`
| accent-foreground  | `--accent-foreground`  | `text-accent-foreground`  | `hsl(212 92% 35%)`
| border             | `--border`             | `border-border`           | `hsl(220 15% 88%)`

### 3.2 Sidebar 颜色（仅当使用 Sidebar 导航时定义）

| 角色                       | CSS 变量                       | Tailwind Class                    | HSL 值     | 设计说明                         |
| -------------------------- | ------------------------------ | --------------------------------- | ---------- | -------------------------------- |
| sidebar                    | `--sidebar`                    | `bg-sidebar`                      | `hsl(215 25% 97%)` | Sidebar 背景色，与主背景一致保持轻盈 |
| sidebar-foreground         | `--sidebar-foreground`         | `text-sidebar-foreground`         | `hsl(220 40% 12%)` | Sidebar 文字色，对比度 ≥ 4.5:1   |
| sidebar-primary            | `--sidebar-primary`            | `bg-sidebar-primary`              | `hsl(212 92% 48%)` | 激活态背景色，使用主色一致       |
| sidebar-primary-foreground | `--sidebar-primary-foreground` | `text-sidebar-primary-foreground` | `hsl(0 0% 100%)` | 激活态文字色，白色对比度达标     |
| sidebar-accent             | `--sidebar-accent`             | `bg-sidebar-accent`               | `hsl(212 92% 96%)` | Hover 态背景，主色浅色调         |
| sidebar-accent-foreground  | `--sidebar-accent-foreground`  | `text-sidebar-accent-foreground`  | `hsl(212 92% 35%)` | Hover 态文字，主色深调           |
| sidebar-border             | `--sidebar-border`             | `border-sidebar-border`           | `hsl(220 15% 88%)` | 右侧边框分隔，保持风格一致       |
| sidebar-ring               | `--sidebar-ring`               | `ring-sidebar-ring`               | `hsl(212 92% 48%)` | 聚焦环颜色，主色一致             |

### 3.3 Topbar/Header 设计策略（仅当使用顶部导航时定义）

**背景策略**：首页使用 `bg-background`，顶部固定导航，底部使用 `border-border` 细线分隔，与内容区分开。

**文字与图标**：
- 默认态：使用 `text-foreground`，品牌名称加粗，导航项使用常规字重
- 激活态：使用 `text-primary`，字重保持不变，下方轻微底边高亮
- Hover 态：使用 `text-primary`，背景 `bg-accent` 圆角高亮

**边框与分隔**：底部使用 `border-border` 1px 细线分隔，无阴影，保持干净清爽。

### 3.4 语义颜色（可选）

> Pipeline 节点状态语义颜色，基于主色色相偏移生成

| 状态     | CSS 变量          | HSL 值                    | 使用场景                     |
| -------- | ----------------- | ------------------------- | ---------------------------- |
| pending  | `--status-pending`| `hsl(38 92% 50%)`         | 等待执行节点                 |
| running  | `--status-running`| `hsl(212 92% 48%)`        | 执行中节点                   |
| success  | `--status-success`| `hsl(142 72% 35%)`        | 成功完成节点                 |
| failed   | `--status-failed` | `hsl(0 84% 60%)`          | 执行失败节点                 |
| rejected | `--status-rejected` | `hsl(0 74% 50%)`         | 被拒绝审批节点               |
| recovered | `--status-recovered` | `hsl(271 91% 55%)`      | 已恢复节点（DoujiaGit 特色） |
| current-ref | `--current-ref`   | `hsl(142 72% 35%) 0.2`  | 当前 Ref 边框标记（带透明度） |

对比度检查：所有状态色在白色背景上均满足 ≥ 4.5:1 对比度要求。

## 4. Typography (字体排版)

- **Heading**: Inter + 系统无衬线字体栈 → `font-family: Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`
- **Body**: Inter + 系统无衬线字体栈 → `font-family: Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`
- **Code / DSL / Data**: JetBrains Mono + 系统等宽字体栈 → `font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace`
- **字体导入**: 使用系统字体栈，无需引入外部字体

**排版层级**（遵循 1.5x 比例原则）：
- Hero 大标题: `text-4xl / font-extrabold / tracking-tight` (≈ 36px)
- 区块标题: `text-2xl / font-bold / tracking-tight` (≈ 24px)
- 卡片标题: `text-lg / font-semibold` (≈ 18px)
- 正文: `text-base / font-normal / leading-7` (≈ 16px)
- 次要文字: `text-sm / font-normal / leading-6` (≈ 14px)
- 极小文字 / 标签: `text-xs / font-medium` (≈ 12px)

## 5. Layout Strategy (布局策略)

### 5.1 结构方向

**导航策略**：
- 首页：顶部固定导航栏 → 官网式叙事需要，内容向下滚动
- 功能页/工作台：左侧侧边栏轻导航 → 运行中工作台需要持久导航，功能模块多
- 理由：混合结构，首页做产品展示用顶部导航，功能区做多模块管理用侧边栏，符合飞书官网参考风格

**页面架构特征**：
- 首页：分屏垂直叙事，每屏一个核心主题，清晰引导评审从价值到亮点到行动
- 功能页（Run 未创建）：居中聚焦布局，突出对话输入，配置默认收起，保持极简
- 运行中工作台：三栏弹性布局，左窄导航 (200px) + 中主执行 (flex 1) + 右可视化 (400px)，明确功能分区
- 所有区块保持最大宽度约束，大屏不无限拉伸，保证阅读舒适度

### 5.2 响应式原则

**断点策略**：
- 桌面端 (>1200px): 完整三栏布局，侧边栏常驻显示
- 平板 (768px - 1200px): 侧边栏可折叠，点击呼出抽屉，右栏 Pipeline 可全屏切换
- 移动端 (<768px): 单栏堆叠，侧边栏完全隐藏为汉堡菜单，右栏置于中栏下方

**内容密度**：
- 桌面端保持正常间距，三栏信息并行展示
- 移动端单栏纵向排列，可点击区域不小于 48px 满足触摸要求
- 对话输入框在移动端保持底部固定，方便输入

## 6. Visual Language (视觉语言)

**形态特征**：基于 Grid 网格 + 柔色块混合风格
- 网格结构：能力总览区 2×3 网格展示 6 个模块，对齐整齐清晰
- 圆角：`rounded-md` (0.375rem) 用于卡片，`rounded-lg` (0.5rem) 用于按钮和弹窗，保持适度柔和不突兀
- 阴影：`shadow-sm` 用于卡片悬浮，`shadow-md` 用于弹窗和下拉菜单，不使用厚重阴影
- 边框：`border` 1px 细边框分隔区块，清晰但不抢眼
- Pipeline 节点使用 `rounded-full` 胶囊形状，状态颜色清晰标识

**装饰策略**：
- 仅在首页 Hero 区使用轻微几何渐变装饰，不干扰内容阅读
- 能力卡片使用 subtle 色块区分，不使用复杂插画或纹理
- 核心亮点区的图示使用线条和色块组合，保持技术清晰感
- 整体装饰元素 ≤ 2 种手法，严格遵循克制原则

**动效原则**：
- 交互反馈快速干脆，hover/focus 过渡 150ms，营造专业响应感
- 折叠/展开过渡 200ms，平滑不突兀
- Pipeline 图节点 hover 有轻微放大和阴影加深，点击有明确反馈
- 滚动页面使用平滑滚动，分区块锚点跳转流畅

**可及性保障**：

- 正文文字与背景对比度 ≥ 4.5:1（当前方案满足：`hsl(220 40% 12%)` on `hsl(215 25% 97%)` 对比度 ≈ 11:1）
- 大号标题对比度 ≥ 3:1（满足要求）
- 所有交互元素（按钮、链接、可点击节点）都有明确的 hover/focus 状态反馈
- Pipeline 节点状态不仅用颜色，还用不同边框粗细和图标辅助区分，色弱用户可识别
- 复杂背景都保证文字对比度，不使用高饱和度背景承载正文

## 7. Application Architecture (应用架构)

### 7.1 路由设计

| 页面 | 路由 | 布局 |
|------|------|------|
| 官网首页 | `/` | 顶部导航 |
| 功能页（Run未创建） | `/run/create` | 侧边栏导航 |
| 运行中工作台 | `/run/:runId/workspace` | 侧边栏导航 |
| 网页注入模式 | `/run/:runId/inject` | 侧边栏导航 |
| Demo/历史Run详情 | `/run/:runId/detail` | 侧边栏导航 |
| 模板与注册中心 | `/templates` | 侧边栏导航 |
| Demo Run快捷入口 | `/demo` | 侧边栏导航（同Run详情） |

### 7.2 混合导航策略

- 首页 `/` 使用顶部固定导航栏，官网式叙事结构
- 其他页面使用左侧侧边栏（shadcn Sidebar），支持 icon 模式折叠
- 导航项：首页、功能页、模板中心、Demo Run

### 7.3 数据模型

- **Run**: 存储运行基本信息、配置与状态
- **Message**: 存储对话历史、执行播报
- **PipelineNode**: 存储Pipeline节点状态、快照
- **PipelineTemplate**: 存储可编排流程模板与DSL
- **RegistrationState**: 存储组件注册状态

### 7.4 插件集成

- 需求澄清插件（ai-text-generate，前端流式调用）
- MR摘要生成插件（ai-text-generate，前端调用）

### 7.5 关键技术亮点前端落点

- DoujiaGit：首页亮点区 + 工作台右栏Pipeline现场图 + 节点详情Ref/Snapshot
- Pipeline JSON DSL：首页亮点区 + 工作台Pipeline结构图 + 模板中心预览
- Human-in-the-Loop：审批卡片 + Reject回退路径