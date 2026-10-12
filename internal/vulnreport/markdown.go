// Package vulnreport formats stored findings without changing their assessments.
package vulnreport

import (
	"cyberstrike-ai/internal/database"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
)

// Inline preserves metadata as literal Markdown text; line breaks cannot create headings or rows.
func Inline(value string) string {
	value = html.EscapeString(strings.Join(strings.Fields(value), " "))
	return strings.NewReplacer("\\", "\\\\", "|", "&#124;", "`", "&#96;", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "#", "\\#").Replace(value)
}

// Fence returns a literal evidence block even when the evidence contains code fences.
func Fence(value string) string {
	longest, run := 0, 0
	for _, char := range value {
		if char == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	delimiter := strings.Repeat("`", max(3, longest+1))
	return delimiter + "text\n" + value + "\n" + delimiter + "\n\n"
}

var severities = map[string]string{"critical": "严重", "high": "高危", "medium": "中危", "low": "低危", "info": "信息", "pending": "待验证"}
var statuses = map[string]string{"open": "待处理", "confirmed": "已确认", "fixed": "已修复", "false_positive": "误报", "accepted": "风险接受", "closed": "已关闭", "retesting": "复测中"}

func label(values map[string]string, value string) string {
	if text, ok := values[value]; ok {
		return text
	}
	return value
}

// Overview reports stored severity counts. Pending is separate from rated findings.
func Overview(items []*database.Vulnerability) string {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Severity]++
	}
	var out strings.Builder
	out.WriteString("| 等级 | 数量 |\n| --- | ---: |\n")
	for _, severity := range []string{"critical", "high", "medium", "low", "info", "pending"} {
		fmt.Fprintf(&out, "| %s | %d |\n", severities[severity], counts[severity])
		delete(counts, severity)
	}
	keys := []string{}
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&out, "| %s | %d |\n", Inline(key), counts[key])
	}
	out.WriteString("\n> 本报告整理已保存记录，不代表重新验证。待验证事项与已定级事项分别统计，潜在影响不视为已证实影响。\n\n")
	return out.String()
}

// Finding renders one finding using a consistent relative heading hierarchy.
// Missing fields remain omitted; stored evidence and severity are never recalculated.
func Finding(v *database.Vulnerability, heading string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s %s\n\n", heading, Inline(v.Title))
	out.WriteString("| 基本信息 | 内容 |\n| --- | --- |\n")
	row := func(name, value string) {
		if value != "" {
			fmt.Fprintf(&out, "| %s | %s |\n", Inline(name), Inline(value))
		}
	}
	row("漏洞 ID", v.ID)
	row("当前等级", label(severities, v.Severity))
	row("处理状态", label(statuses, v.Status))
	row("漏洞类型", v.Type)
	row("目标", v.Target)
	row("对话 ID", v.ConversationID)
	row("对话标签", v.ConversationTag)
	row("任务 ID", v.TaskID)
	row("任务队列 ID", v.TaskQueueID)
	row("任务标签", v.TaskTag)
	if !v.CreatedAt.IsZero() {
		row("创建时间", v.CreatedAt.Format("2006-01-02 15:04:05 -07:00"))
	}
	if !v.UpdatedAt.IsZero() {
		row("更新时间", v.UpdatedAt.Format("2006-01-02 15:04:05 -07:00"))
	}
	out.WriteString("\n")
	section := func(name, text string) {
		if strings.TrimSpace(text) != "" {
			fmt.Fprintf(&out, "%s# %s\n\n%s\n\n", heading, name, text)
		}
	}
	section("漏洞概述", v.Description)
	if v.Assessment == nil {
		section("证据与定级", "历史记录，尚未按当前模型评估。")
	} else {
		data, _ := json.Marshal(v.Assessment)
		fields := map[string]string{}
		_ = json.Unmarshal(data, &fields)
		var table strings.Builder
		table.WriteString("| 评估项目 | 记录内容 |\n| --- | --- |\n")
		for _, key := range assessmentOrder {
			value := fields[key]
			if value == "" {
				continue
			}
			if strings.HasSuffix(key, "severity") {
				value = label(severities, value)
			} else if assessmentEnums[key] {
				value = label(assessmentLabels, value)
			}
			fmt.Fprintf(&table, "| %s | %s |\n", Inline(label(assessmentLabels, key)), Inline(value))
		}
		section("证据与定级", table.String())
	}
	section("前置条件", v.Preconditions)
	section("复现步骤", v.ReproSteps)
	if v.Evidence != "" {
		section("证据 / POC", Fence(v.Evidence))
	}
	section("记录的影响", v.Impact)
	section("修复建议", v.Recommendation)
	section("复测记录与方法", v.RetestNotes)
	out.WriteString("---\n\n")
	return out.String()
}

var assessmentOrder = []string{"version", "evidence_status", "evidence_basis", "boundary_status", "observed_impact", "verification_details", "scope_evidence", "high_impact_evidence", "impact_level", "scope", "asset_value", "access", "interaction", "rationale", "preconditions", "potential_impact", "proposed_severity", "suggested_severity", "adjustment", "override_reason"}
var assessmentEnums = map[string]bool{"evidence_status": true, "evidence_basis": true, "boundary_status": true, "impact_level": true, "scope": true, "asset_value": true, "access": true, "interaction": true}
var assessmentLabels = map[string]string{"evidence_basis": "证据来源类型", "boundary_status": "权限或安全边界核对", "observed_impact": "已观察的实际影响", "verification_details": "验证依据与前后对照", "scope_evidence": "已证明影响范围", "high_impact_evidence": "重大影响的专项证据", "static": "仅静态分析", "runtime": "已记录运行证据", "expected": "符合预期访问", "violated": "已证明边界被突破", "title": "证据与定级", "pending": "待定级", "legacy": "历史记录：尚未经过新模型评估", "evidence_status": "证据状态", "impact_level": "已证实影响", "scope": "已证实范围", "asset_value": "资产价值", "access": "利用权限门槛", "interaction": "用户交互", "rationale": "定级依据", "preconditions": "前置条件（无条件也须明确填写）", "potential_impact": "未验证的潜在影响（不计入定级）", "override_reason": "人工调整理由（留空采用建议等级）", "unknown": "未知", "unverified": "未验证", "partial": "部分验证", "verified": "报告人已验证证据", "none": "无", "limited": "有限信息或轻微影响", "moderate": "局部越权或有限权限", "major": "重大数据或账号控制影响", "critical": "核心资产／广泛重大影响", "single": "单条记录／单个用户／一次交互", "multiple": "多个对象／多次交互", "system": "整个系统", "public": "公开", "internal": "内部", "sensitive": "敏感", "ordinary": "普通权限（含普通登录账号）", "restricted": "特殊资质或高权限", "suggested_severity": "建议等级", "proposed_severity": "提交等级", "adjustment": "规则说明", "version": "规则版本"}
