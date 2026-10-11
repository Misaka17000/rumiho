FROM metacubex/mihomo:v1.19.17@sha256:1c05d1ea17927e7c2c5faa2e9eb4eb965567e3b93c66cb68f6491e6a85e643e4 AS core

FROM node:24-alpine AS frontend
WORKDIR /src
RUN npm install --global pnpm@11.20.0
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY frontend ./
RUN pnpm type-check && pnpm build

FROM golang:1.25-alpine AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /rumiho ./cmd/rumiho

FROM alpine:3.22
RUN apk add --no-cache su-exec && addgroup -S -g 10001 rumiho && adduser -S -D -H -u 10001 -G rumiho rumiho && mkdir -p /config /app && chown rumiho:rumiho /config
COPY --from=core /mihomo /usr/local/bin/mihomo
COPY --from=core /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /rumiho /usr/local/bin/rumiho
COPY --from=frontend /src/dist /app/dashboard
COPY frontend/LICENSE /app/dashboard/LICENSE
COPY third_party/rules /app/rules
COPY third_party/MIHOMO_LICENSE third_party/NOTICE.md /app/licenses/
COPY third_party/assets.lock.json /app/licenses/assets.lock.json
COPY --chmod=755 scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
WORKDIR /app
ENV CONFIG_FILE=/config/config.yaml DATA_DIR=/config/cache LISTEN_ADDR=:8080
EXPOSE 8080
VOLUME ["/config"]
HEALTHCHECK --interval=30s --timeout=8s --start-period=15s --retries=3 CMD ["/usr/local/bin/rumiho", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
