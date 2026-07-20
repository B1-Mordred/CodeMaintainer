.PHONY: bootstrap format test race lint build build-runners compose-check up down

bootstrap:
	./scripts/bootstrap.sh

format:
	docker compose run --rm go-tool gofmt -w cmd internal

test:
	docker compose run --rm go-tool go test ./...
	docker compose run --rm web-tool sh -c 'npm ci && npm test'

race:
	docker compose run --rm go-tool go test -race ./...

lint:
	docker compose run --rm go-tool go vet ./...
	docker compose run --rm web-tool npx redocly lint ../internal/api/openapi.yaml
	docker compose run --rm web-tool npm run check:api

build:
	docker compose build controller maintainctl runnerd
	docker compose run --rm web-tool sh -c 'npm ci && npm run build'

build-runners:
	docker build -f images/runners/Dockerfile --target runner-base -t local/code-maintainer-runner-base:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-python -t local/code-maintainer-runner-python:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-node -t local/code-maintainer-runner-node:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-c -t local/code-maintainer-runner-c:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-cpp -t local/code-maintainer-runner-cpp:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-rust -t local/code-maintainer-runner-rust:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-go -t local/code-maintainer-runner-go:0.1.0-dev .
	docker build -f images/runners/Dockerfile --target runner-full -t local/code-maintainer-runner-full:0.1.0-dev .

compose-check:
	docker compose -f compose.yaml -f compose.dev.yaml config --quiet

up:
	./maintainctl up

down:
	./maintainctl down
