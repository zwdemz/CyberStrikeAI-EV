---
name: unauthorized-access-common-services
description: "Common exposed service and unauthorized access triage. Use when checking exposed admin panels, databases, caches, message queues, search services, cloud consoles, dev tools, reverse proxy mistakes, Nginx off-by-slash, X-Forwarded-For trust, Caddy template exposure, RMI/T3/AJP, Redis, Elasticsearch, MongoDB, RabbitMQ, Jenkins, Grafana, Kibana, or similar service exposure in authorized testing."
---

# Unauthorized Access Common Services

## Core Rule

Use only for authorized exposure triage. Prefer version, banner, login requirement, and read-only proof. Do not change configuration, write data, create users, dump datasets, or execute commands unless explicitly authorized for that exact target.

## First-Pass Triage

1. Identify exposed service type, host, port, scheme, and whether the endpoint is internet-facing or internal.
2. Check unauthenticated reachability with read-only requests: status page, version endpoint, health endpoint, login redirect, or denied response.
3. Record evidence without sensitive data: title, status code, product header, version string, auth requirement, and sample non-sensitive marker.
4. For reverse proxies, inspect path normalization, trailing slash behavior, forwarded headers, and upstream leakage.
5. For databases/caches/search services, stop at confirming unauthenticated access or metadata exposure unless the user explicitly authorizes deeper validation.

## Common Buckets

- Dev/admin panels: Jenkins, Grafana, Kibana, Spring Actuator, Swagger/OpenAPI, phpMyAdmin.
- Data services: Redis, MongoDB, Elasticsearch, CouchDB, RabbitMQ, etcd.
- Java/middleware: RMI Registry, WebLogic T3, AJP, JMX.
- Proxy mistakes: Nginx alias/off-by-slash, `X-Forwarded-For` trust, Caddy template exposure, internal host routing.

## Route To Related Skills

- Use `ssrf-server-side-request-forgery` if the service is only reachable through SSRF.
- Use `insecure-source-code-management` for `.git`, `.svn`, `.env`, backup, and source exposure.
- Use `deserialization-insecure` for exposed Java serialization/RMI/T3 style surfaces.
- Use `burp-mcp-vuln-check` to preserve HTTP evidence and create repeatable proof.
