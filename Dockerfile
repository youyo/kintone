# syntax=docker/dockerfile:1.7
#
# GoReleaser 用 Dockerfile。
# GoReleaser の dockers セクションは事前にビルドされたバイナリ (kintone) のみを
# build context に渡すため、ここでは Go ビルドを行わずバイナリをそのまま COPY する。
#
# ローカルで `docker build` する場合は、別途 multi-stage Dockerfile (Dockerfile.local) を
# 用意するか `goreleaser release --snapshot` 経由で artifact を生成すること。

FROM gcr.io/distroless/static-debian12:nonroot

COPY kintone /usr/local/bin/kintone

USER 65532:65532
WORKDIR /home/nonroot

ENTRYPOINT ["/usr/local/bin/kintone"]
CMD ["version"]
