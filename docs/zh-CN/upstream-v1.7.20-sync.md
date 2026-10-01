# 官方 v1.7.20 同步与部署

## 来源与合并范围

官方发布：[CyberStrikeAI v1.7.20](https://github.com/AIPentest/CyberStrikeAI/releases/tag/v1.7.20)。目标提交为 `a89f21b49e0dc7f629087b56c3341aa52a1439ef`。

EV 仓库原有 `v1.7.20` 标签指向 EV 提交 `18a77285`，并不对应上述官方源码。本次保留原标签，使用 `upstream/releases/v1.7.20` 引用记录官方目标，在 `codex/sync-upstream-v1.7.20` 分支同步。

EV 首次源码导入与官方 `7c45011074c213658c4e253000cec96b00ae8b47` 的代码一致，后续部分更新为手工移植。因此本次以该源码基线进行三方合并，同时补齐官方 v1.7.20 所需的 AgenticMessage/Eino 0.9.14 前置更新。

包含 TypeSafe Jev 审批后端、Eino exit/transfer 状态修复、项目统计图、任务进程生命周期、诊断日志以及配套界面与依赖更新。保留 EV 的 RBAC 批量资源标签查询、缺失工具提示与 Python 降级建议，以及路径边界、MCP 凭据、代理信任、日志脱敏和 Webhook 请求限制修复。

官方已移除会话分组界面与接口；会话记录继续使用现有数据库，本次同步脚本不删除运行数据库。旧 Eino 实现文件按官方结构替换。保留 EV 原有技能目录管理方式。

回归中额外修复日志文件未关闭、同时间戳历史占位消息未正确结束，以及 Windows 测试资源回收与文件权限断言问题。HITL 前端测试更新为新版默认配置初始化契约。两项数据库时间测试改为临时数据库与固定数据，避免打开项目的真实数据库。

## 本机与 Ubuntu 构建

依赖：Go 1.25 或以上（本次 Ubuntu 使用 Go 1.26）、CGO 所需 C 编译器；Node.js 用于前端测试。模型服务与安全工具按项目配置单独提供。

```bash
go build -mod=readonly -o cyberstrike-ai ./cmd/server
go test -mod=readonly -p 2 ./... -count=1 -timeout=180s
node --test web/static/js/*.test.cjs
./cyberstrike-ai --config ./config.yaml
```

Windows 构建输出改为 `cyberstrike-ai.exe`，启动使用 `./cyberstrike-ai.exe --config ./config.yaml`，并准备 GCC/MinGW。核心 Web 服务可运行；内置终端及依赖 `/bin/sh` 的工具仍需要 Linux、WSL2 或容器。

现有配置缺省时继续使用 OpenAI 兼容审批后端。只有选择 `hitl.audit_backend: typesafe` 时才需要配置其独立 API Key；不要将主模型凭据当作 TypeSafe 凭据。

## 容器构建

仓库根目录提供多阶段 `Dockerfile`，以 UID 10001 运行；`.dockerignore` 排除本机配置、数据库、日志、构建输出与本地克隆的工具仓库。

```bash
docker build -t cyberstrike-ai-ev:1.7.20 .
mkdir -p deploy
cp config.example.yaml deploy/config.yaml
# 编辑 deploy/config.yaml：监听 0.0.0.0，设置模型、认证与环境参数。
docker run --rm --name cyberstrike-ai-ev -p 8080:8080 \
  --mount type=bind,src="$(pwd)/deploy",dst=/app/config \
  --mount type=volume,src=cyberstrike-data,dst=/app/data \
  --mount type=volume,src=cyberstrike-log,dst=/app/log \
  --mount type=volume,src=cyberstrike-uploads,dst=/app/chat_uploads \
  cyberstrike-ai-ev:1.7.20 --config /app/config/config.yaml
```

配置目录及文件须允许 UID 10001 写入，供配置页面原子保存。镜像包含核心服务、Web 资源、默认工具定义和基础 Python；各扫描工具及 Python 第三方依赖需按启用的配置在派生镜像中安装。容器构建与运行需在具有 Docker 的环境另行验证。

## 测试机部署与回滚

测试项目目录为 `/home/ubuntu/CyberStrikeAI-EV`。同步前完整备份为 `/home/ubuntu/CyberStrikeAI-EV-backup-v1.7.20-20261001-193840`；部署证据位于 `/home/ubuntu/.codex-deploy/cyberstrikeai-v1.7.20-20261001-193840`。

代码同步前校验旧文件摘要；测试机有本地改动的 4 份工具配置做三方合并。模型凭据、数据库、角色、技能、知识库等运行数据保留，`config.yaml` 仅将版本号改为 `v1.7.20`。服务程序在 Linux 构建和回归通过后更新。使用独立配置、临时数据库与回环地址进行启动检查，避免改动真实业务数据。

2026-10-01 验证结果：

- Ubuntu 全量 `go test ./...`：34 个测试包通过，1,283 项用例/子用例通过、3 项跳过。跳过项是 2 个需要委托 cgroup v2 环境的测试和 1 个仅供子进程调用的辅助入口。
- 前端 19 份测试文件：169 项通过。
- Windows 构建、可运行的 Go 测试与安全回归通过；临时测试程序受系统执行限制的包改用固定路径补验。4 项 POSIX Shell 用例限定在非 Windows 环境执行，并已在 Ubuntu 通过。
- Windows 与 Ubuntu 隔离启动均通过：首页 200、HTML 正常、未登录 API 401、SQLite 成功创建。测试进程已停止，Ubuntu 新程序已安装，服务保持更新前的未启动状态。
- 未使用真实模型凭据调用模型或 Jev 服务，未执行外部扫描工具；Docker 构建与委托 cgroup 隔离尚未实机验证。

### 数据库测试副作用与补救

首次全量测试发现，上游 `audit_time_test.go` 和 `project_time_test.go` 会打开 `data/conversations.db`，触发 `NewDB` 的正常迁移与 WAL 检查点写入。该副作用同时发生在 Windows 本地库和 Ubuntu 测试库，不能将最初的测试通过视为数据未变更。

Ubuntu 已保存测试后副本，并从本次更新前完整备份恢复原数据库及 WAL/SHM 文件，逐文件 SHA-256 与原备份一致。Windows 没有可核验的更新前数据库备份，已保存迁移后完整副本并通过 SQLite `quick_check`，保留当前数据，未宣称恢复到更新前状态。两项测试现已使用临时模拟数据；修复后在两端复测数据库包，并核对真实数据库摘要不再变化。

需要回滚时，先停止本项目进程，将当前项目目录重命名留存，再将上述完整备份复制回原项目路径，以同一用户启动原 `cyberstrike-ai`。不要将旧二进制直接搭配已经由新版本修改过的运行数据库；完整备份同时包含代码、配置、数据和程序。

GitHub 交付使用带 GPG 签名的 `codex/sync-upstream-v1.7.20` 更新分支，原有 EV 标签保持不变；合入主分支另行处理。
