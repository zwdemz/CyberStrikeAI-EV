# AI 执行过程

[English](../en-US/execution-view.md)

## Agent Swarm 环形视图

内环显示 Agent，外环显示工具，实线只连接当前事件页中明确记录的 Agent／工具关系。中心为当前会话，不推断 Agent 间的消息通信或目标连通性。点击节点可筛选右侧动作流；下方保留“计划 → 执行 → 验证”任务看板。

点击“回放本页动作”播放 8 秒 Three.js 粒子动画：调用从 Agent 到工具，结果反向返回。一个粒子对应一条已保存的调用／结果，最多 50 个；这是历史回放，不表示实时网络流量或 Token 速率。D3 提供稳定环形布局。页面替换时释放 GPU 资源，离开页面、隐藏标签、切换时间线或启用减少动态效果时停止回放。窄屏、WebGL 或依赖不可用时保留静态图、键盘节点操作和动作列表。

D3 7.9.0、Three.js 0.186.1 及许可证、SHA-256 清单位于 `web/static/vendor/swarm/`，仅桌面非空图按需加载，不连接 CDN、不新增模型请求。本项目继续使用 Eino 已保存事件；参考 [AutoGen 的事件／消息区分](https://microsoft.github.io/autogen/stable/reference/python/autogen_agentchat.messages.html)和 [MASFactory 图式编排](https://github.com/BUPT-GAMMA/MASFactory)，无需引入另一套 Python 编排运行时。缺少真实 Agent 消息记录时不生成虚假通信边。

侧栏打开 **AI 执行过程** 并选择有权访问的会话，默认展示“计划 → 执行 → 验证”任务看板，读取 TaskCreate/TaskUpdate 保存的记录。待开始任务进入计划列，进行中任务进入执行列，Agent 标记完成的任务进入验证列，并明确显示“待核验”。看板只读，不通过拖拽更改任务，也不把 Agent 完成当作独立验证通过。展开卡片可查看任务说明、负责人和依赖任务。

看板上方是 Agent 作业调度面板，显示上述 Agent Swarm 环形关系图和最近动作流；最多绘制 5 个 Agent 和 5 个工具。点击节点筛选动作流，点击“全部事件”清除筛选。动作流最多显示当前页最近 8 条记录，深浅主题沿用项目样式。

下方看板展示会话最近保存的计划，不随上方关系图及时间线的助手轮次筛选，不冒充某一历史轮次的计划快照。会话结束后仍能查看；已停止会话的进行中卡片明确显示当前已停止。任务每页最多 50 条，列内计数仅针对当前任务页。

切换“动作时间线”后可选择助手轮次，查看已保存的轮次状态、迭代轮数、工具调用数、异常工具动作数和记录耗时。

时间线展示模型活动、工具动作及协调／审批事件。即使工具结果位于其他事件页，也根据已保存的工具执行摘要显示状态；未知状态和缺少结果会明确标示。查看页面不会执行工具或改变审批决定。

## 历史与刷新

- 会话每页 25 条，支持搜索；轮次选择器每次读取 20 条消息，点击“更早轮次”加载下一批，点击“最新轮次”返回最新记录。
- 事件每页最多 50 条。筛选只作用于当前事件页，顶部统计作用于当前选中轮次。
- 活动轮次或运行中的会话看板每 10 秒刷新，可关闭自动刷新以便阅读。不再活动时停止轮询，可手动刷新。
- 请求串行执行；切换会话、离开页面和隐藏标签页时取消读取。错误后延长刷新间隔；连续三次读取失败后暂停自动刷新，需点击刷新恢复。
- 临时网络失败保留上次成功结果并显示错误提示；认证、权限或资源不可用响应会清空执行记录。
- 支持 `#execution-view?conversation=<id>` 链接，沿用聊天的访问校验。错误提示不直接输出上游原始报错。

## 权限和数据边界

页面需要 `chat:read`。会话列表、消息和过程详情沿用现有鉴权及资源范围检查，没有额外的高权限接口和数据库。

时间线只渲染事件元数据：类别、工具／Agent 名称、时间、已记录的迭代和状态，不展示推理正文、工具参数、原始输出或任意事件消息；看板另行展示明确保存的任务说明。证据原文通过“打开原对话”核对。缺少任务或事件时显示空状态，不编造步骤。打开页面不会调用模型供应商。

## 最近计划接口

`GET /api/conversations/{id}/plan-tasks?snapshot=latest&limit=50&offset=0` 用于读取会话最近保存的计划，包括已停止会话。需要登录、`chat:read` 及会话访问权限。`limit` 为 1–100，`offset` 非负。返回 `tasks`、`total`、`completed`、`activeStep`、`running`、`snapshot`、`limit`、`offset`，例如 `{"tasks":[],"total":0,"completed":0,"activeStep":0,"running":false,"snapshot":"latest","limit":50,"offset":0}`。参数错误返回 400，未登录 401，无会话权限 403，记录不可用 404，存储异常 500。不传 `snapshot` 时，保留聊天页原有的只显示活动计划行为。

Windows/Linux 共用服务端接口和静态前端代码，不改变命令执行、进程隔离或启动行为。部署时等待活动任务结束，同步服务端、HTML、JS、CSS 和语言文件；回滚时一起恢复这些资源。本功能不需要数据库迁移或新增运行配置。

## 验证

```sh
node --test tests/web/*.test.cjs tests/web/*.test.mjs web/static/js/*.test.cjs
node tests/browser/execution-view.cjs
```

浏览器测试需要 Playwright 与已安装的 Chromium。本机可设置 `EXECUTION_BROWSER_CHANNEL=chrome` 使用已安装的 Chrome；设置 `EXECUTION_VIEW_SCREENSHOTS` 保存截图。Linux CI 使用默认 Chromium。测试数据均为模拟记录，不连接公网目标和真实模型。

![深色主题，使用模拟执行数据](../../images/execution-view-dark.png)
