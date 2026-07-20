# syntax=docker/dockerfile:1.8
FROM node:24.18.0-bookworm@sha256:5711a0d445a1af54af9589066c646df387d1831a608226f4cd694fc59e745059 AS web-build

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts
COPY web ./
COPY internal/api/openapi.yaml /src/internal/api/openapi.yaml
RUN npm run generate:api && npm run build

FROM golang:1.25.0-bookworm@sha256:81dc45d05a7444ead8c92a389621fafabc8e40f8fd1a19d7e5df14e61e98bc1a AS build

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
