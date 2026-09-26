# SSH 服务器别名管理 — 技术设计

## 1. 目标与非目标

**目标**

1. 一处维护 SSH 别名/IP/端口/用户/凭据，加密落盘。
2. Agent 会话内 `ssh <别名>` 直接可用（核心体验，依赖 OpenSSH 兼容的 config 物化）。
3. 密钥支持"选择已有路径"或"粘贴内容保存"；密码与密钥路径支持快速复用。
4. 导入：MindFS 导出 JSON（含加密凭据）+ 本机 `~/.ssh/config` Host 块。导出：JSON，凭据默认口令加密。
5. 密码型服务器可一键部署专用密钥，转成非交互的密钥认证。

**非目标（第一版不做）**

- SSH/SFTP 文件浏览器、端口转发管理、ssh-agent 服务、跳板机 ProxyJump 编排 UI（数据结构预留 `proxy_jump` 字段但 UI 不做）。
- MindFS 内置独立 SSH Web 终端（复用现有长 shell：物化后直接 `ssh <别名>` 即是终端）。
- Windows 远端服务器运维（OpenSSH server on Windows 的 shell 差异，v1 仅保证 Linux/Unix 远端）。

## 2. 数据模型

```go
// server/internal/sshops/store.go
type Server struct {
    ID        string `json:"id"`         // 服务端生成的随机 id
    Alias     string `json:"alias"`      // 唯一，ssh <alias> 即用这个
    Host      string `json:"host"`       // IP 或域名
    Port      int    `json:"port"`       // 默认 22
    User      string `json:"user"`
    Auth      AuthMode `json:"auth"`     // key_path | key_inline | password
    KeyPath   string `json:"key_path,omitempty"`   // auth=key_path 时
    ProxyJump string `json:"proxy_jump,omitempty"` // 预留
    Enabled   bool   `json:"enabled"`
    Notes     string `json:"notes,omitempty"`
    CreatedAt / UpdatedAt time.Time
}

type AuthMode string // "key_path" | "key_inline" | "password"
```

**存储文件**：`~/.config/mindfs/ssh-servers.json`（Windows: `%AppData%/mindfs/`），结构为"明文元数据 + 加密信封"：

```json
{
  "version": 1,
  "servers": [
    {
      "id": "srv_9f3k",
      "alias": "crunchbits",
      "host": "203.0.113.10",
      "port": 22,
      "user": "root",
      "auth": "password",
      "enabled": true,
      "secrets": { "alg": "AES-256-GCM envelope（见 §3）", "hint": "" },
      ...
    }
  ]
}
```

- `auth=key_inline`：密钥内容加密存于 `secrets.key_b64`，同时物化到 `~/.config/mindfs/ssh-keys/<id>.key`（0600），`~/.ssh/config` 里 IdentityFile 指向该物化路径；密文为唯一事实源，文件是物化产物（丢了可重新物化）。
- `auth=password`：密码加密存于 `secrets.password_b64`，仅供连接测试、公钥部署使用；物化的 config 条目不写任何密码。
- `auth=key_path`：不存密文，仅存路径（路径本身不算高敏，但导出时默认保留）。

## 3. 加密设计（`server/internal/secretbox`）

复用 `e2ee` 包中已有的 AES-GCM envelope 思路，抽出通用助手：

- **Master key**：`~/.config/mindfs/ssh-secret.key`，32 字节随机，`0600`，目录 `0700`，首次使用时生成；支持 `MINDFS_SSH_SECRET_KEY` 环境变量覆盖（便于容器只读卷挂载）。
- **派生**：每条记录用 HKDF-SHA256(masterKey, salt=记录 ID, info="mindfs-ssh-secrets") 派生 32 字节 AES key（复用 `e2ee.hkdfBytes` 模式）。
- **信封**：`{"v":1,"iv":"base64","ct":"base64"}` AES-256-GCM，AAD 绑定记录 ID，防跨记录移植密文。
- **导出口令加密**：PBKDF2-HMAC-SHA256(口令, salt 16B, 600k 迭代)（`golang.org/x/crypto/pbkdf2` 已随 x/crypto 提供）→ AES-GCM；导出文件自带 kdf 参数。
- 内存中密码用后即焚（`[]byte` 清零 helper），不进日志；API 层禁止回显（见 §6）。

## 4. 别名落地：物化 OpenSSH 配置（`ssh crunchbits` 的关键）

新增 `server/internal/sshops/materialize.go`：

1. 生成 `~/.ssh/config.d/mindfs-servers.conf`（0600；目录不存在则 0700 创建）：

```
# Managed by MindFS — do not edit (begin)
Host crunchbits
    HostName 203.0.113.10
    Port 22
    User root
    IdentityFile ~/.config/mindfs/ssh-keys/srv_9f3k.key
    IdentitiesOnly yes
    StrictHostKeyChecking accept-new

Host web-1
    ...
# Managed by MindFS (end)
```

2. 幂等地在用户 `~/.ssh/config` 顶部（首行区，避开任何 `Host` 块之前必须出现的全局项约束——`Include` 本身可出现在任意位置，放最前最安全）插入一行 `Include ~/.ssh/config.d/mindfs-servers.conf`（带标记注释，去重；用户已有等价 Include 时不重复加）。Windows 写 `%USERPROFILE%\.ssh\config`，Include 用绝对路径。
3. 每次 CRUD / 启用停用 / 导入后全量重写该文件；删除服务器时同步移除其块。写文件用"写临时文件 + rename"保证原子性，并保持 0600。
4. `password` 型条目也写入 Host 块（HostName/Port/User），交互式 ssh 会提示输密码（Web 终端可用）；非交互场景引导用户点"部署专用密钥"。
5. `~/.ssh` 或 config 文件不存在时自动创建；无 HOME 的环境（部分容器）降级：只报结构化警告，不阻塞其他服务器保存。

## 5. 公钥部署（密码 → 密钥）

`DeployKey(serverID)`：

1. 若无专用密钥则生成 ed25519 密钥对，私钥加密入库（auth 切为 key_inline 语义的"managed key"），公钥待部署。
2. 用存储的密码经 `golang.org/x/crypto/ssh` 拨号（已依赖 v0.52.0），执行幂等追加：`grep -qxF '<pubkey>' ~/.ssh/authorized_keys 2>/dev/null || echo '<pubkey>' >> ~/.ssh/authorized_keys`，并 `chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys`。
3. 成功后重新物化 config（IdentityFile 指向新密钥），返回结构化结果；失败按网络/认证/远端错误分类。

## 6. API 设计（全部 `protectedEndpoint`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ssh-servers` | 列表（脱敏：`has_password`、`key_source: path|managed`、`key_path`，无任何明文凭据） |
| POST | `/api/ssh-servers` | 新建/更新（携带明文密码/密钥内容仅此一次） |
| DELETE | `/api/ssh-servers/{id}` | 删除并重新物化 |
| POST | `/api/ssh-servers/{id}/test` | 拨号测试，返回 `{ok, reason_code, banner}` |
| POST | `/api/ssh-servers/{id}/deploy-key` | §5 流程 |
| POST | `/api/ssh-servers/import` | body: `{source:"json", payload, passphrase}` 或 `{source:"ssh_config"}`；dry-run 预览 + 确认导入两段式 |
| GET | `/api/ssh-servers/export?mode=encrypted&passphrase=...` | `mode=encrypted|no-secrets`，返回 JSON 文件下载 |

校验规则（防 ssh config 注入，`sshops/validate.go`）：

- `alias`：`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`，且不得为 `*`、`ssh` 等保留词；全局唯一。
- `host`：IP 或域名正则，禁止空白/换行/`=`/`#`；`port` 1–65535；`user`：`^[A-Za-z_][A-Za-z0-9._-]{0,31}$`。
- `key_path` 必须是绝对路径或 `~/` 开头，保存时后端尝试读取一次验证可解析（不存在 → 结构化错误）。
- 所有错误返回结构化 `error_code`，前端映射文案。

## 7. 导入 / 导出

- **导入 MindFS JSON**：识别版本与 kdf 参数，口令解密后走与新建相同的校验；同名别名自动加后缀 `-2` 或按选择覆盖（两段式：先返回 preview 列表 `{alias, host, action: create|conflict}`）。
- **导入 `~/.ssh/config`**：解析 Host 块的 `HostName/Port/User/IdentityFile/ProxyJump`（tokenizer 按空白+`=` 分隔，忽略注释；不支持 `Match` 块，跳过并提示）。IdentityFile 存在 → `key_path` 模式；无 IdentityFile → 预填 password 模式但密码留空。
- **导出**：`encrypted`（默认，口令 AES-GCM，含密钥内容与密码）或 `no-secrets`（仅元数据 + key_path）。文件名 `mindfs-ssh-servers-YYYYMMDD.json`。

## 8. 前端

- `FileTree.tsx` 菜单新增按钮（图标：终端/钥匙），打开 `SSHServersDialog`（结构对齐 `RemoteServersDialog` 的 popover 风格）。
- 左列服务器列表（别名 + host + 认证徽标），右列表单：别名/IP/端口/用户/认证方式三选一：
  - key_path：文本框 + "浏览"（复用文件树/已有路径补全）+ **快速复用下拉**（列出其他服务器用过的 key_path 去重）。
  - key_inline：粘贴框 + **快速复用下拉**（列出 managed key 服务器，可"复用其密钥"）→ 保存后变 managed。
  - password：密码框 + **快速复用下拉**（"复用 web-1 的密码"）；保存后永远不再回显。
- 操作按钮：测试连接、部署专用密钥（password 型显示）、删除；顶部：导入（文件上传 / 从本机 ~/.ssh/config 导入）、导出。
- `web/src/services/sshServers.ts` 对齐 `remoteServers.ts` 风格；i18n 补 `sshServers.*` zh-CN/en-US。

## 9. 安全清单

- 落盘：master key `0600`、密文库 `0600`、托管密钥 `0600`、`config.d` `0700`。
- API 永不返回明文密码/密钥内容/解密后的私钥；日志与 `command_history` 不记录凭据字段。
- 注入防护见 §6 校验；物化文件整体由后端生成，用户手工改动会在下次保存时被覆盖（头部注释声明）。
- 导出默认强制口令；`no-secrets` 模式导出仍含 host/user（提示用户妥善保管）。

## 10. 测试策略

- `secretbox`：往返、错 key 失败、AAD 绑定（换记录 ID 解密失败）。
- `sshops`：别名/主机/用户非法值表驱动测试；config 物化幂等（重复写不重复 Include）；`~/.ssh/config` 解析金样本（含 `=` 分隔、多 Host 行、Match 块跳过）。
- 导入导出：加密导出 → 口令导入往返；别名冲突 preview。
- test/deploy-key：用 Go 起 `gliderlabs/ssh` 或本地 sshd（CI 有 sshd 时）做拨号成功/密码错/网络不通三类；无 sshd 环境用接口 mock。
- 前端：service 层与对话框关键交互（认证模式切换、快速复用列表、导入预览）用现有 web 测试框架覆盖冒烟路径。
