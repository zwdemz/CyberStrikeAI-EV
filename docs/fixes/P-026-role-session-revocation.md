# P-026：角色维护的会话失效范围

## 工作流与维护检查

- 请求：修复角色更新/删除使无关用户掉线的问题。阶段：缺陷反馈与修复（P10）。
- 上游依据：用户提供的 A/B 结果、已核对的调用链及明确修复授权；无需新产品、UI 或 API 决策。允许实施。
- 范围：handler 角色更新/删除、AuthManager 精确撤销、回归测试；不更改数据库结构、API 或部署。
- 验收：删除零成员角色保持所有原有 token 有效；变更权限/scope 或删除有成员角色只撤销成员；描述/名称及等价权限集合变更不撤销；失败操作不撤销。
- 维护检查：rbac.go 429 行，auth_manager.go 266 行；数据库文件 1453 行，本次只读取，不添加逻辑。选择 narrow_fix，无需先做结构重构。已有 handler 测试缺少携带真实会话的角色维护回归。
- 假设：现有 Session.Roles 是登录时的角色 ID 快照，适合作为精确失效依据。作用范围仍为当前 AuthManager 实例。

## 实现设计与验证计划

在 AuthManager 增加按角色 ID 撤销方法，直接匹配会话快照的 Roles，覆盖同成员多个 token 和多个角色。角色删除后仍可匹配旧会话，不依赖已删除的数据库关联。

更新前读取旧权限集合，更新成功后比较规范化权限集合和数据库实际保存的 scope；权限顺序、重复、空白不构成实质变化。错误路径保留会话。串行化同 handler 的角色更新/删除，避免旧状态比较被并发角色维护打乱。

登录记录会话撤销版本；发布会话时检查版本，若期间发生角色撤销，则重新读取授权，避免撤销之后再写入旧快照。版本变化只要求并发中的登录重新解析，现有无关会话保持有效。

验证：真实登录 token 的 handler HTTP 回归、security 单元测试、相关包测试与 race 检查、git diff --check。部署及线上复验不在本次范围。

## 实现记录

- `internal/handler/rbac.go`：更新角色前读取权限集合，成功后按权限/scope 变化精确撤销；角色删除改为按角色撤销；用互斥锁串行化角色更新与删除。
- `internal/security/auth_manager.go`：增加 `RevokeRoleSessions`，匹配登录快照而非删除后的数据库关系；登录发布通过版本检查防止撤销后重新写入旧授权。用户授权撤销和全局撤销也推进版本，维持相同安全边界。
- 新增两个回归文件；中英文 RBAC 管理指南及 Unreleased changelog 已同步更新。
- 分支：`codex/fix-rbac-role-session-revocation`。修复已完成，进入检视与 main 合并流程；尚未部署。检视及合并结果另见本次检视记录。

## 验证记录（2026-10-10，Asia/Shanghai）

- 新增 HTTP 回归 12 个场景全部通过，含真实 admin、无关 viewer、多角色成员和同成员多个 token；权限收回或角色删除后重新登录不会保留 `chat:write`，scope 收窄后重新登录得到新 scope。
- `go test ./internal/security ./internal/handler`：两个包全部通过。
- `go vet ./internal/security ./internal/handler`：通过。
- `git diff --check`：通过。
- 原实现对照：使用临时 Go overlay 将两个生产文件映射到基线 HEAD，保持工作树不变，运行同一 HTTP 回归。8 个成功维护场景检出管理员旧 token 的 `401`（预期 `200`），证明测试可识别原缺陷；对照测试退出码 1 为预期结果。
- `go test -race ./internal/security ./internal/handler -run 'TestRBACRoleMaintenanceSessionIsolation|TestRevokeRoleSessions|TestPublishSession|TestAuthManagerAuthenticatesCreatedRBACUser' -count=1`：通过，无 race 报告。

## 边界

作用范围仍为当前服务实例。已经通过鉴权并持有 Principal 的在途请求/后台任务不由会话撤销中断；此次修复保证后续 token 鉴权及新登录快照的行为。上线需按现有发布流程部署，生产环境尚未复验。回滚可恢复本次涉及文件的基线实现，但会恢复全员掉线缺陷。
