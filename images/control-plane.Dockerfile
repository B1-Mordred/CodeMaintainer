# syntax=docker/dockerfile:1.8
FROM node:24.18.0-bookworm@sha256:5711a0d445a1af54af9589066c646df387d1831a608226f4cd694fc59e745059 AS web-build

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts
COPY web ./
COPY internal/api/openapi.yaml /src/internal/api/openapi.yaml
RUN npm run generate:api && npm run build

FROM golang:1.25.12-bookworm@sha256:ea341baa9bd5ba6784f6d7161ace70544349a6242d54d34a0fbfd2c4d51c9d58 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web-build /src/internal/ui/dist ./internal/ui/dist
ARG VERSION=0.1.0-dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/controller ./cmd/controller && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/maintainctl ./cmd/maintainctl && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/runnerd ./cmd/runnerd
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="-s -w" -o /out/fake-model-server ./cmd/fake-model-server
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="-s -w" -o /out/git-bridge ./cmd/git-bridge && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="-s -w" -o /out/hermes-tool-bridge ./cmd/hermes-tool-bridge

FROM gcr.io/distroless/static-debian12@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b AS controller
COPY --from=build --chown=65532:65532 /out/controller /controller
COPY --from=build --chown=65532:65532 /out/maintainctl /maintainctl
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/controller"]

FROM gcr.io/distroless/static-debian12@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b AS maintainctl
COPY --from=build --chown=65532:65532 /out/maintainctl /maintainctl
USER 65532:65532
ENTRYPOINT ["/maintainctl"]

FROM gcr.io/distroless/static-debian12@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b AS runnerd
COPY --from=build --chown=65532:65532 /out/runnerd /runnerd
USER 65532:65532
ENTRYPOINT ["/runnerd"]

FROM gcr.io/distroless/static-debian12@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b AS fake-model-server
COPY --from=build --chown=65532:65532 /out/fake-model-server /fake-model-server
USER 65532:65532
EXPOSE 8082
ENTRYPOINT ["/fake-model-server"]

FROM debian:12.13-slim@sha256:2749ca60ffb3c42de053229d7967d292d7dad1067936b38995da0bbfb96c4c23 AS git-bridge
ARG DEBIAN_SNAPSHOT=20260505T000000Z
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/%s bookworm main\n' "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build --chown=65532:65532 /out/git-bridge /git-bridge
USER 65532:65532
EXPOSE 8083
ENTRYPOINT ["/git-bridge"]

FROM gcr.io/distroless/static-debian12@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b AS hermes-tool-bridge
COPY --from=build --chown=65532:65532 /out/hermes-tool-bridge /hermes-tool-bridge
USER 65532:65532
EXPOSE 8085
ENTRYPOINT ["/hermes-tool-bridge"]
