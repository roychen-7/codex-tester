.DEFAULT_GOAL := help
.PHONY: help install dev dev-local down logs check-docker dev-api dev-web build build-api build-web start-api check check-api check-web typecheck test

help:
	@echo "make dev | down | logs | check-docker | install | dev-local | dev-api | dev-web | build | check | test | typecheck | start-api"

install:
	pnpm --dir apps/web install --frozen-lockfile
	go -C apps/api mod download

dev:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f

check-docker:
	docker compose build
	docker compose run --rm --no-deps api sh -c 'go vet ./... && go test -race ./... && go build -o /tmp/api-check ./cmd/server'
	docker compose run --rm --no-deps web pnpm check

dev-local:
	$(MAKE) -j2 dev-api dev-web

dev-api: build-api
	./apps/api/bin/api

dev-web:
	pnpm --dir apps/web dev

build: build-api build-web

build-api:
	go -C apps/api build -o bin/api ./cmd/server

build-web:
	pnpm --dir apps/web build

start-api:
	./apps/api/bin/api

check: check-api check-web

check-api:
	go -C apps/api vet ./...
	go -C apps/api test -race ./...
	$(MAKE) build-api

check-web:
	pnpm --dir apps/web check

typecheck:
	pnpm --dir apps/web typecheck

test:
	go -C apps/api test -race ./...
