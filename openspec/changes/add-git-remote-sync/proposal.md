## Why

MindFS 已经开始规划本地 Git 状态、拉取和提交能力，但完整的远端协作闭环还缺少从远端获取仓库信息、更新代码、提交变更并发布到远端的明确产品契约。用户需要在 MindFS 内完成常见的远程仓库工作流，包括在线获取远端状态、一键 pull、一键 commit 并 push，减少在 UI 和终端之间切换，并让 agent 产生的代码变更可以被用户审阅后直接同步到远端仓库。

## What Changes

- 增加远端仓库发现与同步状态能力，支持在线获取已配置远端的名称、URL 摘要、可达性、默认分支/远端分支摘要，以及当前分支的 upstream、ahead/behind 和工作区阻塞状态。
- 支持从 MindFS 触发一键远端更新操作：fetch 和安全 pull，pull 默认使用可预测的快进策略，并对无 upstream、脏工作区、非快进、认证失败等场景返回结构化结果。
- 支持从 MindFS 触发 push，将当前分支的本地提交推送到配置好的 upstream，必要时支持首次设置 upstream。
- 增加一键 commit 并 push 工作流：用户选择提交范围并输入提交信息后，由后端完成校验、暂存、提交和推送；若提交成功但推送失败，返回可恢复的部分成功结果。
- 在现有 Git API 和前端 Git 面板中加入远端仓库信息、同步入口、commit+push 入口、操作中状态、成功/失败反馈和操作后的状态刷新。
- 第一版不实现复杂冲突解决 UI、凭据管理器、force push、merge/rebase pull 策略或多远端管理界面。

## Capabilities

### New Capabilities
- `git-remote-sync`: 在 MindFS 内查看 Git 远端仓库和同步状态，并执行 fetch、pull、push、commit-and-push 等常用远端协作操作。

### Modified Capabilities

## Impact

- 后端 `server/internal/gitview` 需要新增或扩展远端状态读取、远端发现、fetch、pull、commit、push 的 Git 命令封装和结构化结果分类。
- 后端 `server/internal/api/usecase` 与 HTTP 路由需要暴露远端仓库状态、远端操作和 commit-and-push 接口。
- 前端 `web/src/services/git.ts`、Git 状态面板和相关应用状态需要新增远端仓库展示、远端同步操作、commit-and-push 表单、禁用条件、错误提示和刷新逻辑。
- 测试需要覆盖远端发现、有 upstream、无 upstream、ahead/behind、脏工作区、非快进、认证失败、push rejected、首次设置 upstream、空提交信息、无变更和提交成功但推送失败等关键场景。
