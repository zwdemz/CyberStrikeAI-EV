# 官方 v1.7.21 同步

本次以官方 v1.7.20 提交 `a89f21b49e0dc7f629087b56c3341aa52a1439ef` 为基线，移植 [v1.7.21](https://github.com/AIPentest/CyberStrikeAI/releases/tag/v1.7.21) 提交 `82b0af10f8519bf4baaa08958a8dbe38299a7685` 的变更。开发直接在 `dev` 进行，经签名提交和 `dev → main` PR 发布。

## 变更

- 新增俄语界面、浏览器语言识别、语言偏好持久化与地区化日期显示；缺失译文回退英语。
- 新增运行空间统计、清理预览与按类别清理，接入平台权限及审计。自动清理默认关闭；实际删除要求显式确认。
- 同步会话清理及存储活动查询，保留 EV 工具调用拦截、角色范围、DNSLog MCP 与工具就绪检测。
- 摘要请求按供应商选择输出限制参数，避免同时发送两种限制。EV 的一次精简重试继续沿用原始预算，输出截断时不接受不完整摘要。
- 依赖安全扫描发现既有依赖存在可达漏洞记录，升级 MCP Go SDK、AWS EventStream、OpenTelemetry、gRPC 和 Go 扩展库至包含修复的版本；联动更新必要的传递依赖，保持 Go 1.26.8。扫描结果表示静态调用可达，不等同于已验证可利用。

## 配置迁移

新默认摘要输出预留从 8192 提高为 40960。已有部署若未显式配置 `multi_agent.eino_middleware.summarization_output_reserve_tokens`，升级前应根据模型上限填写该值；填写 `8192` 可保留升级前行为。显式配置值保持不变。模型凭据、角色、工具依赖和运行数据均应保留。

`storage.auto_clean` 默认关闭。上线初期保持关闭，使用清理预览核对类别和保留期后再自行启用。存储读写分别受 `storage:read`、`storage:write` 权限约束。

## 验证与部署

```bash
go test -timeout 180s ./internal/storage ./internal/multiagent ./internal/config ./internal/database ./internal/security ./internal/handler ./tests/internal/handler
node --test tests/web/i18n-ru.test.cjs
go build -mod=readonly -trimpath -o cyberstrike-ai ./cmd/server
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

升级前备份旧程序、配置、受影响源码及一致性数据库快照；在独立目录构建和测试。确认无运行或排队任务后，平滑停止旧进程，替换源码与程序并沿用原有启动方式。版本号设为 `v1.7.21`，验证主页、新存储接口认证、数据库完整性与运行程序哈希。

回滚时停止新进程，恢复备份程序、配置及对应源码后重启。数据库只在确认需要时从一致性备份恢复，避免覆盖升级后写入的数据。备份和包含运行环境信息的部署记录应留在私有位置。
