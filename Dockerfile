# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:26-alpine AS assets

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY assets ./assets
COPY internal/server/templates ./internal/server/templates
COPY tools ./tools
COPY docs/assets/images ./docs/assets/images
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY . .
COPY --from=assets /app/internal/server/static ./internal/server/static

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags="-s -w -X 'main.version=${VERSION}' -X 'main.commit=${COMMIT}' -X 'main.buildTime=${BUILD_TIME}'" \
    -o compass ./cmd/compass

FROM alpine:3.24.1

RUN apk --no-cache add ca-certificates && \
    addgroup -g 1001 -S compass && \
    adduser -u 1001 -S compass -G compass

WORKDIR /app
COPY --from=builder /app/compass .

USER compass

EXPOSE 8080

# compass.yaml is intentionally not baked in — mount your config at /app/compass.yaml.
ENTRYPOINT ["/app/compass"]
