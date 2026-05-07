---
name: antd-table-guide
description: "antd Table 的开发规范与常见修改场景参考，包含列配置、后端分页、固定列、虚拟滚动、操作列等最佳实践。触发词：改表格, 加一列, 表格加个, 表格太长, 加分页, 加筛选, 加搜索, 固定列, 表格太卡, 列太多, 操作按钮, 表格排序, 编辑删除, antd table"
---

# Antd Table 使用指南

antd Table 的开发规范与常见修改场景参考，确保列定义、分页、固定列等配置符合最佳实践，减少修改时的常见错误。

## 为什么需要这个技能

表格是应用里最常见也最容易改坏的组件。用户说"加一列状态"，常见的翻车情况：
- 加了列但忘了设宽度，撑坏整个表格
- 加了列但没写显示逻辑，直接显示 `true`/`active` 这种原始值
- 改了一个地方，分页丢了
- 改完手机上打不开了

## 改表格的流程

### 第一步：先看懂现有表格

**改之前必须先读代码。** 搞清楚：
- 现在有哪些列（columns 数组）
- 数据从哪来（dataSource 绑定的是什么）
- 有没有分页（pagination 怎么配的）
- 有没有固定列（fixed）和滚动设置（scroll）
- 列的显示逻辑（render 函数怎么写的）

### 第二步：理解用户想要什么，找对应的改法

用户说的话和你要做的事：

```
用户想改表格
  ├─ 加列 / 改列
  │   ├─ "加一列XX"
  │   │   → 在 columns 数组里加一项
  │   │   → 必须设 title、dataIndex、key、width
  │   │
  │   ├─ "用颜色/标签显示"
  │   │   → 加 render 函数
  │   │   → 用 <Tag> 组件，不同值不同颜色
  │   │   → 例：render: (status) => <Tag color={status === '通过' ? 'success' : 'error'}>{status}</Tag>
  │   │
  │   ├─ "显示日期"
  │   │   → render: (v) => dayjs(v).format('YYYY-MM-DD')
  │   │
  │   ├─ "显示金额"
  │   │   → render: (v) => `¥ ${v.toFixed(2)}`
  │   │
  │   └─ "加操作按钮（编辑/删除）"
  │       → 加在 columns 最后一项
  │       → fixed: 'right'，必须设 width
  │       → render 里用 <Space> 包裹多个 <Button type="link">
  │
  ├─ 加搜索 / 筛选
  │   ├─ "加个搜索框"
  │   │   → 表格上方加 <Input> 搜索框
  │   │   → 有后端 API 就走后端搜索，没有就前端 filter
  │   │
  │   ├─ "按XX筛选"
  │   │   → 加 <Select> 下拉筛选
  │   │   → 选项从数据中提取，或由用户指定
  │   │
  │   └─ "加多个筛选条件"
  │       → 表格上方加一个筛选栏
  │       → 用 <Form layout="inline"> + Row/Col 布局
  │
  ├─ 加分页
  │   → 看数据从哪来：
  │   ├─ 有后端接口 → 后端分页（current + pageSize + total + onChange 触发请求）
  │   └─ 纯前端数据 → 可以前端分页，但要加注释提醒以后改后端
  │
  ├─ 加排序
  │   → 对应列加 sorter: true
  │   → 在 handleTableChange 里处理排序参数
  │
  ├─ 表格体验问题
  │   ├─ "表格太长看不到表头"
  │   │   → 加 scroll={% raw %}{{ y: 500 }}{% endraw %}（固定表头）
  │   │
  │   ├─ "列太多超出屏幕"
  │   │   → 加 scroll={% raw %}{{ x: 总宽度 }}{% endraw %}
  │   │   → 重要列 fixed: 'left'，操作列 fixed: 'right'
  │   │
  │   ├─ "表格很卡"
  │   │   → 数据量大就开虚拟滚动：<Table virtual scroll={% raw %}{{ y: 500 }}{% endraw %} />
  │   │
  │   └─ "手机上看不了"
  │       → 加 scroll={% raw %}{{ x: 'max-content' }}{% endraw %}
  │       → 考虑隐藏次要列
  │
  └─ 加增删改
      ├─ "能新建/编辑"
      │   → 加一个 <Modal> 弹窗 + <Form> 表单
      │   → 表格上方加「新建」按钮
      │   → 操作列加「编辑」按钮
      │
      └─ "能删除"
          → 操作列加「删除」按钮
          → 点击后弹 Modal.confirm 二次确认
```

### 第三步：改代码

改的时候注意这些铁律：

| 规则 | 说明 |
|------|------|
| 每列必须设 width | 不设宽度会导致列宽不可控 |
| fixed 列必须设 width | 不设会渲染异常 |
| 后端数据必须后端分页 | 禁止全量拉到前端再分页，会卡 |
| 固定表头 | 表格必须加 `scroll={% raw %}{{ y: 500 }}{% endraw %}`，否则数据多了看不到表头 |
| columns 用 useMemo | 避免每次渲染都重新创建列定义 |
| 操作列放最后 + fixed right | 用户习惯操作在最右边 |
| 删除要二次确认 | 直接删太危险 |

### 第四步：告诉用户改了什么

改完用大白话说清楚：

> "表格改好了：
> - 加了一列'状态'，通过的显示绿色标签，未通过显示红色标签
> - 姓名列固定在左边了，横向滚动时不会跑掉
> - 加了分页，每页显示 20 条
>
> 你刷新看一下效果。"

## 常见翻车和怎么避免

| 翻车 | 原因 | 怎么避免 |
|------|------|---------|
| 加了列但显示空白 | dataIndex 拼错了 | 先看数据字段名，对着写 |
| 改完表格宽度乱了 | 新列没设 width | 每列都设 width |
| 分页不好使 | 前端分页但数据量太大 | 用后端分页 |
| 改完其他功能坏了 | 动了不该动的代码 | 只改 columns / pagination / scroll，不动无关逻辑 |
| Tag 颜色不对 | 颜色名写错了 | antd Tag color 可选值：success / error / warning / processing / blue / red / green |

## 参考代码片段

### 带标签的状态列
```tsx
{
  title: '状态',
  dataIndex: 'status',
  key: 'status',
  width: 100,
  render: (status: string) => (
    <Tag bordered={false} color={status === '通过' ? 'success' : 'error'}>
      {status}
    </Tag>
  ),
}
```

### 后端分页配置
{% raw %}
```tsx
<Table
  pagination={{
    current: page,
    pageSize: pageSize,
    total: total,
    showSizeChanger: true,
    showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`,
    onChange: (p, ps) => { setPage(p); setPageSize(ps); },
  }}
/>
```
{% endraw %}

### 操作列（编辑 + 删除）
```tsx
{
  title: '操作',
  key: 'action',
  fixed: 'right',
  width: 120,
  render: (_, record) => (
    <Space size="middle">
      <Button type="link" onClick={() => handleEdit(record)}>编辑</Button>
      <Button type="link" danger onClick={() => showDeleteConfirm(record.id)}>删除</Button>
    </Space>
  ),
}
```
