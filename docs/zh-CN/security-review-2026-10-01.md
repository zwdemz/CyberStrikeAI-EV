# 2026-10-01 安全复核与修复记录

## 结论与范围

复核基线为 `18a77285b09c9eff7b0fb81985ff080f1493e69b`，修复位于 `codex/fix-security-boundaries` 工作区。前轮确认的 5 个安全问题均已完成源码修复和定向回归；正常回调测试另外发现并修复了 1 个原有 panic。未修改数据库结构、依赖版本或现有运行程序，未部署、提交或推送。

本记录不表示项目已无其他缺陷，也不表示全部依赖告警已关闭。原有 `run.sh`、`upgrade.sh` 的执行位差异予以保留。

## 复核与修复矩阵

| 编号 | 级别与成立条件 | 原因 | 修复结果 | 回归证据 |
| --- | --- | --- | --- | --- |
| EV-01 | 高；具有文件读取权限和至少一个可访问会话的用户 | 授权提取第一个路径段，实际读取再执行 Clean，导致两者使用不同会话 ID | 制品下载、路径查询和导出统一使用严格路径解析；拒绝父级、点段、空段、盘符、NUL、Windows 尾部点/空格歧义 | 自有文件正常；外部会话直接访问及正反斜杠穿越均为 403；URL 编码、双重编码、缺参和文件缺失有覆盖；导出不包含其他会话 |
| EV-02 | 高；MCP 的 URL、命令或参数内含凭据时 | 只掩码 Env/Headers，仍返回 URL/Args/Command；传输错误也可能带凭据 | 完整配置和错误详情仅向全局 `mcp:write` 返回；其他会话获得显式元数据投影、URL origin、掩码值和通用错误 | 列表与单条接口、只读用户、局部写权限和全局管理员均有覆盖；存储配置未被掩码覆盖；管理员编辑响应保留完整字段 |
| EV-03 | 中；攻击者能直接接入应用，或代理保留其伪造来源头 | Gin 默认信任所有代理，IP 限流身份由外部头控制 | 默认不信任来源头；显式代理 IP/CIDR 配置及启动校验，拒绝通配和全网段 | 同一 TCP 对端轮换 XFF/X-Real-IP 仍触发 429；可信代理、IPv6 和多跳地址选择正常 |
| EV-04 | 中；SSE/WebSocket 查询参数携带有效会话 Token | Gin 默认访问日志输出完整查询串，默认 panic 日志还会转储请求 | 替换为结构化访问日志和不输出请求/异常原文的恢复中间件；路由模板替代原始 URL | 查询串、Header、Cookie、路径参数和 panic 测试标记均未进入日志；SSE 与实际 WebSocket 握手/消息仍通过认证 |
| EV-05 | 中；企业微信回调启用且服务可达 | 先无限读取回调体再检查签名；服务器读取和空闲等待未设期限 | 缺少签名参数时提前返回；XML 解析前限制体积，超限 413；主服务和独立 MCP 增加可配置读取/空闲超时，保持无全局写超时 | 缺参时处理器读取 0 字节；未知长度请求至多读取限额 + 1 字节；有签名正常回调与重放拒绝正常；本机 TCP 测试验证不完整头/体被限时终止 |
| EV-06 | 普通功能缺陷；企业微信明文回复短于 50 字节 | 即便 Debug 关闭，构造日志字段时 `content[:50]` 也会越界 | 回复日志只记录长度，删除回复内容、签名及调试解密转储 | 45 字节的正常事件回复成功，随后相同请求仍被重放检查拒绝 |

主要实现位于 `internal/handler/chat_uploads.go`、`internal/handler/external_mcp.go`、`internal/handler/robot.go`、`internal/security/http_server.go`、`internal/config/http_security.go` 和 `internal/app/app.go`。新增回归位于镜像目录 `tests/internal/config`、`tests/internal/security`、`tests/internal/handler`。已有 MCP CRUD 测试显式绑定全局管理员身份，与实际编辑权限一致。

## 配置与接口兼容性

新增 `server.trusted_proxies`、`read_header_timeout_seconds`、`read_timeout_seconds`、`idle_timeout_seconds`、`webhook_max_body_bytes`。旧配置无需增加字段即可加载，默认分别为空列表、10 秒、300 秒、120 秒、1 MiB。具体范围、代理日志和部署示例见 [安全加固指南](security-hardening.md#http-信任边界与资源限制) 与 `config.example.yaml`。

- 代理部署需要填写实际代理地址，否则限流按代理自身 IP 计数。大文件上传应结合网络带宽调整读取期限。
- 企业微信缺少签名仍按原协议返回空的 200 确认；超限新增 413。签名及重放检查保持生效。
- 路径解析拒绝非规范点段，但保留正常嵌套目录及反斜杠输入。只读 MCP 的 `command`/`args` 不再返回，URL 不再包含路径或认证材料。
- 全局写权限管理员的 MCP 配置编辑、SSE/WebSocket 查询参数认证和流式输出保持兼容。
- 此次仅修复原报告中的路径规范化越权。没有对能够在制品目录中放置符号链接的本地写入主体给出隔离保证，也没有完成链接竞态的专项验证。
- 已存在的日志与会话没有被自动修改。若历史日志包含有效会话 Token，需按事件处理流程保全证据并撤销相关会话；反向代理日志也须同步去除查询串。

## 实际验证

环境为 Windows/amd64、Go 1.25.5、CGO 启用。

| 检查 | 结果 |
| --- | --- |
| 新增配置、安全及处理器回归 | 13 个顶层测试通过，另含路径、角色、超时子用例 |
| 已有外部 MCP、附件、机器人及企业微信测试 | 28 个顶层测试通过 |
| `go test -mod=readonly ./internal/config ./internal/app -count=1 -timeout=120s` | 两个包通过（实际与 security 包一起执行，结果分别判定） |
| `go test -mod=readonly ./internal/security -count=1 -timeout=120s` | 4 个 `TestEinoStreamingShell_*` 因依赖 `/bin/sh` 在 Windows 失败 |
| `go test -mod=readonly ./internal/security -skip '^TestEinoStreamingShell_' -count=1 -timeout=120s` | 其余安全模块测试通过；不将上述 4 个用例视为已通过 |
| `go build -mod=readonly ./...` | 全项目构建通过 |
| `git diff --check` | 通过 |

Go 临时目录中的部分大型测试程序启动时重复出现 `Access is denied`，未证实根因。通过 `go test -c -o <证据目录>` 保留测试程序后，按标准 `-test.*` 参数执行成功；未修改防护策略或添加排除项。初次用例也暴露了测试对原有缺参状态码的错误假设，以及 EV-06，均经源代码复核后修正并重测。没有执行生产流量、压力测试或真实凭据测试。

复现主要回归的命令（从仓库根目录运行）：

```powershell
$env:GOTOOLCHAIN = 'local'
go test -mod=readonly ./tests/internal/... -count=1 -timeout=120s
go test -mod=readonly ./internal/handler -run '^(TestExternalMCP|TestChatUpload|TestHandleWecom|TestWecom|TestRobot)' -count=1 -timeout=120s
go build -mod=readonly ./...
```

本机证据保存在 `C:\Users\amire\Documents\ChatGPT\Github\audit-reports\CyberStrikeAI-EV-2026-10-01`：`repair-config-tests.txt`、`repair-handler-tests.txt`、`repair-security-tests.txt`、`repair-existing-handler-tests.txt`、`repair-existing-core-tests.txt`、`repair-existing-security-windows-tests.txt`。这些日志只对应已说明的检查范围，未测全仓覆盖率、真实浏览器交互或 Linux/macOS 运行行为。

## 依赖告警复核与未关闭事项

前轮 `govulncheck v1.8.0`、Go 1.25.5、2026-09-28 漏洞库快照产生 91 个去重 GO 公告命中：44 个符号级、16 个包级、31 个模块级。33 个涉及标准库，58 个涉及其他模块。这是扫描命中与修复候选数量，不是已经证实可利用的漏洞总数。本轮没有升级 Go/依赖，也没有重新运行依赖扫描，不能从 91 中简单减去本次 5 项代码问题。

优先级和适用条件复核如下；以下推断以当前调用代码为依据，没有进行破坏性复现：

| 类别 | 源码与公告复核 | 后续处理 |
| --- | --- | --- |
| Go 标准库 | 当前构建工具链为 1.25.5，HTTP/TLS/URL 处理为实际路径。例如 [GO-2026-4341](https://pkg.go.dev/vuln/GO-2026-4341) 涉及大量参数解析，公告针对该项的修复版本为 1.25.6；该版本并不覆盖其他公告 | 优先统一构建/镜像工具链到覆盖相关公告的受支持修订版，重新构建和扫描实际交付物；本次读取限制不能替代标准库修复 |
| 图片解码 | `internal/vision/preprocess.go` 使用 `image.DecodeConfig` 和 `imaging.Open`；[GO-2026-4815](https://pkg.go.dev/vuln/GO-2026-4815) 涉及 TIFF 恶意偏移引发内存分配，公告针对该项修复为 x/image v0.38.0。文件字节上限不能直接保证解码内存安全 | 若允许处理不可信图片，应优先升级解码依赖并验证相应输入路径；其余 x/image 公告也需覆盖 |
| OTLP HTTP 导出 | `internal/einoobserve/otel.go` 仅在启用追踪且选择 `otlphttp` 时初始化此导出器。[GO-2026-4985](https://pkg.go.dev/vuln/GO-2026-4985) 需要恶意或异常 Collector 响应；该导出器公告修复版本为 v1.43.0 | 按部署配置确认暴露条件，协调升级 OTel 组件并回归导出链路 |
| MCP SDK 服务端公告 | `internal/mcp/client_sdk.go` 使用 SDK 的 `NewClient`。例如 [GO-2026-4773](https://pkg.go.dev/vuln/GO-2026-4773) 针对无授权 HTTP 服务端，且 Go 条目标记未审阅；仅模块/符号命中不足以证明当前部署具有该攻击面 | 区分 SDK 客户端与项目自己的 MCP 服务端，单独追踪其他 SDK 公告与配置；不能据此将所有 SDK 告警判为误报 |

其余依赖逐项适用性仍需继续确认。原始清单与结构化结果保留在证据目录，不将升级复杂度或当前环境限制表述为风险已消除。

## 发布与回滚

本次交付为源码、测试和文档，不自动替换正在运行的程序。发布前备份当前可执行文件、配置与数据，按实际代理拓扑填写信任配置，构建新产物并在测试环境验证登录限流、管理员 MCP 编辑、文件下载、SSE/WebSocket 和回调。重启后新默认值才生效。

没有数据库迁移。需要回滚时恢复上一版产物与配置并重启，保留数据和审计证据；回滚旧产物会同时恢复本记录中的原有风险。依赖升级和跨平台回归未完成前，不应将此次修复标记为整仓安全验收通过。
