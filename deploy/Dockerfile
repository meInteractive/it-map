# syntax=docker/dockerfile:1
# Build from the repository root. Secrets are supplied only at runtime.
ARG NODE_IMAGE=node:24-bookworm-slim
ARG GO_IMAGE=golang:1.27.3-bookworm
FROM ${NODE_IMAGE} AS web
WORKDIR /src
COPY package.json package-lock.json ./
# Optional CA bundle for a managed build proxy; never retained in a layer.
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/proxy_ca; fi; \
    npm ci --no-audit --no-fund
COPY vite.web.config.ts ./
COPY web/ web/
COPY frontend/ frontend/
COPY shared/ shared/
COPY src/ src/
COPY public/ public/
RUN npm run build:web

FROM ${GO_IMAGE} AS backend
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then export SSL_CERT_FILE=/run/secrets/proxy_ca; fi; \
    go mod download && go mod verify
COPY cmd/ cmd/
COPY internal/ internal/
RUN go build -mod=readonly -trimpath -tags mariadb -o /out/itmap ./cmd/itmap

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /out/itmap /app/itmap
COPY --from=web /src/dist/web /app/web
USER 65532:65532
WORKDIR /app
ENV WEB_DIST_DIR=/app/web HTTP_ADDR=127.0.0.1:8080
ENTRYPOINT ["/app/itmap"]
