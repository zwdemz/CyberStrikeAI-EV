FROM golang:1.26.8-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -mod=readonly -trimpath -o /out/cyberstrike-ai ./cmd/server

FROM debian:bookworm-slim
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
RUN mkdir -p data log skills chat_uploads \
    && chown -R 10001:10001 /app
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["cyberstrike-ai"]
CMD ["--config", "/app/config.yaml"]
