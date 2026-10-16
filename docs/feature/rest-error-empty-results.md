# REST 删除错误与空列表修复

## F0：接入与门禁

- 分支：codex/rest-error-empty-results；起始提交 68df7c1，工作区干净。
- P10 反馈修复，用户已明确授权修改及必要验证。已有只读分析：工作区 reviews/P-022-rest-semantics/analysis.md。
- 涉及 handler 的 c2.go (1267 行)、webshell.go (1097 行)、vulnerability.go (632 行)、batch_task_manager.go (1486 行)。较大文件负责模块 HTTP 或队列业务；已有模块测试，缺少本次 HTTP 合同回归覆盖。
- Maintainability Gate：高风险文件采用 narrow_fix，只改错误分支及空切片初始化，不增加职责、不移动代码；无需先重构。
- Workflow Gate：上游接口及实现已核对，期望明确，无阻塞依赖；允许实施。验证使用临时 SQLite 和 httptest，不访问部署服务。
- 生命周期范围：完成修复、验证、文档及变更记录；不合并、不部署。

## F1：已确认需求

用户明确要求：C2 Profile 和 WebShell 删除不存在对象返回 404，不暴露底层 SQL 错误；漏洞和批量任务列表无匹配时返回 []；补充必要验证。单对象读取、其他模块错误处理、鉴权及部署不在本次范围。真实数据库失败仍为 500。

## F2：接口设计

保持原 JSON 字段名、成功删除响应和分页字段。C2 缺失映射为 {"error":"profile not found"}，WebShell 保持 connection not found。两者真正数据库错误记录内部日志，对外为稳定通用文案。列表在产生集合的现有方法初始化非 nil 空切片，覆盖数据库和内存路径。兼容性：消费者将收到 [] 而非 null；该变更为用户明确要求。

## F3：实施计划

改动 c2.go 的 DeleteProfile、webshell.go 的 DeleteConnection、database/vulnerability.go 的列表初始化、batch_task_manager.go 的 ListQueuesForAccess 初始化。添加独立 HTTP 合同测试，临时数据库验证缺失、成功删除和重复删除、真实 SQL 失败、空库、筛选无匹配及有结果列表；批量列表含内存路径。运行针对性测试及 handler/database 包测试、git diff --check。更新 changelog 与验证记录。回退使用本分支修复提交的 revert。

## F4：实施结果

- C2 Profile 删除将 sql.ErrNoRows 映射为 404；真实错误写内部日志，对外通用 500 文案。
- WebShell 删除使用 errors.Is 识别缺失，对外数据库错误改为通用 500 文案，底层已有日志保留。
- 漏洞数据库列表、批量队列业务列表初始化为空切片，成功的无记录查询序列化为 []；后者同时覆盖数据库与内存路径。
- 独立 httptest 回归覆盖缺失、成功删除、重复删除、SQL 错误脱敏、两类空列表、非空结果和批量内存分支。
- 修复前回归按预期失败，明确捕获原行为；修复后 `go test ./internal/handler -run '^TestREST(Delete|VulnerabilityList|BatchList)Semantics$' -count=1` 通过。

## F5：验证与消费者文档

- 修复前新增合同测试失败，分别捕获 C2 500、WebShell 表名泄露及两个列表 null（批量数据库/内存均覆盖）。
- 修复后针对性合同测试通过。
- `go test ./internal/handler ./internal/database -count=1` 全部通过（handler 20.597s，database 2.551s）。
- `git diff --check` 通过。新增及改动 Go 文件已 gofmt。
- 中英文 API 参考补充缺失删除、通用 500 和空列表 [] 合同。
- 未执行线上请求或部署验证；HTTP 回归使用真实临时 SQLite，通过路由调用 handler，未覆盖完整认证中间件。权限规则未修改。

## F6：合并准备与变更记录

- 修复提交：9f111a13；新增回归验证原缺陷可检出且修复后通过，两个受影响包完整测试通过。
- 差异自查：生产变更仅涉及四个方法，正常删除响应、鉴权、分页及非空列表行为保留；内部错误不会被缺失或空成功分支吞掉。
- API 参考和 CHANGELOG 的 Unreleased/Fixed 已更新。null 到 [] 的迁移属于本次明确授权的合同修复。
- 本分支各阶段已提交并推送；未创建 PR、合并或部署，发布后的线上验证留待部署阶段。

## F8：main 合并记录（2026-10-10）

- 用户明确授权验证通过后推送 main。
- 获取远端最新状态后，origin/main 仍为起始提交 68df7c1；main 快进合并修复分支至 3da1d22b，无冲突。
- 在合并后的 main 运行 `go test ./internal/handler ./internal/database -count=1` 全部通过（handler 20.748s，database 2.697s），`git diff --check origin/main..HEAD` 通过。
- 本次 main 推送包含修复、HTTP 回归测试、中英文 API 文档及 CHANGELOG；未执行部署或线上数据操作。
