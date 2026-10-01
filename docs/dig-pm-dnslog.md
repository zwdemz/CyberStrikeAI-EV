# dig.pm DNSLog 工具

`dig-pm-dnslog` 为项目增加独立的 DNSLog 提供方，原有 `dnslog` 工具保持独立。
客户端使用 Go 和项目已有的 Gorilla WebSocket，Windows、macOS、Linux 与现有 Docker 镜像共用实现，无需 Python、pip 或额外命令安装。

## 模块与配置

- `tools/dig-pm-dnslog.yaml`：工具定义，随 `security.tools_dir` 加载。
- `internal/dnslog/`：参数校验、HTTPS API、内存会话和 WSS 记录处理。
- `internal/security/dnslog.go`：MCP 适配和可信调用上下文绑定。
- `tests/internal/dnslog/`：使用本地 HTTPS/WSS 模拟服务的协议与边界测试。

默认连接 `https://dig.pm`。可通过服务进程环境变量 `DIG_PM_BASE_URL` 设置兼容提供方的 HTTPS 源站；不接受账号、路径、查询参数或片段。使用系统信任链，不关闭证书验证，不跟随 HTTP/WS 重定向。dev/test/prod 应使用独立进程和环境配置。

会话 token 仅存在服务进程内存中，工具参数、输出和持久化配置不包含该 token。工具只返回随机 `session_id`，同时绑定调用用户和对话；没有登录身份或可信对话上下文时拒绝调用。只有相同上下文能读取会话。

本地会话有效期为 1 小时，总容量 256（包括进行中的申请）；过期会话在后续访问/申请时清理。重启或重建工具执行器会丢失所有会话。此期限是客户端限制，平台期限未知；平台提前失效时需重新申请。

## MCP 调用

使用现有已认证的 MCP `tools/call` 接口，工具名 `dig-pm-dnslog`。参数中不接受 token、任意 URL、脚本或目标地址。

| 操作 | 必需参数 | 可选参数 | 返回内容 |
|---|---|---|---|
| `list_domains` | `operation` | 无 | 当前主域数组 |
| `get_domain` | `operation` | `main_domain` | `domain`、`session_id`、本地 `expires_at` |
| `get_records` | `operation`、`session_id` | `wait_time`，整数 1～30，默认 5 | 去重记录、计数、`truncated` |

```json
{"name":"dig-pm-dnslog","arguments":{"operation":"get_domain"}}
```

```json
{"name":"dig-pm-dnslog","arguments":{"operation":"get_records","session_id":"<上次返回的 session_id>","wait_time":5}}
```

查询结果示意（模拟数据）：

```json
{"status":"success","domain":"fixture.dns.example.test","records":[{"FullDomain":"probe.fixture.dns.example.test.","ClientIp":"192.0.2.1","Location":"fixture","CreatedAt":"2026-01-01T00:00:00Z","UUID":"fixture-record"}],"record_count":1,"truncated":false}
```

观察窗口完整结束且没有匹配记录时为 `no_records`。参数错误、会话过期、跨用户/对话访问、TLS/网络失败、连接提前关闭和无效消息均返回 MCP `isError=true`、`status=error`，不伪装为无记录。错误文本不带提供方响应正文或鉴权 URL。连接异常后可显式重试当前会话；不自动重连或降级执行 Python。

支持 `history`、`new_record`、`new_records`。按 UUID、ID、域名/来源/时间顺序去重，仅保留当前会话域及子域记录。每次最多 100 条或 256 个消息；提前达到限制时 `status=partial`、`truncated=true`，不宣称观察完整。总读取内容最多 4 MiB，单次 JSON/WS 消息最多 1 MiB，字段最多 1024 字节。取消工具调用会关闭 WSS 连接。各次调用均可返回历史，跨调用汇总时也应去重。

## 使用边界

仅观察 DNS 通道，不主动触发目标请求、不生成攻击载荷、不支持 HTTP/SMTP 回连。教育和企业 SRC 角色的工具白名单及 `src-low-impact` 策略继续禁止本工具；其他角色仍受现有 RBAC 和工具权限控制。

`ClientIp` 是递归 DNS 出口地址。DNS 回连只能作为相关性证据，不能单独证明漏洞；无记录也不能排除漏洞。域名和记录经过第三方平台，不应携带凭证、个人信息或业务数据。

## 构建、部署与回滚

1. 使用 Go 1.26.8 或更新的受支持补丁版本运行 `go build -o cyberstrike-ai ./cmd/server`（Windows 输出名为 `cyberstrike-ai.exe`）。现有 Dockerfile 已复制 tools 目录并编译此模块。
2. 备份现有程序，部署新程序及 `tools/dig-pm-dnslog.yaml`，沿用原配置/数据库。
3. 重启服务以载入新内部实现；只有 YAML 热加载不能升级旧程序。确认工具列表出现新工具后，在许可角色中申请会话。
4. 回滚时恢复旧程序，将新工具 YAML 的 `enabled` 改为 `false` 并重新加载/重启。内存会话随进程退出丢失，无数据库迁移。

本地模拟回归：`go test -race -coverpkg=./internal/dnslog ./tests/internal/dnslog`。模拟测试不会连接公共 DNSLog 或任何测试目标。协议依据用户提供的 dig.pm 调用手册；公共服务实时可用性与模拟协议通过是不同的验证项。
