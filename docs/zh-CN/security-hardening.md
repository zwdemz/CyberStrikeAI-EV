# 安全加固指南

[English](../en-US/security-hardening.md)

本文给出 CyberStrikeAI 上线前和持续运行中的安全加固清单。

## 上线前必做

- 首次部署后立即修改 `admin` 初始密码（Web 界面或平台权限 → 用户管理）。
- 使用 HTTPS，或放在可信反向代理之后。
- 限制来源 IP、VPN 或堡垒机访问。
- 开启 `audit.enabled`。
- 不需要 C2 时设置 `c2.enabled: false`。
- 不暴露独立 HTTP MCP，除非设置强认证和网络隔离。
- 外部 MCP 只接可信服务。
- 备份 `config.yaml`、`data/`、自定义资源目录。

## 反向代理建议

Nginx 基线：

```nginx
client_max_body_size 200m;
proxy_buffering off;
proxy_http_version 1.1;
proxy_set_header Host $host;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto https;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

建议额外加：

```nginx
add_header X-Content-Type-Options nosniff;
add_header Referrer-Policy no-referrer;
add_header X-Frame-Options DENY;
```

## HTTP 信任边界与资源限制

服务器默认忽略 `X-Forwarded-For` 和 `X-Real-IP`，按 TCP 对端地址限流。反向代理部署必须在 `server.trusted_proxies` 中填写实际代理的 IP 或 CIDR；不要填写客户端网段。空列表适合直接访问，`*`、`0.0.0.0/0`、`::/0` 和主机名会被拒绝。代理应覆盖来源头，且后端端口只允许代理连接。单层代理可使用 `proxy_set_header X-Forwarded-For $remote_addr;`；多层代理仅追加经过逐跳信任检查的地址。

```yaml
server:
  trusted_proxies: ["127.0.0.1", "::1"] # 仅示例：应用与代理同机
  read_header_timeout_seconds: 10
  read_timeout_seconds: 300
  idle_timeout_seconds: 120
  webhook_max_body_bytes: 1048576
```

省略或设置为 0 使用上述默认期限和 1 MiB 回调上限；超时字段可设为 1–86400 秒，回调上限可设为 1–67108864 字节，负值或超范围会导致启动失败。主服务与独立 HTTP MCP 均应用读取/空闲超时；不设全局写超时，保持 SSE、WebSocket 和 MCP 长连接输出。`read_timeout_seconds` 包含上传读取时间，应按实际带宽和允许的文件大小配置。企业微信缺少签名参数时不读取请求体，超限返回 HTTP 413；完整签名及重放校验仍然执行。其他回调和附件的体积限制沿用原有逻辑。

访问日志只记录 HTTP 方法、路由模板、状态、客户端 IP 和耗时，异常日志不记录请求转储或 panic 原文。SSE/WebSocket 的查询参数认证仍受原有认证中间件限制。反向代理也须去除查询串日志：在 Nginx `http` 块定义下面的格式，并在相应 `server` 块使用它；不要记录 `$request`、`$request_uri`、Authorization 或 Cookie。

```nginx
log_format cyberstrike_safe '$remote_addr $request_method $uri $status $body_bytes_sent';
access_log /var/log/nginx/cyberstrike-access.log cyberstrike_safe;
```

配置调整需要重启服务。本次改动不修改现存日志或已签发会话；若历史日志已含有效 Token，应在保留审计证据后按事件处理流程撤销相应会话。新日志设置不会替代已有日志的访问控制。

外部 MCP 完整配置及连接错误详情仅向具有全局 `mcp:write` 权限的用户返回。只读用户获得运行状态、传输类型、URL 的协议和主机等元数据；命令及参数被省略，环境变量和请求头值被掩码，URL 的用户信息、路径、查询串与片段被省略。

## HITL 白名单基线

推荐最小白名单：

```yaml
hitl:
  tool_whitelist:
    - read_file
    - glob
    - grep
    - tool_search
```

不要默认加入：

- `execute`
- WebShell 写入/执行工具
- C2 任务和 payload 工具
- 外部 MCP 高风险工具
- 删除、写入、上传、持久化相关工具

## 文件权限

建议：

```bash
chmod 600 config.yaml
chmod 700 data
```

生产环境使用独立系统用户运行：

```text
cyberstrike-ai:cyberstrike-ai
```

避免 root 运行，除非明确需要绑定低端口或访问特殊资源。

## 外部 MCP 审查

接入前确认：

- 工具是否能执行命令。
- 是否能读写本机文件。
- 是否会把数据发往第三方。
- 是否有自己的认证。
- 是否会返回不可信网页或模型内容。
- 是否需要容器隔离。

接入后：

- 高风险工具不进白名单。
- 定期检查工具列表变化。
- 审计配置变更。

## C2 和 WebShell

C2：

- 默认关闭。
- 演练窗口临时开启。
- Listener 端口与管理端口分离。
- 结束后清理 payload、session、task、event。

WebShell：

- 只保存授权目标。
- 使用清晰命名。
- 写入/删除/执行必须审批。
- 项目结束删除连接。

## 数据保留

建议：

- 审计：30-90 天。
- 工具监控：90-180 天。
- 上传附件：项目结束清理。
- C2/WebShell 输出：只保留报告需要的证据。
- 知识库：不放真实凭证和客户私密数据。

## 周期巡检

每周：

- 登录失败和异常 IP。
- 配置变更。
- 外部 MCP 增删改。
- 长时间运行工具。
- C2 是否被意外开启。
- WebShell 连接是否过期。
- 磁盘空间和数据库大小。

每个项目结束：

- 清理临时 workspace。
- 删除无用附件。
- 归档必要证据。
- 删除过期 WebShell/C2 资源。
- 导出审计记录。
