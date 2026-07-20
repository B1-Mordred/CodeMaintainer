# syntax=docker/dockerfile:1.8
ARG DEBIAN_DIGEST=sha256:2749ca60ffb3c42de053229d7967d292d7dad1067936b38995da0bbfb96c4c23

FROM debian:12.13-slim@${DEBIAN_DIGEST} AS llama-source
ARG DEBIAN_SNAPSHOT=20260505T000000Z
ARG LLAMA_CPP_COMMIT=aedb2a5e9ca3d4064148bbb919e0ddc0c1b70ab3
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "$DEBIAN_SNAPSHOT" > /etc/apt/sources.list && \
    rm -f /etc/apt/sources.list.d/* && \
    apt-get -o Acquire::Check-Valid-Until=false update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates git cmake ninja-build g++ && \
    rm -rf /var/lib/apt/lists/*
RUN git clone --filter=blob:none https://github.com/ggml-org/llama.cpp.git /src/llama.cpp && \
    git -C /src/llama.cpp checkout --detach "$LLAMA_CPP_COMMIT" && \
    test "$(git -C /src/llama.cpp rev-parse HEAD)" = "$LLAMA_CPP_COMMIT"

FROM llama-source AS llama-haswell
RUN cmake -S /src/llama.cpp -B /src/llama.cpp/build -G Ninja \
      -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
      -DGGML_NATIVE=OFF -DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON -DGGML_BMI2=ON \
      -DLLAMA_CURL=OFF -DLLAMA_BUILD_UI=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=ON && \
    cmake --build /src/llama.cpp/build --target llama-server --parallel 4

FROM golang:1.25.0-bookworm@sha256:81dc45d05a7444ead8c92a389621fafabc8e40f8fd1a19d7e5df14e61e98bc1a AS supervisor-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/model-supervisor ./cmd/model-supervisor
COPY cmd/model-tool ./cmd/model-tool
COPY internal ./internal
ARG VERSION=0.1.0-dev
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" -o /out/model-supervisor ./cmd/model-supervisor && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/model-tool ./cmd/model-tool

FROM debian:12.13-slim@${DEBIAN_DIGEST} AS inference-runtime
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "$DEBIAN_SNAPSHOT" > /etc/apt/sources.list && \
    rm -f /etc/apt/sources.list.d/* && \
    apt-get -o Acquire::Check-Valid-Until=false update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libgomp1 && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /models /etc/maintainer/models /run/secrets && chown -R 65532:65532 /models /run/secrets
COPY --from=supervisor-build --chown=65532:65532 /out/model-supervisor /usr/local/bin/model-supervisor
COPY --from=supervisor-build --chown=65532:65532 /out/model-tool /usr/local/bin/model-tool
USER 65532:65532
EXPOSE 8081 8082
ENTRYPOINT ["/usr/local/bin/model-supervisor"]

FROM inference-runtime AS inference-haswell
COPY --from=llama-haswell --chown=65532:65532 /src/llama.cpp/build/bin/llama-server /usr/local/bin/llama-server

FROM llama-source AS llama-native
RUN cmake -S /src/llama.cpp -B /src/llama.cpp/build-native -G Ninja \
      -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_NATIVE=ON \
      -DLLAMA_CURL=OFF -DLLAMA_BUILD_UI=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=ON && \
    cmake --build /src/llama.cpp/build-native --target llama-server --parallel 4

FROM inference-runtime AS inference-native
COPY --from=llama-native --chown=65532:65532 /src/llama.cpp/build-native/bin/llama-server /usr/local/bin/llama-server
