# API client identification

Configure the runtime `config.yaml`:

```yaml
api_client:
  user_agent: "CyberStrikeAI"
```

This setting applies to the information collection client's FOFA, ZoomEye, Quake and Shodan HTTP requests, including page queries and Agent queries that reuse this client. It does not configure model SDKs, external MCP, scanning tools, WebShell or C2.

Missing, empty or space-only values default to `CyberStrikeAI`, without the previously hardcoded old version. Custom values accept up to 256 bytes of printable ASCII; surrounding spaces are trimmed before transmission. Control characters, including newlines and tabs, reject configuration loading without echoing the value in errors.

Restart the service after editing the file. There is no Web settings control for this option. Back up configuration before upgrading; existing files need no migration. Restore the previous binary and configuration backup to roll back.
