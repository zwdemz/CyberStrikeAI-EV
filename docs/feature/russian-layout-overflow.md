# Russian layout overflow

## F0 Intake / workflow and maintainability gates

User reported Russian overflow with a dashboard screenshot. Clean main checkout; dedicated branch codex/russian-layout-overflow.
Workflow: P10 feedback → P7 narrow fix → P9 visual QA. Existing template, translations and screenshot supply upstream UI contract; no missing product/API decisions. Implementation allowed.
Scope: dashboard severity/status/risk presentation and shared sidebar labels. Acceptance: complete readable labels without overlapping adjacent cells at desktop and narrow widths, with existing themes/navigation preserved.
Maintainability: style.css 47,163 lines and index.html are existing large composition files. High size risk; CSS presentation only, no new responsibility. Allowed narrow_fix; no broad refactor required. Validate actual template/styles/translations in Chromium, plus diff check. Assumption: wrapping is preferable to truncating operational status labels; no translation changes.
Lifecycle: F0–F6 documentation here, scoped commits pushed per phase; Unreleased changelog, no merge/release/deploy requested.

## F1 Requirements

Small corrective scope inferred from explicit bug report; no unresolved product decision or separate draft requirements gate needed.
REQ/AC-001: Russian status labels stay inside their cards with no overlap, including false-positive labels.
REQ/AC-002: Risk badge, severity names and navigation labels remain fully readable by wrapping within available space.
REQ/AC-003: Chinese/English, light/dark, desktop/tablet/mobile and collapsed sidebar retain usable layout and click targets. Data/loading/error rendering and API contracts unchanged.

## F2 Design

Use existing shared styles with language-independent wrapping, bounded flex children and auto-fit grid tracks. Status panel uses container width to decide whether progress fits alongside cards; avoid viewport-only decisions when dashboard sits beside a right column. Keep existing colors, semantics, event handlers and translations. Sidebar labels wrap with stable nonshrinking icons; collapsed rules continue hiding labels.

## F3 Implementation Plan

Touch style.css existing navigation/dashboard rules and index.html stylesheet cache key. Constrain and wrap risk/urgent/status/severity/batch labels; make status grid auto-fit and progress responsive to actual panel width. Verify real extracted dashboard/sidebar template with actual i18n dictionaries in headless Chromium at 320/390/768/1024/1440/1920/2938px, light/dark and all three languages; inspect screenshots. Check overlap and label containment, collapsed/sidebar states, nonzero large values and diff whitespace. Record evidence and Unreleased fix. Rollback via reverting scoped implementation commit; no backend changes.

## F4 Implementation

Shared navigation labels now wrap instead of ellipsis. Risk header wraps badge/label, urgent and batch labels are bounded and wrap. Severity legend gives remaining space to complete label text and intrinsic space to numeric columns. Status cards auto-fit with a 110px preferred minimum and bounded multiline labels; progress stacks until chart container has at least 1000px. Container queries at 760/480px adapt risk/chart/legend to the real panel width. Header actions wrap; main grid no longer expands from min-content. Updated stylesheet cache key.
Validation before code commit: headless Chrome 42 language/theme/width scenarios passed for measured label containment, no overlapping status cards and main card element boundaries; existing mobile i18n 8/8 tests and diff check passed.

## F5 Verification

- Chrome/Playwright temporary harness `/tmp/csai-layout-qa.cjs`: actual index.html, style.css/mobile.css, bundled i18next, actual i18n.js changeLanguage/applyTranslations and all three dictionaries. Network/backend replaced with local resource fixtures; no live authentication or backend requests.
- 42 scenarios: zh-CN/en-US/ru-RU × light/dark × 320/390/768/1024/1440/1920/2938 CSS pixels. PASS: target labels stay within their parents, no horizontal text overflow, status cards do not overlap, risk/legend/status/header elements stay within chart card. Dark scenarios use 99,999 counts and critical risk text; light scenarios use zero counts. Real card decorative glow intentionally extends beyond chart bounds; not a label overflow.
- Additional PASS: collapsed desktop sidebar hides labels; mobile expanded sidebar labels remain contained at 320/390px; false-positive cell click and Enter preserve inline filter dispatch (navigation stub, no live API).
- `node --test web/static/js/mobile-ui-i18n.test.cjs`: 8/8 PASS. `git diff --check`: PASS.
- Screenshots/results at `/tmp/csai-layout-proof/`; manually inspected Russian desktop light and mobile dark. Screenshot fixture omits backend-driven donut SVG and dynamic status/theme labels, which were not modified.
- Scope limitation: focused dashboard/shared navigation visual checks, not a full application page audit or live-server end-to-end test. No backend code changed. Implementation commit 132babe3 pushed; preceding phase commits e82ee59f/c356e514/3889de46/f6f93a1b pushed.

## F6 Review / Release Record

Scoped review complete: CSS presentation and cache version only, existing data/translation keys/navigation handlers preserved. Unreleased changelog added. Documentation, implementation and verification committed and pushed on codex/russian-layout-overflow. No merge, version release or deployment performed; ready for merge review. Recommended next step: merge this branch, then smoke the running instance in Russian. No outstanding implementation gap in the requested dashboard/navigation slice.

## F8 Merge Summary — 2026-10-10

用户授权提交 main。合并前工作区干净，fetch 后 main 与 origin/main 均为 8ce62813，功能分支无远端主线冲突，diff check 通过。main 以 fast-forward 合并至 ab04c864，并成功推送 origin/main；实现提交 132babe3 与 F5 验证记录均已包含。合并没有改动已验证的实现，无需重复布局矩阵。此节替代 F6 的未合并状态；未发布版本或部署服务。
