# [Bug] Eino ChatModel 节点偶发 400（Invalid request body）于工具执行后的第二次模型调用

## 环境信息
- CyberStrikeAI-EV-SRC `v1.6.51-src`
- 模型：`glm-5.2`，`base_url: https://ark.cn-beijing.volces.com/api/coding/v3`（火山方舟 **Coding Plan** 端点），`provider: openai`
- `reasoning: {mode: on, effort: high, profile: auto}` -> 经 `resolveWireProfile` 走 `wireOpenAI`，`applyOpenAICompat` 向请求体注入 `reasoning_effort: "high"`
- 依赖：`eino-ext/components/model/openai v0.1.13`、`eino-ext/libs/acl/openai v0.1.17`、`meguminnnnnnnnn/go-openai v0.1.2`
- 编排模式：`eino_single`（`/api/eino-agent`）

## 问题描述
渗透测试会话中，**工具执行完成、Agent 再次调用模型的那个节点 `[node_1, ChatModel]`** 偶发抛出 400，终止整轮编排。错误消息为泛化形态（无字段名），三次实测样本分别为：

| 消息 | Request id |
|---|---|
| `Invalid request body` | `021785260356895aa4407e4e0ac0c4c28f824a6d9e4aeeecc6604` |
| `A parameter specified in the request is not valid` | `021785259659723aa4407e4e0ac0c4c28f824a6d9e4aeee2f88ac` |
| `messages 参数非法。请检查文档。` | - |

完整报错形态：`[NodeRunError] error, status code: 400, status: 400 Bad Request, message: <上述消息> Request id: ...`

## 根因分析（已证实）
**根因：ARK glm-5.2 coding-plan 网关要求 `messages` 数组至少含一条 `user` 角色消息，否则返回泛化 400；而摘要收尾逻辑丢失了原始 `user` 消息。**

1. **错误格式溯源**：报错串与 `meguminnnnnnnnn/go-openai@v0.1.2/error.go:41` 的 `APIError.Error()` 完全一致（`error, status code: %d, status: %s, message: %s`）。**无 `body:` 后缀** ⇒ 已解析的 `APIError`（非网络层 `RequestError`），`e.Message` 取自 ARK 响应体 `error.message`。即 ARK Coding Plan 网关真实返回的 400。

2. **诊断抓取（已实现 `internal/openai/request_diag.go`）**：4 次复现的请求体 `req_summary` 完全同构--
   - `messages_seq = ["system","assistant","assistant(tc=1)","tool(tci=...)","assistant(tc=1)","tool(tci=...)", …]`
   - `reasoning_effort:"high"`、`stream:true`、`stream_options`、`tools:24~26`、`req_body_bytes:155K~178K`
   - **无孤儿 tool、无空/null content、无 schema 异常** -- 结构本身合法。
   - 关键：**整条 `messages` 数组不含任何 `user` 角色消息**。

3. **curl 二分验证**（同一 `/api/coding/v3` 端点）：

   | 变体 | 结果 |
   |---|---|
   | 复刻真实结构（无 user） | **400** `InvalidParameter "A parameter specified in the request is not valid"` |
   | 去掉 `reasoning_effort` 但仍无 user | **400**（推理字段无辜） |
   | **加一条 `user`** | **200** ← 解药 |
   | `user` 放最前 / 放最后 | 均 **200**（只要存在即可） |
   | `system + assistant`（最简无 user） | **400** |

   结论：**网关要求至少一条 `user` 消息**；缺失即返回**泛化** 400（无字段名）。

4. **丢失点定位**：`internal/multiagent/eino_summarize.go` 的 `summarizeFinalizeWithRecentAssistantToolTrail` 在摘要压缩后只保留 `system + 摘要(assistant) + 末尾若干 round`。原始 `user` 消息位于对话**开头**，不在末尾保留下来的 round 里，遂被丢弃。压缩后下发给模型的 `messages` 变成 `system → assistant(摘要) → assistant(tc) → tool → …`，无 `user` → 触发 400。该 Finalize 为 eino_single / deep / supervisor / plan_execute 共用，故四种编排模式均受影响。

> 说明：此前一度怀疑是 `reasoning_effort` 或中间件改写造成的结构异常，经诊断抓取真实请求体 + curl 二分后**排除**，确定为「摘要丢 user」。

## 修复（已实现并部署）
`internal/multiagent/eino_summarize.go` 新增 `ensureRecentUserMessage`，在摘要收尾的三个返回点包裹：若最终 `messages` 不含 `user`，则从压缩前原始历史取**最近一条 `user`** 消息插到摘要之后（保持末尾 assistant/tool 轨迹不动，下一轮模型调用仍从 tool 结果续跑）。既满足网关 user 角色要求，又让模型在压缩后仍保有任务意图。一处修复覆盖全部编排模式。

## 诊断（已实现，保留）
`internal/openai/request_diag.go` 经 `AttachRequestErrorDiagTransport` 挂载于 `eino_single_runner.go` 与 `runner.go`。仅缓冲 ≥400 响应（小 JSON 错误体），200/流式原样透传不打断 SSE。输出 `req_summary`（含 `messages_seq`、`messages_orphan_tool_idx`、`messages_empty_content_idx`、`tools_bad_schema`）+ 解析后的网关错误 + 原始请求体头部，便于定位后续任何 4xx。
