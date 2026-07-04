# Ant Design 前端重构清单

## 已完成

- 新建独立分支：`frontend-refactor/antd-enterprise-ui`
- 引入 `antd` 与 `@ant-design/icons`
- 接入全局 `antd/dist/reset.css`
- 新增 `MindFSAntdProvider`，统一 antd 中文 locale、主题 token、暗色模式与项目主题色
- `AppShell` 接入 antd `Layout`、`Sider`、`Content`、`Footer`、`Drawer`、`Button`、`Tooltip`
- 登录/节点入口页接入 antd `Card`、`Input`、`Button`、`Modal`、`Alert`、`Popconfirm`
- 全局 Toast 接入 antd `Alert` 与 `Button`
- ErrorBoundary 兜底页接入 antd `Result`、`Card`、`Button`
- 顶层配对码弹窗接入 antd `Modal`、`Input`、`Alert`、`Button`
- 任务错误弹窗接入 antd `Modal`、`Alert`
- 文件侧栏顶部项目视图切换接入 antd `Segmented`，菜单入口接入 antd `Button`、`Tooltip`

## 进行中

- 文件树下拉菜单内部仍保留部分自定义按钮与嵌套菜单状态
- 任务内联编辑弹窗仍保留 TokenEditor 周边的定制输入与候选面板
- ActionBar 输入区仍以现有定制交互为主，已通过全局 antd 主题统一外层视觉

## 待处理

- 将文件树菜单继续拆分为 antd `Dropdown` / `Menu` / `Switch`
- 将会话列表批量操作、搜索框和空状态迁移为 antd `Input.Search`、`Empty`、`Popconfirm`
- 将任务模板、定时任务等复杂表单继续迁移为 antd `Form`、`Select`、`Radio`、`DatePicker`
- 将 git 状态、历史、diff 操作区继续统一为 antd `Tabs`、`Tag`、`Table` 或 `List`
- 补充 Playwright 桌面/移动端截图回归，覆盖登录页、主工作台、左右侧栏、核心弹窗
