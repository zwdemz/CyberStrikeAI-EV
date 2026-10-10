# Token 用量统计

点击仪表盘的「Token 用量」卡片打开统计弹窗，不再跳转到聊天页面。支持 Enter/空格打开、Escape 关闭，沿用明暗主题。

- 时间范围：近 7、30、90 天，默认与卡片的近 7 天一致。
- 汇总：总 Token、模型调用次数、输入、输出、缓存和推理 Token。
- 明细：按数据库日期分组、按模型分组（最多 100 个模型）。
- 项目范围固定为打开弹窗时的仪表盘筛选；后端仍按当前用户的数据权限过滤。
- 请求超过 15 秒提示超时，可以刷新重试。错误或无权限不会显示成零用量。

数据来自现有认证接口 `GET /api/usage/tokens?days=7&limit=100`；选择项目时增加 `project_id`。接口要求 `dashboard:read` 权限，响应使用 `summary`、`byDay`、`byModel`。统计仅覆盖已记录的模型用量，不代表供应商账单；缓存和推理是细分指标，不能再加到总量。

Linux 浏览器回归（需要 Playwright 和 Chromium）：

```sh
NODE_PATH=/path/to/node_modules node tests/browser/dashboard-token-usage.cjs
```

用例使用合成数据，覆盖键盘操作、统计显示、项目范围、明暗主题、安全文本渲染、过期响应、权限错误和空记录。设置 `TOKEN_USAGE_SCREENSHOTS` 可指定截图目录。

该修复只涉及静态资源，不需要数据库迁移。部署时一起更新 HTML、dashboard.js、主题 CSS 和语言包；回滚时恢复相同文件组。页面缓存仍旧时刷新浏览器。
