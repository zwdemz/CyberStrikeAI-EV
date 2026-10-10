# 测试代理池

在「设置 → 测试代理池」管理测试请求的代理，按当前账号保存选择后生效，无需绑定项目。界面沿用现有明暗主题。AI 模型 API、知识库索引等服务请求保持原网络配置。

## 使用

1. 以具备 `config:write` 权限的账号进入设置，填写名称和并发限制。
2. 粘贴 Markdown 表格、CSV、TSV 或每行一个代理 URL，点击「检查导入」，核对脱敏预览后确认导入。
3. 选择代理池并点击「记住我的选择」。偏好按认证账号入库，跨会话和重启保留，不影响其他账号；选择「不使用代理池」可明确退出。仍需 `config:write` 权限。聊天中的自然语言不会自动更改此设置。
4. 如需验证连通性，先在配置 `test_proxy.probe_urls` 列表中登记自己控制的完整 HTTP/HTTPS 地址并重启，再填写相同地址进行节点测试。白名单默认空，禁止任意地址探测。测试仅发送一次 HEAD，不跟随重定向。

支持 `http://`、`https://`、`socks5://`；必须显式填写端口。示例仅为占位，不是可用代理：

```csv
Host,Port,类型,账号,密码,地区,状态,DB_id,ProxyAddr
proxy.example.invalid,1080,socks5,,,lab,启用,1,socks5://proxy.example.invalid:1080
```

优先使用 `ProxyAddr`；同时提供 Host/Port/类型时必须一致。相同端点重复记录会拒绝整批导入。每批最多 200 个节点、256 KiB，最多 50 个池。导入后节点列表不可原地修改：创建替代池，重新保存账号选择，再删除旧池。被账号选择、旧项目绑定或有在途请求的池不能删除。批量预览不发起网络请求。

## 执行范围

当前接入工具为 `http-framework-test`。账号启用代理后，不支持代理约束的 shell、扫描器和外部 MCP 执行入口会拒绝调用。资产、漏洞和项目事实等内部记录操作仍可使用。未设置偏好的账号保留旧项目约束以避免升级后意外直连；显式保存账号选择后以账号为准。

同一会话固定同一节点；节点故障进入冷却，不自动轮换，不直连，不重放请求。目标返回 403、429 或 5xx 不触发换代理。重复请求参数在绑定代理时不允许大于 1。`NO_PROXY` 和工具参数不能覆盖账号偏好。SOCKS5 域名由代理解析。

这是应用层工具准入与请求转发控制，不是操作系统级网络隔离。管理员自行执行程序、独立 MCP 客户端或额外扩展应单独配置容器/网络命名空间和出口防火墙。不要把此功能视作整台主机的流量接管。

## 配置与凭据

可在现有 `config.yaml` 顶层增加，修改后重启：

```yaml
test_proxy:
  max_concurrent: 16
  max_concurrent_per_target: 2
  queue_timeout_seconds: 30
  probe_urls: []
```

范围分别为 1–128、1–16、1–120。零或省略采用示例默认值。单池默认并发 4，单节点 1，连续代理连接失败阈值 3，冷却 60 秒；池 API 可配置阈值 1–10、冷却 10–3600 秒。并发排队可取消，超时与熔断会返回不同状态，不自动重试。并发和冷却计数是进程内状态，重启会重置；多副本不能共享额度。

带用户名或密码的代理需要启动环境变量 `TEST_PROXY_KEY`：32 字节随机值的 Base64 编码。使用秘密管理服务或权限为 0600 的服务环境文件注入，禁止写入仓库。可用 `openssl rand -base64 32` 在受控终端生成。未配置密钥仍可预览，但禁止保存带凭据的节点。密钥用于 AES-256-GCM 加密入库；列表、日志、预览不返回凭据。执行时通过子进程环境传递，主机管理员仍可读取进程环境。

数据库和密钥需分开安全备份。替换密钥前须使用原密钥解除相关项目绑定、删除旧池，然后在新密钥下重新导入；直接更换会导致既有凭据无法解密。

## API

所有接口均要求认证和 `config:write`；绑定接口额外要求项目写权限。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/test-proxy-pools` | 返回 `pools` 和 `health` |
| POST | `/api/test-proxy-pools/import` | `{name,text,preview,max_concurrent,per_node,failure_threshold,cooldown_seconds}`；返回脱敏池 |
| DELETE | `/api/test-proxy-pools/:id` | 删除未使用的池，返回 `{ok:true}` |
| GET | `/api/test-proxy-pools/binding/:id` | 项目当前 `{pool_id}` |
| PUT | `/api/test-proxy-pools/binding/:id` | `{pool_id}`；空字符串解除绑定，返回 `{ok:true}` |
| POST | `/api/test-proxy-pools/probe` | `{pool_id,node_id,url}`；返回 `{reachable,node_id,http_status,latency_ms}` |

成功为 200；无效参数/探测失败 400；未认证 401；无权限 403；池占用或绑定未解除 409；存储不可用 500/服务不可用 503。探测成功只表示收到 HTTP 响应，不代表目标业务健康；冷却未触发也不代表节点已验证可用。

## 存储、验证与部署

主数据库新增 `test_proxy_pools(id,document)` 和 `test_proxy_bindings(project_id,pool_id)`。池使用主键索引，绑定以项目 ID 为主键，外键关联项目和池；项目删除级联移除绑定。文档包含节点和加密凭据，无明文凭据。

Linux 安装依赖 `pip install -r requirements.txt`（包含 `httpx[http2,socks]`），运行：

```sh
go test -p 2 ./tests/internal/testproxy ./internal/security ./internal/app ./internal/multiagent ./internal/handler
python3 tests/tools/test_proxy_transport.py
go build -p 2 -o cyberstrike-ai ./cmd/server
```

运输测试只访问本机模拟代理，不连接真实目标或调用模型。发布前备份当前二进制、配置和数据库，替换二进制、Web 资源及 HTTP 工具配方后重启。已有容器需按原 Dockerfile 重建镜像以安装 SOCKS 依赖，并用容器秘密注入密钥。

回滚前停止绑定项目任务并解除绑定，再恢复旧二进制及配方；旧版本不识别绑定，不具备本功能的出口限制。新增表可保留，勿用旧数据库覆盖上线后的业务记录。

## 界面验证

截图仅使用合成数据。浏览器回归命令： `NODE_PATH=/path/to/node_modules node tests/browser/test-proxy.cjs` （需安装 Playwright/Chromium）。设置 `TEST_PROXY_SCREENSHOTS` 指定截图目录。

![Light theme](../../images/test-proxy/light.png)

![Dark theme](../../images/test-proxy/dark.png)

## 账号偏好与验证结果

`GET /api/test-proxy-pools/preference` 返回 `{pool_id,configured}`；`PUT` 接收 `{pool_id}`（空字符串为明确退出）。身份仅取认证上下文，不能提交其他账号 ID。未认证返回 401、无配置权限 403、无效 JSON 或不可用池 400、读取失败 500。旧项目绑定 API 保留用于迁移，不再显示项目绑定界面。

`test_proxy_preferences(user_id,pool_id)` 以账号为主键；账号删除级联删除，池外键防止悬空引用。已有项目绑定不会自动转移给其他账号。目标封锁不会自动启用代理、轮换出口或重放请求。

导入支持 Markdown（含尾部空列）、CSV、TSV、单行 URL 列表。重复列名、冲突字段、重复节点及未知状态拒绝整批导入；不会将拼写错误的状态默认为启用。导入的延迟仅是外部历史信息，不代表本机验证成功。

节点验证不跟随重定向、无直连兜底。HTTP 407 认定为代理认证失败；超时与取消单独提示。`reachable` 表示收到响应，`usable` 仅在测试端点返回 2xx 时为真；403、429、5xx 不能标为验证通过，也不触发更换代理。验证只是该时刻到指定端点的结果，不保证所有站点可用。
