FROM node:22.19.0-bookworm-slim AS node
FROM golang:1.26.9-bookworm AS build
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run ./cmd/build-runtime -root runtime/pi \
    && mkdir -p /out \
    && cp -a runtime/pi/.builds/* /out/pi-runtime
RUN CGO_ENABLED=1 go build -mod=readonly -trimpath -o /out/cyberstrike-ai ./cmd/server

FROM node:22.19.0-bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates bash python3 python3-venv curl git \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 cyberstrike
WORKDIR /app
COPY --from=build /out/cyberstrike-ai /usr/local/bin/cyberstrike-ai
COPY --chown=10001:10001 config.example.yaml ./
COPY --chown=10001:10001 web ./web
COPY --chown=10001:10001 agents ./agents
COPY --chown=10001:10001 roles ./roles
COPY --chown=10001:10001 tools ./tools
COPY --chown=10001:10001 knowledge_base ./knowledge_base
COPY --from=build --chown=10001:10001 /out/pi-runtime /opt/cyberstrike/pi
COPY --from=build --chown=10001:10001 /src/runtime/pi/THIRD_PARTY_NOTICES.md /opt/cyberstrike/pi/THIRD_PARTY_NOTICES.md
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
# Container defaults terminate TLS at a trusted edge and use absolute resource
# paths because the editable configuration lives in its own persistent volume.
RUN sed -e 's/\r$//' config.example.yaml \
      -e 's/^  tls_enabled: true/  tls_enabled: false/' \
      -e 's/^  tls_auto_self_sign: true/  tls_auto_self_sign: false/' \
      -e 's|^  tools_dir: tools|  tools_dir: /app/tools|' \
      -e 's|^  base_path: knowledge_base|  base_path: /app/knowledge_base|' \
      -e 's|^skills_dir: skills|skills_dir: /app/skills|' \
      -e 's|^agents_dir: agents|agents_dir: /app/agents|' \
      -e 's|^roles_dir: roles|roles_dir: /app/roles|' \
      -e 's|^  worker_path:.*|  worker_path: /opt/cyberstrike/pi/dist/worker.js|' \
      -e 's|^  node_path:.*|  node_path: /usr/local/bin/node|' \
      > /usr/local/share/cyberstrike-config.yaml \
    && sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh \
    && chmod 755 /usr/local/bin/docker-entrypoint.sh \
    && mkdir -p data log skills chat_uploads tmp runtime \
    && chown -R 10001:10001 /app
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
