# 普通 API 客户端标识

在运行时 `config.yaml` 中配置：

```yaml
api_client:
  user_agent: "CyberStrikeAI"
```

此设置统一用于信息收集客户端向 FOFA、ZoomEye、Quake、Shodan 发出的 HTTP 请求，包括页面查询和复用该客户端的 Agent 查询。它不改变模型 SDK、外部 MCP、扫描工具、WebShell 或 C2 的标识。

省略字段、空字符串或仅空格时使用 `CyberStrikeAI`，不再固定发送旧版本号。自定义值最多 256 字节，只接受可打印 ASCII；首尾空格在发送前移除。换行、制表符等非法字符会导致配置加载失败，错误消息不回显配置值。

修改配置文件后重启服务生效。本项没有 Web 设置控件。升级前备份配置，旧配置无需迁移；回滚时恢复原二进制及配置备份。
