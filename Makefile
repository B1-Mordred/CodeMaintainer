.PHONY: bootstrap format test race lint build compose-check up down

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

build:
	docker compose build controller maintainctl
	docker compose run --rm web-tool sh -c 'npm ci && npm run build'

compose-check:
	docker compose -f compose.yaml -f compose.dev.yaml config --quiet

up:
	./maintainctl up

down:
	./maintainctl down
