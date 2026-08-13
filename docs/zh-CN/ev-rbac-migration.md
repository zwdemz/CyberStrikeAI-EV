# EV RBAC 迁移说明

本分支以 CyberStrikeAI 上游 RBAC 主线为基础，并保留 CyberStrikeAI-EV 的 SRC/EDU AI 测试策略与本地 Skills 资源。

## 两套角色相互独立

| 类型 | 位置 | 作用 |
| --- | --- | --- |
| 平台 RBAC 角色 | 平台权限页面、`/api/rbac/*` | 控制账号可访问的 API、资源和高风险能力。 |
| AI 测试角色 | `roles/*.yaml` | 控制 Agent 的提示词、测试策略和可选工具。 |

选择“企业SRC渗透测试”或“EDUSRC渗透测试”不会绕过平台 RBAC；用户必须同时拥有调用相应资源与工具的权限。

## 已保留的 EV 定制

- `roles/企业SRC渗透测试.yaml`
- `roles/EDUSRC渗透测试.yaml`
- `skills/` 下的本地安全测试与报告 Skills 资源。

上述两个 AI 角色的主体提示词、工具策略和约束均已保留。原 EV 角色中依赖旧执行编排模块的 `record_vulnerability_candidate`、`upsert_execution_coverage`、`get_execution_coverage`、`should_continue_execution` 与 `logic_probe_diff` 已从工具白名单移除，因为上游 RBAC 基线尚未实现这些工具。这样可避免 UI 选择角色后模型请求不存在的工具。

## 初始化与升级影响

首次启动会自动创建平台 `admin` 用户并在控制台输出一次性初始密码。请立即登录并修改密码，然后在“平台权限”中创建操作员、审计员或自定义角色。

旧版单管理员内存会话不会迁移；升级后需重新登录。旧数据库应在升级前备份，建议在测试环境先启动并验证 RBAC 用户、角色与资源授权。

## 验证步骤

1. 登录后访问“平台权限”，确认管理员具有 `admin` 平台角色。
2. 新建一个 `operator` 或自定义用户，按需授予项目和工具权限。
3. 使用该用户登录，选择“企业SRC渗透测试”或“EDUSRC渗透测试”。
4. 验证未授权的资源、WebShell、C2、外部 MCP 和本地执行操作均被服务端拒绝。

完整权限目录、资源 Scope 与 API 请参阅 [RBAC 指南](rbac.md)。
