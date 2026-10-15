# Windows 原生运行（第二阶段）

第一阶段覆盖主服务构建、启动、登录、设置保存、项目、会话、资产、漏洞和知识库持久化。Linux 的启动方式和容器部署保持不变。

## 环境与构建

- Windows x64，PowerShell 5.1 或更新版本。
- Go：版本以仓库 `go.mod` 为准；允许 Go 自动下载对应工具链。
- GCC：与 Go 目标架构匹配，并加入 PATH。SQLite 使用 CGO，不能以 `CGO_ENABLED=0` 作为可运行版本发布。
- 保留完整仓库资源，尤其是 `web`、`roles`、`agents`、`tools` 和配置模板；单独复制 exe 不构成完整部署。

```powershell
.\scripts\build-windows.ps1
.\run.ps1
```

构建产物为 `bin/cyberstrike-ai.exe`。启动脚本自动切换到仓库根目录，路径含空格时无需拼接命令字符串。首次运行由主服务创建 `config.yaml`，不会覆盖已有配置。使用启动输出中的实际 HTTPS 地址；初始管理员口令只用于本机登录，随后修改。

```powershell
.\run.ps1 -ConfigPath 'config.local.yaml'
```

相对配置路径按仓库根目录解释。现有运行数据依赖工作目录，因此不要直接从其他目录运行 exe。配置内含密钥时使用环境变量，并通过 Windows ACL 限制配置及数据目录的访问；POSIX 文件权限位不等同于 Windows ACL。

## 配置和数据

保留 `config.yaml`、`data`、`knowledge_base`、`chat_uploads`、`skills` 以及需要保留的 `tmp` 工作区。知识库、模型 API 和工具依赖仍需独立配置。

在前台控制台按 Ctrl+C 停止，等待进程完全退出后备份。升级前备份程序、资源、配置和全部数据库；数据库包含 WAL 时必须一起保存。失败回滚时恢复同一份完整备份，不能只恢复旧 exe 而保留已迁移数据库。

启动配置错误返回非零退出码，便于脚本识别失败。当前启动脚本不是 Windows 服务管理器，不提供后台服务安装。

## 验证

```powershell
python -m pip install PyYAML==6.0.2
python tests/native/service_smoke.py --binary bin/cyberstrike-ai.exe
python tests/native/service_smoke.py --binary bin/cyberstrike-ai.exe --https
go test -p 2 ./internal/config ./internal/database ./internal/knowledge ./internal/processguard -count=1 -timeout 5m
```

集成测试在含中文和空格的临时目录启动真实服务，使用本地模拟聊天/嵌入接口和生成的登录口令，验证 HTTP/HTTPS、鉴权、参数校验、配置保存、聊天回复和记录持久化、向量索引、优雅关闭、重启和数据库完整性。不会访问真实模型或执行扫描工具。CI 使用同一测试覆盖 Windows 和 Linux。

## 尚未覆盖的能力

- 第三阶段：工具安装、平台支持状态、Windows 发布包和升级自动化。
- 不宣称全部安全工具支持 Windows；依赖 Linux Shell 的工具不能直接使用。
- 本阶段的会话记录验证不等同于完整 Agent 工具执行验证；真实模型服务兼容性需另行测试。
- Windows 存储容量查询尚未适配，不能把“不可用”解释为磁盘空间充足。

## 第二阶段：命令与交互终端

Windows 未指定 shell 时默认 PowerShell，Linux 仍默认 sh；支持显式 cmd.exe 或已安装的 pwsh.exe。命令须符合所选 shell 的语法，不自动翻译 POSIX 脚本。普通 PowerShell 命令采用非交互 UTF-8 输出；cmd 和外部程序可能保留自己的代码页。后台任务使用受管理的后台入口，不将末尾的 & 当作 POSIX 后台标记。

每条 Windows 命令由独立 Job Object 管理，取消时清理子进程且不影响同任务其他命令。命令 Job 嵌套在任务 Job 下，保留任务总资源限制；守护进程也计入进程数。隔离失败不会降级为无隔离执行。

交互终端采用 ConPTY（Windows 10 1809+），支持输入、窗口缩放和断线回收，保留现有认证和同源检查。ConPTY 不可用时明确报错，可改用普通命令执行。SSE 按块发送无换行长输出，展示上限为 256 KiB，超过后继续排空管道。

```powershell
go test -p 1 ./tests/internal/security ./tests/internal/handler ./tests/internal/terminalpty -run 'TestWindows|TestNativeConsole' -count=1 -timeout 2m
go test ./internal/multiagent -run TestEinoStreamingShellWrap -count=1 -timeout 2m
```

测试覆盖中文/引号、显式 cmd、Eino 执行、子进程清理、同任务命令独立取消、长输出、请求取消、终端输入/缩放和 WebSocket 断线回收。工具安装、支持状态和发布包仍属于第三阶段，不代表全部工具配方已兼容 Windows。
