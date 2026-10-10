# Skills 目录与场景配置

## 配置

在现有配置中合并以下字段，不覆盖其他 multi_agent 项：

```yaml
multi_agent:
  eino_skills:
    catalog_profile: src
```

`src` 展示 18 个常用候选，适合教育和企业 SRC；`all` 或省略保持全部候选。未知值会报配置错误。修改后在空闲时重启，下一次运行使用新目录。
仅隐藏默认描述，不删除技能；知道精确名称时仍可使用 `skill` 加载低频专项。文件系统访问不受目录筛选限制，真正的权限边界仍是角色策略与审批。该配置是实例级目录，不是每角色 ACL。

## 统一路径

宽泛任务 → pentest-agent-os → 查询 pentest-blackboard → 选择一个专项 → pentest-verification → pentest-output-standards。
明确专项可直接进入，普通开发或无相关攻击面的任务不加载专项。不要强制每轮调用技能；已有上下文可复用。

web-attack-methods 与 bug-bounty 保留兼容名称并转向统一入口。bug-bounty 原文迁入 references/legacy-workflow.md，按需读取，不作为执行授权。
unlimited-attack-scope 保留旧名称但改为授权边界说明。缺失的 CTF 安装脚本引用已移除；Burp 工作流使用仓库已有的入口。

## 效果和限制

技能加载成功仅证明读取成功，不能等同验证成功或漏洞有效。现有统计仍是加载次数，不据此删除零调用技能。
本次降低默认候选数量和总览正文长度；不承诺固定 Token 节省比例或漏洞产出提升。需在同类任务中比较加载次数、重复路径、有效证据与 Token 用量。
回滚可将 catalog_profile 改成 all 并在空闲时重启，不需要迁移数据库。

## 验证

Linux 执行 `go test ./tests/internal/skillcatalog ./internal/multiagent ./internal/config`。
验证 src 顺序、缺失包不广告、按名称访问低频技能、all 兼容、错误传递和非法配置。

## 运行时包管理

本仓库忽略 skills/，技能内容不随 Git 发布。先备份运行时包，再单独同步修改过的技能文件；不得用源码归档覆盖整套运行时技能。未安装的候选不会展示。
运行时验证：`SKILLS_VALIDATION_DIR=/path/to/skills python3 tests/skillcatalog/test_catalog.py`（依赖 PyYAML）；未提供运行时包时这些文本检查明确跳过，Go 目录行为测试仍执行。
