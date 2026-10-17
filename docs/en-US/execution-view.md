# AI execution view

[中文](../zh-CN/execution-view.md)

Open **AI execution** in the sidebar, search for an accessible conversation, then select an assistant turn. The overview shows persisted turn status, iteration count, tool count, tool issues and recorded duration. Stage indicators mean records exist, not a completion percentage or an inferred execution plan.

The timeline shows model activity, tool actions and coordination/approval events. A tool's result may be outside the selected page: its status is reconciled against the persisted tool execution summary. Unknown and missing results remain explicit. The view never executes tools or changes approval decisions.

## History and refresh

- Conversations are searched in pages of 25. The turn selector reads 20 messages at a time; **Earlier turns** loads the next older batch. **Latest turns** returns to its newest turns.
- Event pages contain at most 50 records. Filters apply to the current event page; overview counts describe the selected turn.
- An active turn refreshes every ten seconds. Turn off automatic refresh to inspect a static snapshot. Completed turns stop polling; use Refresh to read again.
- Reads are serialized and canceled on conversation changes, navigation and hidden tabs. Refresh waits for the preceding read and backs off after errors. Three consecutive failed reads pause automatic refresh until an explicit Refresh.
- Transient failures retain the last successful snapshot and show a failure notice. Authentication, permission or unavailable-resource responses clear displayed execution records.
- `#execution-view?conversation=<id>` opens a conversation using the same access checks as chat. API errors do not expose raw upstream messages in this page.

## Access and data boundaries

The page requires `chat:read`. Conversation lists and message/process-detail reads use the existing authenticated conversation APIs and resource-scope checks. The page introduces no alternate privileged endpoint or database.

Only event metadata is rendered: event class, tool/agent name, time, recorded iteration and persisted status. The page does not display reasoning text, tool arguments, raw outputs or arbitrary event messages. Use the original conversation for detailed evidence. Old conversations without process records display an empty state rather than invented steps. No model/provider requests are made by opening this view.

Static frontend code is shared by Windows and Linux. No native command, process-isolation or service-startup behavior changes. Deploy matching HTML, JavaScript, CSS and locale resources after active work finishes; roll back those resources together if needed. No schema migration or new runtime configuration is required.

## Validation

```sh
node --test tests/web/*.test.cjs tests/web/*.test.mjs web/static/js/*.test.cjs
node tests/browser/execution-view.cjs
```

The browser fixture needs Playwright and an installed Chromium browser. `EXECUTION_BROWSER_CHANNEL=chrome` optionally uses an installed Chrome for local validation. Set `EXECUTION_VIEW_SCREENSHOTS` to capture fixtures. CI runs the default bundled Chromium on Linux. Tests use synthetic records and no external targets or providers.

![Dark theme with synthetic execution metadata](../../images/execution-view-dark.png)
