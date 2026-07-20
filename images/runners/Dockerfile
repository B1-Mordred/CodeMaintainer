# syntax=docker/dockerfile:1
FROM golang:1.25.0-bookworm@sha256:81dc45d05a7444ead8c92a389621fafabc8e40f8fd1a19d7e5df14e61e98bc1a AS worker-build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/maintainer-worker ./cmd/maintainer-worker
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /out/maintainer-worker ./cmd/maintainer-worker

FROM debian:12.13-slim@sha256:2749ca60ffb3c42de053229d7967d292d7dad1067936b38995da0bbfb96c4c23 AS runner-base
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends ca-certificates git jq ripgrep make cmake ninja-build pkg-config \
    && rm -rf /var/lib/apt/lists/*
COPY --from=worker-build --chown=65532:65532 /out/maintainer-worker /usr/local/bin/maintainer-worker
WORKDIR /workspace
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "verification"]

FROM runner-base AS qc-agent
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "qc"]

FROM golang:1.25.0-bookworm@sha256:81dc45d05a7444ead8c92a389621fafabc8e40f8fd1a19d7e5df14e61e98bc1a AS runner-go
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends ca-certificates git jq ripgrep make cmake ninja-build pkg-config \
    && rm -rf /var/lib/apt/lists/*
COPY --from=worker-build --chown=65532:65532 /out/maintainer-worker /usr/local/bin/maintainer-worker
WORKDIR /workspace
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "verification"]

FROM runner-base AS runner-python
USER 0:0
RUN apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends python3 python3-pytest python3-hypothesis python3-flake8 \
    && rm -rf /var/lib/apt/lists/*
USER 65532:65532

FROM node:24.18.0-bookworm@sha256:5711a0d445a1af54af9589066c646df387d1831a608226f4cd694fc59e745059 AS runner-node
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
COPY --from=worker-build --chown=65532:65532 /out/maintainer-worker /usr/local/bin/maintainer-worker
WORKDIR /workspace
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "verification"]

FROM gcc:15.2.0-bookworm@sha256:c74b2d34b775e6a1b14b13b1d41dc7233f62a18f7a6a4e139e0cf59eeab2e070 AS runner-c
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends ca-certificates git cmake ninja-build \
    && rm -rf /var/lib/apt/lists/*
COPY --from=worker-build --chown=65532:65532 /out/maintainer-worker /usr/local/bin/maintainer-worker
WORKDIR /workspace
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "verification"]

FROM runner-c AS runner-cpp

FROM rust:1.92.0-bookworm@sha256:5c38a9e735664861e84ce6918e98ce7f05dbac210230d45381bcf870b2d58083 AS runner-rust
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends ca-certificates git cmake ninja-build \
    && rm -rf /var/lib/apt/lists/*
RUN rustup component add rustfmt clippy
COPY --from=worker-build --chown=65532:65532 /out/maintainer-worker /usr/local/bin/maintainer-worker
WORKDIR /workspace
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "verification"]

FROM runner-rust AS runner-full
USER 0:0
COPY --from=runner-go /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/usr/local/cargo/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
RUN apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends \
      python3 python3-pytest python3-hypothesis python3-flake8 \
      nodejs npm gcc g++ \
    && rm -rf /var/lib/apt/lists/*
USER 65532:65532

FROM runner-full AS implementation-agent
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "implementation"]

FROM runner-full AS dependencies-worker
ENTRYPOINT ["/usr/local/bin/maintainer-worker", "dependencies"]
