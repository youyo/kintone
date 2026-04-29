# syntax=docker/dockerfile:1.7

# ============================================================================
# Builder stage
# ============================================================================
FROM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

WORKDIR /src

# 依存キャッシュ最適化のため go.mod / go.sum を先にコピー
COPY go.mod go.sum ./
RUN go mod download

# ソースをコピーしてビルド
COPY . .

# CGO 不要（modernc.org/sqlite は pure Go）
ENV CGO_ENABLED=0

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
        -trimpath \
        -ldflags "-s -w -X github.com/youyo/kintone/internal/cli.Version=${VERSION}" \
        -o /out/kintone \
        ./cmd/kintone

# ============================================================================
# Runtime stage（distroless static / nonroot）
# ============================================================================
FROM gcr.io/distroless/static-debian12:nonroot

# /home/nonroot は distroless:nonroot で作成済（uid:gid = 65532:65532）
COPY --from=builder /out/kintone /usr/local/bin/kintone

USER 65532:65532
WORKDIR /home/nonroot

ENTRYPOINT ["/usr/local/bin/kintone"]
CMD ["version"]
