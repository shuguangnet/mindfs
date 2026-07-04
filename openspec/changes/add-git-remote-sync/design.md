## Context

MindFS 当前已经有围绕本地 Git 仓库的规划和部分实现路径：`gitview` 负责仓库定位与 Git 命令封装，`usecase` 将 Git 能力暴露给 HTTP handler，Web 端通过 Git 状态面板呈现状态、历史、分支和工作区信息。已有 `local-repo-pull-and-commit` 变更覆盖了本地同步和提交的基础方向，但明确把 `git push` 排除在第一版之外。

本变更把远端协作闭环补齐：用户在 MindFS 审阅、提交变更后，需要能在线获取常用远端仓库信息、拉取远端更新，并把本地提交推送到远端。实现仍应沿用现有 Git 链路，避免引入独立 Git 子系统或凭据托管层。

## Goals / Non-Goals

**Goals:**

- 展示当前分支相对 upstream 的远端同步状态，包括 ahead/behind、upstream 标识、工作区是否阻塞 pull。
- 展示已配置远端仓库列表和在线摘要，包括 remote 名称、脱敏 URL、可达性、默认分支和有限的远端分支摘要。
- 支持从 MindFS 执行 fetch 和安全 pull，并返回结构化结果供 UI 映射提示和按钮状态。
- 支持从 MindFS 执行 push 到当前 upstream，并支持首次 push 时选择 remote/branch 并设置 upstream。
- 支持从 MindFS 执行一键 commit 并 push，包括提交信息校验、提交范围校验、暂存、提交、推送和部分成功结果返回。
- 操作成功后刷新 Git 状态、历史和远端同步状态，保证 UI 不停留在旧数据。
- 对无 upstream、detached HEAD、脏工作区、非快进、认证失败、远端拒绝、空提交信息、无可提交变更和提交成功但推送失败等场景给出可测试的结果分类。

**Non-Goals:**

- 不实现 force push、delete remote branch、tag push 或多远端管理界面。
- 不实现冲突解决器、merge pull、rebase pull 或自动 stash 流程。
- 不实现 Git 凭据配置、OAuth 登录或 SSH key 管理；继续依赖本机 Git 凭据链路。
- 不实现远端仓库创建、fork 创建、PR 创建或远程托管平台账号绑定。
- 不替代已有本地 commit 规划；本变更只吸收远端工作流所需的提交能力，并保留本地 commit 能力可独立演进。

## Decisions

### 1. 继续在 `gitview` 封装 Git 远端操作

远端状态、远端发现、fetch、pull、commit、push 都需要相同的 repo root 解析、路径约束、命令执行和错误包装。将这些能力放在 `server/internal/gitview` 中可以复用现有 `loadRepoContext`、Git 命令 runner 和状态读取逻辑，再由 usecase/API 层做请求校验和响应映射。

替代方案是在 HTTP handler 或 usecase 中直接调用 `git`。这会让仓库定位、错误分类和路径安全规则分散，后续维护成本更高，因此不采用。

### 2. Pull 使用保守策略并拒绝隐式合并

第一版 pull 只允许 clean worktree 上的快进更新。实现上应在 pull 前读取状态和 upstream，检测脏工作区、无 upstream、detached HEAD，并使用不会创建 merge commit 的 Git 策略执行更新。

普通 `git pull` 会在分支分叉时进入 merge/rebase/冲突处理流程，还可能需要交互式编辑提交信息。MindFS 当前没有对应 UI，因此 pull 必须保持可预测。

### 3. 在线远端仓库信息优先通过 Git 协议获取

“在线获取”第一版应以已配置 remotes 为范围，不做 GitHub/GitLab/Gitea 等平台 API 绑定。后端从本地配置读取 `git remote -v`，再按需使用 `git ls-remote --symref` 或等价 Git 命令获取远端默认分支和有限的 heads 摘要。这样可以兼容 SSH、HTTPS、私有 Git 服务和本机已有凭据链路。

远端 URL 返回给前端前必须脱敏，尤其是 HTTPS URL 中的 username/password/token 片段。可达性检测失败时返回结构化状态，例如 `unreachable_auth`、`unreachable_network`、`unreachable_remote`、`unknown`，并保留远端名称和脱敏 URL 供用户识别。

### 4. Push 默认只发布当前分支到 upstream

默认 push 只面向当前分支配置的 upstream，并禁止 force push。这样 UI 的语义简单：按钮表示“发布当前分支尚未推送的提交”。当远端拒绝非快进更新时，后端返回 `rejected_non_ff`，提示用户先 fetch/pull 或去终端处理复杂历史。

替代方案是暴露任意 refspec 输入。该能力风险高、UI 复杂，也容易误推送错误分支，不适合作为第一版。

### 5. 首次 push 使用显式 remote/branch 输入设置 upstream

无 upstream 时，默认 push 应被阻塞，但 UI 可以提供“发布并设置 upstream”的路径。请求需要包含 remote 名称和 remote branch 名称；后端先验证 remote 存在，再执行 push 并设置 upstream。remote branch 默认值可由前端用当前 branch 名生成，但最终仍作为显式请求传给后端。

这避免了在后端猜测用户意图，也让 monorepo、fork 和非 `origin` 远端场景有清晰扩展点。

### 6. 一键 commit 并 push 是顺序工作流，不伪装成事务

一键 commit 并 push 请求包含提交信息、提交范围和推送目标策略。提交范围应复用已有本地提交规划中的约束：允许显式 paths 或 all changes，但所有 paths 必须位于目标 repo root 内；空提交信息、无可提交变更、无效路径都在执行 Git commit 前被拒绝。

工作流按顺序执行：刷新状态、暂存选定变更、创建 commit、执行 push。如果 commit 成功但 push 失败，后端不能回滚本地 commit，也不应隐藏该事实；响应应返回 `committed_push_failed`，包含新 commit hash、push 失败分类和刷新后的 sync state。UI 需要提示“已创建本地提交，但未推送成功”，并允许用户后续单独 push 或先 pull。

为降低误操作风险，第一版不自动执行 pull 或 rebase 来修复 push rejected，也不自动 amend 已创建提交。

### 7. 后端返回结构化状态码，前端不解析 Git 输出

Git stdout/stderr 在不同版本、语言环境和认证方式下不稳定。API 应返回稳定的枚举结果，例如：

- remote state: `ready`, `no_upstream`, `detached_head`, `not_git_repo`, `unknown`
- remote discovery: `listed`, `no_remotes`, `unreachable_auth`, `unreachable_network`, `unreachable_remote`, `failed`
- fetch result: `fetched`, `failed_auth`, `failed_network`, `failed_remote`, `failed`
- pull result: `fast_forwarded`, `up_to_date`, `blocked_dirty`, `blocked_no_upstream`, `blocked_detached_head`, `blocked_non_ff`, `failed_auth`, `failed_network`, `failed`
- push result: `pushed`, `up_to_date`, `blocked_no_upstream`, `blocked_detached_head`, `rejected_non_ff`, `failed_auth`, `failed_network`, `failed`
- commit-and-push result: `committed_and_pushed`, `committed_push_failed`, `blocked_empty_message`, `blocked_no_changes`, `blocked_invalid_paths`, `blocked_no_upstream`, `blocked_detached_head`, `rejected_non_ff`, `failed_auth`, `failed_network`, `failed`

原始 Git 输出可以作为调试字段返回给 UI 展示详情，但按钮状态、提示类别和测试断言必须依赖结构化字段。

### 8. UI 入口放在现有 Git 状态面板

远端同步属于当前 branch/status 的上下文，不需要新建独立页面。Git 状态面板 header 或 toolbar 展示 upstream、ahead/behind 和远端摘要，提供 fetch、pull、push、commit-and-push 操作；无 upstream 时展示发布分支入口；操作中禁用同 root 的其他远端操作。成功或阻塞后刷新当前 root 的 Git 状态、历史和远端同步状态。

commit-and-push 应使用明确表单：提交信息、提交范围、目标 remote/branch（无 upstream 时必填）。如果存在多个 remote，UI 默认选择当前 upstream remote；没有 upstream 时可以预填 `origin` 或第一项 remote，但必须展示最终目标。

## Risks / Trade-offs

- [认证失败导致 UI 操作不可用] → 不在 MindFS 中托管凭据，返回 `failed_auth` 并提示用户使用本机 Git 凭据链路完成授权。
- [远端 URL 泄露 token] → 所有 URL 在 API 响应前脱敏；调试详情不返回完整凭据。
- [pull 覆盖本地工作] → pull 前拒绝脏工作区，第一版不自动 stash。
- [远端已更新导致 push 被拒绝] → 返回 `rejected_non_ff`，不自动 force push，提示先 fetch/pull。
- [一键 commit 后 push 失败造成用户误解] → 返回 `committed_push_failed` 和 commit hash，UI 明确区分“本地提交已创建”和“远端发布失败”。
- [首次 push 误设置 upstream] → 要求请求显式提供 remote 和 branch，并验证 remote 存在；UI 显示目标后再提交。
- [Git 错误分类不完整] → 结构化分类覆盖主路径，保留 `failed` 兜底和原始输出详情，后续根据真实错误样本扩展。
- [与已有本地同步变更重叠] → 本变更聚焦 remote discovery、push 和完整远端工作流契约；实现时可合并复用已有 fetch/pull/commit 代码，避免重复 API。

## Migration Plan

1. 在 `gitview` 增加远端发现、远端状态、fetch、safe pull、commit、push、first push、commit-and-push 的命令封装和结果模型。
2. 在 usecase 和 HTTP handler 中新增远端仓库、远端同步和 commit-and-push 接口，保持 root 解析、请求校验和错误响应一致。
3. 在前端 Git service 和状态面板接入远端仓库展示、fetch/pull/push 操作、commit-and-push 表单、首次 upstream 输入和操作结果反馈。
4. 为后端 Git 命令分类和前端 UI 状态补充测试，使用临时本地 bare remote 覆盖 remote discovery、pull、push 和 commit-and-push 场景。
5. 发布为增量能力；回滚时隐藏前端入口并移除新 API 调用即可，不涉及持久化数据迁移。

## Open Questions

- 首次 push 的 remote branch 是否允许与本地 branch 不同名，还是第一版限制为同名以降低误操作风险？
- commit-and-push 的第一版是否允许选择部分文件，还是先只支持 all changes 以减少暂存状态复杂度？
- 当当前 root 是仓库子目录时，远端同步状态按整个仓库展示还是强调操作会影响整个仓库？
