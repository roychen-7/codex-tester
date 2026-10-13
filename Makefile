.DEFAULT_GOAL := help
.PHONY: init-env migrate create-user test-db format check-format check-e2e

format:
	go -C apps/api tool gofumpt -w cmd internal
	pnpm --dir apps/web format

check-format:
	@files="$$(go -C apps/api tool gofumpt -l cmd internal)" || exit 1; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files" 'Go 格式不符合要求，请运行 make format'; exit 1; fi

check-e2e:
	pnpm --dir apps/web test:e2e

init-env:
	python3 scripts/init-env.py

migrate:
	docker compose run --rm --no-deps api go run ./cmd/manage migrate

create-user:
	@test -n "$(USERNAME)" || (echo '用法：make create-user USERNAME=roy（密码由终端隐藏输入）'; exit 1)
	@python3 scripts/create-user.py "$(USERNAME)" "$(or $(DISPLAY_NAME),$(USERNAME))"

test-db:
	docker compose --profile test up -d --wait mysql-test
.PHONY: install-hooks check-git-flow

check-git-flow:
	sh scripts/test-git-flow.sh

install-hooks:
	git config --local core.hooksPath .githooks
.PHONY: help install dev dev-local down logs check-docker dev-api dev-web build build-api build-web start-api check check-api check-web typecheck test

help:
	@echo "make init-env | dev | down | logs | create-user USERNAME=... | migrate | test-db | check | check-docker | check-e2e | format"

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
	$(MAKE) test-db
	docker compose run --rm --no-deps -e TEST_MYSQL_DSN='agent:agent-test-only@tcp(mysql-test:3306)/agent_test' api sh -c 'files="$$(go tool gofumpt -l cmd internal)" && { [ -z "$$files" ] || { printf "%s\n" "$$files" "请运行 make format"; exit 1; }; } && go vet ./... && go test -race ./... && go build -o /tmp/api-check ./cmd/server && go build -o /tmp/manage-check ./cmd/manage'
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
	go -C apps/api build -o bin/manage ./cmd/manage

build-web:
	pnpm --dir apps/web build

start-api:
	./apps/api/bin/api

check: check-format check-git-flow check-api check-web

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
