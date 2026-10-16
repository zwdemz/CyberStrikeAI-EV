# 调用拦截配置修改冲突检测

## F0 Intake / Gates

2026-10-10，用户要求修复已核实的多用户规则覆盖问题，并明确目标为当前开源版 CyberStrikeAI。main 起点 a8321c4b，工作目录干净；分支 codex/tool-guard-conflict-detection。Pro 版由其他聊天处理，本分支不编辑其文件。

Workflow Gate：P10 缺陷反馈 → P7 局部修复 → P9 验证。上游为用户问题、截图、已完成的源码核实；允许实现保存冲突检测，验收同版本只有一次成功，失败保留草稿并提示刷新。

Maintainability Gate：config.go 2940 行、tool_guard.go 175 行、tool-guard.js 732 行。风险高，按 narrow_fix 执行；config.go 只加一个受现有锁保护的版本字段，行为放在现有专项 handler；不新增全局配置职责，不做无关重构。前后端接口联动，需 handler race 测试和现有前端测试。

Lifecycle Report：F0–F6；影响 handler、调用拦截 JS 和文档；权限和业务规则保持原有约束。GET/PUT 增加版本头，旧无条件 PUT 将拒绝，这是用户要求的覆盖保护所必需；客户端须先读取版本。阶段文档使用本文件，逐阶段提交推送。未要求合并、发布或部署。

## F1 Requirements

依据：用户已明确提出只有一个用户修改成功、其他用户失败并提示，随后明确要求实施修复且指定开源仓库。此处记录已有授权，不将未被确认的新文档标成用户批准。

- REQ-001 / AC-001：两个或更多客户端基于同一配置版本保存，只有一次成功；失败请求不改变成功方配置。
- REQ-002 / AC-002：冲突有合理错误提示；本地草稿保留，刷新获取最新配置后可以重新编辑保存。
- REQ-003 / AC-003：输入校验、权限、试匹配、落盘失败保持原策略行为继续成立。
- 范围解释：现接口提交整个列表，按整份配置保护，避免不同规则的旧快照相互覆盖。无需等待用户退出编辑，正常刷新后的后续修改允许保存。
- 非目标：编辑期间占用租约、多实例共享配置支持、发布或部署。

## F2 Design

GET /api/tool-guard 的 JSON 不变，提供强 ETag 和 Cache-Control: no-store。版本由随机 nonce 和当前有效配置摘要组成；每次成功保存更换 nonce（包括无变化保存），直接更新 manager 也会改变摘要，防止 ABA 旧版本恢复。版本状态归 ConfigHandler，读取/比较/推进由 h.mu 保护。

PUT 必须携带完整精确匹配的 If-Match；未提供返回 428/tool_guard_version_required，不匹配返回 409/tool_guard_conflict。校验、落盘、策略发布、版本推进在同一个保存临界区，失败不推进版本、不返回成功；成功返回新 ETag。星号、弱标签不作为覆盖保护绕过方式。服务重启后旧版本失效。

前端将 ETag 独立保存，PUT 携带读取时版本，成功才更新；冲突不采用错误响应上的新版本、不自动重试，保留原输入。中英俄文提示：配置已被其他用户修改，本次修改未保存，请刷新后重试。试匹配不带 If-Match，不更新版本。缺少响应版本不能静默发无条件写入。

兼容：旧 API 客户端需先 GET 再使用返回 ETag 提交 PUT；前后端同时升级，更新脚本缓存版本。整个列表任意编辑触发版本变化，不同规则同时保存也会冲突。仅保证当前服务实例内协调；多实例共享配置需另行引入持久化条件更新。

## F3 Implementation Plan

修改 internal/handler/config.go 一个字段、tool_guard.go 的版本协议；扩展专项 handler 测试验证真实并发单赢家、顺序旧草稿、无条件写拒绝、无变化保存、失败版本不变、manager 直接更新。修改 tool-guard.js 的版本状态和请求头，中英俄语言提示及 index.html 的 JS 缓存版本；扩展现有 VM 前端测试验证两个页面冲突、草稿保留、重试不换版本、成功更新版本、试匹配无副作用。更新中英文调用拦截文档、Unreleased changelog。

验证命令：go test -race ./internal/handler -run ToolGuard；node --test web/static/js/tool-guard.test.cjs；node --check web/static/js/tool-guard.js；git diff --check。必要时完整 handler 测试确认共享 ConfigHandler 无回归。测试只用临时目录和请求 fixture，不触碰线上业务数据。回滚需要前后端一起回滚，但会恢复覆盖风险；不执行部署。

## F4 Implementation

已实现持锁 ETag 比较与版本推进，强制条件保存；无条件写入 428，旧版本 409，成功保存返回新版本。前端独立记录基线版本，错误不会采用响应新版本，保留草稿；只在 GET/PUT 处理版本，试匹配不参与。中英俄提示及脚本缓存版本已更新。

专项 race 测试通过（3.246s），覆盖 12 个并发请求仅 1 成功、落盘/运行策略与成功回执一致、顺序旧草稿失败、缺版本/星号/弱版本拒绝、无变化保存推进版本、落盘失败可安全重试、manager 直接更新冲突。前端 36/36 通过（135ms），覆盖两个页面竞争、重试不偷偷换版本、草稿保留、重新读取后保存、失败版本保留及试匹配无版本头。JS syntax/diff check 通过；完整 handler 回归正在执行。

## F5 Verification / Documentation

- `go test -race ./internal/handler -run ToolGuard -count=1`：PASS，3.246s。读写和冲突比较均使用原锁，无数据竞争报告。
- `go test ./internal/handler -count=1`：PASS，20.512s。共享 ConfigHandler 和其他 handler 完整包回归通过。
- `node --test web/static/js/tool-guard.test.cjs`：36/36 PASS，135ms。
- `node --check web/static/js/tool-guard.js`、中英俄 JSON 解析与新提示存在校验、`git diff --check`：PASS。
- 中英调用拦截文档和 Unreleased changelog 已记录 GET/PUT 版本协议、错误码、草稿恢复和旧客户端迁移。
- 检查实际 apiFetch：使用 Headers 保留传入 If-Match，返回原生 Response，409/428 交由调用方处理。i18n 使用 no-cache；JS 模板缓存版本已更新。
- 未执行线上双账号写入、完整仓库套件或浏览器视觉检查：本次不部署、不改变视觉布局，测试使用临时文件、handler 请求上下文和现有前端 DOM fixture。多实例保证不在本次范围。

实现提交 `1ecbd405` 已推送；F0–F3 文档提交为 `65f947c9`、`7e6feeb9`、`23034ffa`、`4a1d0e11`，均已推送。

## F6 Review / Merge Readiness

局部复核：版本比较与文件写入、策略发布、版本推进在同一 h.mu 临界区；被拒绝请求在写入前返回；合法保存继续使用原 configFileMu 与原子 rename。前端仅成功 GET/PUT 更新版本，冲突或落盘失败不改变基线，不自动覆盖；无条件客户端无法绕过。HEAD 工作目录干净，阶段记录与实现均已推送，测试和稳定文档一致。

草稿 PR 描述：两名用户读取相同规则后分别保存时，原接口会接受旧草稿并覆盖首次保存。增加强 ETag/If-Match 保护，使同一版本仅一次成功，旧版本返回 409、缺版本返回 428；前端保留冲突草稿，并提供中英俄提示。验证：专项 race、完整 handler、36 项前端测试、JS syntax/JSON/diff 检查通过。API 调用方需先 GET 再携带 ETag 保存。整个规则列表按同一版本保护，需前后端同时升级。

代码修复完成，待 PR 审阅合并；未发布或部署，不声明线上已解决。多实例共享配置、编辑占用租约不在此修复范围。

草稿 PR：[AIPentest/CyberStrikeAI #351](https://github.com/AIPentest/CyberStrikeAI/pull/351)。F5 验证记录 7302c3d8、F6 审阅记录 8477bfcc 均已推送。远端镜像：未同步（本次未构建镜像）。

## F8 Merge Summary — 2026-10-10

用户授权检视无问题后合并 main。检视快照：base `a8321c4bff3a76fe0dfc3b576ed116e9a4d4101e`，head `5d44b59c2fbd8ec2df27949daa791f503cdbe988`；没有阻断 finding。重新执行专项 handler race（4.347s）、完整 handler（20.798s）、36 项前端测试（184ms）、JS syntax、三语言 JSON 和 diff 检查均通过；Self-update 三平台 CI 成功。

PR #351 在 2026-10-10 17:34:52 Asia/Shanghai 合并，merge commit `2335f1b639d0cd42a3ec7907e5e86f166a5d8f4a`。合并命令使用 match-head-commit 绑定检视提交，远端回执 merged=true；本地 main 已 fast-forward，合并树与检视 head 无差异。此记录替代上文待审阅合并状态。未发布版本、构建镜像或升级运行服务。
