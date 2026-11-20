# EV v1.8.0 同步与迁移

## 运行环境与保留能力

基于上游 v1.8.0，使用 Pi 1.5.2。Go 负责权限、项目、MCP 执行、证据与持久化，隔离 Node worker 负责模型会话。需要 Go 1.26.9、Node.js >=22.19 与 npm；仅替换 Go 二进制不能完成升级。

保留 EV 的固定代理出口及不支持工具阻断、主子代理共享的 SRC 工具限制、MCP 连续失败冷却、任务索引与证据关联、漏洞去重、定级与历史记录、权限范围内的 Token 统计。模型侧工具别名在宿主审批和执行前还原为原始 MCP 名称。旧 Eino 用量记录继续参与查询和展示。

## 构建与配置

在项目根目录执行：

```sh
go run ./cmd/build-runtime -root runtime/pi
go build -mod=readonly -trimpath -o cyberstrike-ai ./cmd/server
```

配置示例使用 `agent_runtime.engine: pi`、`multi_agent.runtime` 和 `multi_agent.skills`。读取时迁移旧配置键，设置保存时输出新键。部署须保留配置、数据库、已安装工具、Skills 和上传文件，不能用示例覆盖私有配置。

Docker 内置 Node，预构建 worker 位于 `/opt/cyberstrike/pi`，不会被原有 `/app/runtime` 配置数据卷遮盖。Windows 构建也先准备 worker；离线分发包只包含匹配源码指纹的 worker 和锁定依赖，不包含会话状态。目标 Windows 主机仍需安装 Node。

Windows 离线 worker 分发包应在 Windows 上生成，不应复用 Linux 的 `node_modules`。

## 升级与回滚

1. 等待活动对话、工具、批量任务和工作流结束后再停服。旧 Eino Workflow 二进制检查点无法在 Pi 中恢复，须先完成旧任务，或导出证据后创建新任务。
2. 在独立目录完成构建和验证。备份旧二进制、源码、配置、数据库及 SQLite 附属文件、工具和运行资源。数据库应在停服后复制，或通过 SQLite backup API 创建一致性备份。
3. 同步更新服务源码、静态资源和预构建 Pi worker，保留原会话与证据文件。通过原服务管理器启动后，核对 HTTPS、鉴权、数据库完整性和最小授权任务。
4. 失败时先停新服务，再一起恢复匹配的旧二进制、源码、配置和数据库备份。源码回滚会选择匹配的旧 Pi bundle，不能覆盖运行中 worker 的依赖目录。数据库回滚会丢失备份后的写入，应先保存新证据。

保留最近两次完整部署备份。现有公开 Tag、Release 不移动；新 EV 版本应在 dev PR 通过 CI 并合入 main 后指向已验证的发布提交。

## 验证边界

Linux 验证使用隔离数据库、模型模拟响应和本地监听，不调用付费模型或公网代理。覆盖角色权限、代理策略、任务证据、定级历史、去重、Token 持久化、worker 生命周期和服务重启持久化。真实供应商可用性和目标行为不由这些用例证明。本机不可执行时，Windows CI 和容器构建仍是发布门槛。
