# syntax=docker/dockerfile:1

ARG GO_IMAGE=docker.io/library/golang:1.25-bookworm
ARG RUNTIME_IMAGE=docker.io/library/debian:bookworm-slim

FROM ${GO_IMAGE} AS builder

ARG ONNXRUNTIME_VERSION=1.22.0
ARG ONNXRUNTIME_SHA256=8344d55f93d5bc5021ce342db50f62079daf39aaafb5d311a451846228be49b3
ARG MOBILENET_SHA256=c1c513582d56afceff8516c73804e484c81c6a830712ab6d682253f4a3cd042f
ARG GOPROXY=https://proxy.golang.org,direct
ARG ONNXRUNTIME_URL=
ARG MOBILENET_URL=https://media.githubusercontent.com/media/onnx/models/main/validated/vision/classification/mobilenet/model/mobilenetv2-7.onnx

WORKDIR /src

COPY go.mod go.sum ./
RUN GOPROXY="${GOPROXY}" go mod download

RUN set -eux; \
    mkdir -p /out/models/onnxruntime; \
    runtime_url="${ONNXRUNTIME_URL:-https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz}"; \
    curl --fail --location --retry 3 \
        "${runtime_url}" \
        --output /tmp/onnxruntime.tgz; \
    echo "${ONNXRUNTIME_SHA256}  /tmp/onnxruntime.tgz" | sha256sum --check -; \
    tar -xzf /tmp/onnxruntime.tgz -C /tmp; \
    cp "/tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/libonnxruntime.so.${ONNXRUNTIME_VERSION}" \
        "/out/models/onnxruntime/libonnxruntime.so.${ONNXRUNTIME_VERSION}"; \
    curl --fail --location --retry 3 \
        "${MOBILENET_URL}" \
        --output /out/models/mobilenetv2-7.onnx; \
    echo "${MOBILENET_SHA256}  /out/models/mobilenetv2-7.onnx" | sha256sum --check -

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM ${RUNTIME_IMAGE} AS runtime

ARG ONNXRUNTIME_VERSION=1.22.0

RUN mkdir -p /app/models/onnxruntime /app/data/rag \
    && chown -R 10001:10001 /app

WORKDIR /app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/lib/x86_64-linux-gnu/libstdc++.so.6* /usr/lib/x86_64-linux-gnu/
COPY --from=builder /lib/x86_64-linux-gnu/libgcc_s.so.1 /lib/x86_64-linux-gnu/libgcc_s.so.1

COPY --from=builder --chown=10001:10001 /out/server /app/server
COPY --from=builder --chown=10001:10001 \
    /out/models/onnxruntime/libonnxruntime.so.${ONNXRUNTIME_VERSION} \
    /app/models/onnxruntime/libonnxruntime.so.${ONNXRUNTIME_VERSION}
COPY --from=builder --chown=10001:10001 \
    /out/models/mobilenetv2-7.onnx \
    /app/models/mobilenetv2-7.onnx
COPY --chown=10001:10001 models/imagenet_classes.txt /app/models/imagenet_classes.txt

ENV GIN_MODE=release \
    HTTP_ADDR=0.0.0.0:8080 \
    ONNXRUNTIME_SHARED_LIBRARY_PATH=/app/models/onnxruntime/libonnxruntime.so.${ONNXRUNTIME_VERSION} \
    IMAGE_MODEL_PATH=/app/models/mobilenetv2-7.onnx \
    IMAGE_LABELS_PATH=/app/models/imagenet_classes.txt \
    RAG_STORAGE_ROOT=/app/data/rag

USER 10001:10001

EXPOSE 8080

STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/server"]
