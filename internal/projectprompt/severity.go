// Package-level evidence guidance is shared by agent prompts and MCP metadata.
package projectprompt

// VulnerabilitySeverityGuidance defines evidence-based reporting guidance; it does not enforce or rewrite stored ratings.
const VulnerabilitySeverityGuidance = `### 漏洞定级与证据约束

- 严重程度由已证实的影响、可利用条件、受影响范围及项目明确的定级标准共同决定，禁止按漏洞名称、扫描器标签、报告数量或追求高危的目标直接定级。安全验证必须保持在授权范围内，不得为证明高危进行破坏性利用。
- 在 impact 中分别写明「已证实影响」「定级依据」「前置条件与范围」「未验证的潜在影响」。区分证据置信度与严重程度；无法确认的发现标注疑似/待验证并记录为项目事实，不把未知影响写成已证实，也不把证据不足解释为系统安全。
- high / critical 必须说明支持重大影响的具体证据和必要前提；允许使用充分的非破坏性证据或源码依据，不要求实际窃取数据、接管账号或执行破坏操作。仅有可能钓鱼、可能撞库、可能串联其他漏洞，不足以证明高危。攻击链各环节未经证实，不得把整条假设链的最高危害赋给单个问题。
- 账号枚举：仅区分账号存在与否、无敏感身份关联或其他已证实影响时，本项目默认 low；公开且预期披露的信息可能仅为 info 或不构成漏洞。若涉及敏感身份关联、额外敏感信息或已证实的独立攻击链，依据新增证据和项目规则重新评估，不能仅凭“可能导致账号接管”升为 high。
- CORS：存在宽松响应头不等于高危。公开非敏感资源的预期跨域访问通常不构成漏洞；仅有配置迹象时记录待验证。需要说明不可信来源在真实浏览器中能否读取本不应访问的响应、数据敏感性、认证/凭据条件、Cookie/SameSite 限制和用户交互要求。不能把 curl 看到响应或 OPTIONS 成功视作浏览器跨域读取成功；Access-Control-Allow-Origin: * 与携带凭据的跨域读取不兼容。只有证据支持相应业务影响时才升级定级，不将所有 CORS 问题固定为 low 或 high。
- info / low / medium / high / critical 不是漏洞类型映射表。需要 CVSS 时注明版本、完整向量和各指标依据；缺少依据不得编造精确分数。项目/SRC 定级规则与通用评分不同，须写明采用的规则及差异。
`
