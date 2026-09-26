## Why

MindFS 目前只能通过 `远端服务器`（node_id/pairing）连接其他 MindFS 实例，缺少对任意 Linux 服务器进行 SSH 运维的能力。用户希望在一处维护所有服务器的 SSH 别名、地址、密钥或密码，然后在任何 Agent 会话里直接说 `ssh crunchbits`，Agent 即可在自己的 shell 中用别名解析出对应配置并连接服务器执行运维操作，而无需手动在 `~/.ssh/config` 中维护条目，也无需把密码明文贴进对话。

同时 SSH 凭据属于高敏感数据：必须加密落盘、支持快速导入导出（含 `~/.ssh/config` 导入）、密钥支持"选择已有路径"或"粘贴内容保存"两种方式，密码与密钥路径支持从既有配置中快速复用，避免重复输入。

## What Changes

- 新增 SSH 服务器管理模块（后端 `server/internal/sshops` + API `/api/ssh-servers`），维护别名、主机、端口、用户、认证方式（密钥路径 / 粘贴密钥 / 密码）等字段，凭据全部加密存储，列表接口只返回 `has_password`、`key_source` 等脱敏元数据。
- 左侧边栏菜单新增 `SSH 服务器` 入口，弹出管理对话框：列表、表单（别名/IP/端口/用户/认证）、连接测试、删除，密码与密钥路径支持从既有服务器快速复用，粘贴的密钥内容保存为 MindFS 管理的密钥文件（0600）。
- 提供与 OpenSSH 兼容的别名落地机制：将所有启用的条目物化为用户 `~/.ssh/config` 中的 MindFS 托管配置（通过 `Include` 指向 `~/.ssh/config.d/mindfs-servers.conf`），从而让 Agent shell、终端以及任何本机 ssh 调用都能直接 `ssh <别名>` 解析连接。
- 密码型服务器支持一键"部署专用密钥"：后端用已存密码经 Go SSH 客户端登录一次，将自动生成的 ed25519 公钥追加到远端 `authorized_keys`，此后别名走密钥认证，Agent 可非交互运维。
- 支持导入导出：导入 MindFS 导出的 JSON（凭据可用口令加密）以及解析本机 `~/.ssh/config` 的 Host 块；导出为 JSON 文件，凭据默认口令加密、可选脱敏导出。
- 支持连接测试（真实 SSH 拨号并执行回显命令），返回结构化的成功/失败原因（网络、认证、密钥不可读等）。

## Capabilities

### New Capabilities
- `ssh-server-management`: 在 MindFS 内集中管理 SSH 别名与凭据（加密存储、快速复用、导入导出），并把别名物化为 OpenSSH 兼容配置，使 Agent 会话中 `ssh <别名>` 直接可用。

### Modified Capabilities

## Impact

- 后端新增 `server/internal/sshops`：加密存储（复用/抽象 AES-GCM envelope）、SSH 配置物化、`~/.ssh/config` 解析导入、连接测试、公钥部署；新增内部 `secretbox` 加密助手（master key 0600 + HKDF 派生 + AES-256-GCM）。
- 后端 `server/internal/api`（`http.go` + `usecase/ssh_servers.go`）新增 `/api/ssh-servers` CRUD、测试、公钥部署、导入、导出端点，全部走 `protectedEndpoint`，响应不含任何凭据明文。
- 前端 `web/src/components/SSHServersDialog.tsx`、`web/src/services/sshServers.ts`、`FileTree.tsx` 侧边栏菜单新增入口，`i18n` 增加 zh-CN/en-US 文案。
- Agent 集成无需改动：long-shell 继承 HOME，物化后的 `~/.ssh/config` 对所有 Agent（Claude Code、Codex 等）的 `ssh <别名>` 生效。
- 测试需覆盖加解密往返、别名/主机/用户注入防护、config 物化幂等、`~/.ssh/config` 解析、导入导出往返、连接测试与公钥部署（可对本地 sshd 或 mock server）。
