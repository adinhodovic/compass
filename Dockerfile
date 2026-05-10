FROM node:24-alpine AS assets

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY assets ./assets
COPY internal/server/templates ./internal/server/templates
COPY tools ./tools
COPY docs/assets/images ./docs/assets/images
RUN npm run build

FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=assets /app/internal/server/static ./internal/server/static

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o compass ./cmd/compass

FROM alpine:3.22

RUN apk --no-cache add ca-certificates && \
    addgroup -g 1001 -S compass && \
    adduser -u 1001 -S compass -G compass

WORKDIR /app
COPY --from=builder /app/compass .

USER compass

EXPOSE 8080

# compass.yaml is intentionally not baked in — mount your config at /app/compass.yaml.
ENTRYPOINT ["/app/compass"]
