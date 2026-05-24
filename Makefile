# Makefile – local dev and CI convenience targets for wow-dashboard-api
#
# Usage:
#   make check       # run all verification steps (CI uses this)
#   make dev         # start Air live-reload server
#   make fmt         # auto-format Go source files
#   make openapi     # regenerate openapi/openapi.json
#   make openapi-types # regenerate frontend TypeScript types from OpenAPI
#   make sqlc        # regenerate sqlc models and queries
#   make migrate-up  # run goose migrations up (requires local DATABASE_URL)
#   make migrate-down # run goose migrations down (requires local DATABASE_URL)
#   make seed        # seed local demo auth user (requires local DATABASE_URL)
#   make local-setup # start local Postgres, run migrations, and seed demo auth user
#   make postman-test # run Newman black-box smoke tests against a running API
#   make smoke-local # prepare local deps, run API, run Newman, stop API

.PHONY: fmt fmt-check test test-race test-integration vet openapi openapi-check openapi-types openapi-types-check sqlc sqlc-check migrate-up migrate-down seed compose-up compose-down wait-db local-setup local-reset smoke-auth postman-test smoke-local check dev docker-build docker-run

LOCAL_DATABASE_URL ?= postgres://wow_dashboard:wow_dashboard@localhost:5432/wow_dashboard_api?sslmode=disable
SMOKE_AUTH_BASE_URL ?= http://localhost:7272
BASE_URL ?= $(SMOKE_AUTH_BASE_URL)
SMOKE_AUTH_EMAIL ?= demo@wow-dashboard.test
SMOKE_AUTH_PASSWORD ?= @Password
POSTMAN_BASE_URL ?= $(BASE_URL)
POSTMAN_EMAIL ?= $(SMOKE_AUTH_EMAIL)
POSTMAN_PASSWORD ?= $(SMOKE_AUTH_PASSWORD)
POSTMAN_COLLECTION ?= postman/wow-dashboard-api.postman_collection.json
POSTMAN_ENV ?= postman/env.local.json
NEWMAN ?= npx --yes newman
OPENAPI_TYPESCRIPT_PACKAGE ?= openapi-typescript@7.13.0
OPENAPI_TYPESCRIPT ?= $(shell if command -v bunx >/dev/null 2>&1; then printf 'bunx --bun $(OPENAPI_TYPESCRIPT_PACKAGE)'; else printf 'npx --yes $(OPENAPI_TYPESCRIPT_PACKAGE)'; fi)
OPENAPI_TYPESCRIPT_OUT ?= openapi/typescript/schema.ts

# ---------- formatting ----------

fmt:
	gofmt -w .

fmt-check:
	@echo "==> Checking gofmt…"
	@test -z "$$(gofmt -l .)" || { echo "gofmt found unformatted files:"; gofmt -l .; exit 1; }

# ---------- code generation & migrations ----------

sqlc:
	go tool sqlc generate

sqlc-check: sqlc
	@echo "==> Checking for uncommitted SQLC codegen drift…"
	@git diff --exit-code -- internal/store/query
	@test -z "$$(git status --porcelain -- internal/store/query | grep -E '^.[^ ]' || true)" || { echo "SQLC generated files are untracked or out of date:"; git status --short -- internal/store/query; exit 1; }

migrate-up:
	@if [ -z "$$DATABASE_URL" ]; then echo "DATABASE_URL is not set"; exit 1; fi
	go tool goose -dir migrations postgres "$$DATABASE_URL" up

migrate-down:
	@if [ -z "$$DATABASE_URL" ]; then echo "DATABASE_URL is not set"; exit 1; fi
	go tool goose -dir migrations postgres "$$DATABASE_URL" down

seed:
	@if [ -z "$$DATABASE_URL" ]; then echo "DATABASE_URL is not set"; exit 1; fi
	go run ./cmd/seed

# ---------- local development harness ----------

compose-up:
	docker compose up -d postgres

compose-down:
	docker compose down

wait-db:
	@echo "==> Waiting for local PostgreSQL to accept connections…"
	@for i in $$(seq 1 30); do \
		if docker compose exec -T postgres pg_isready -U wow_dashboard -d wow_dashboard_api >/dev/null 2>&1; then \
			echo "==> PostgreSQL is ready."; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "PostgreSQL did not become ready in time."; \
	docker compose logs postgres; \
	exit 1

local-setup: compose-up wait-db
	$(MAKE) migrate-up DATABASE_URL="$(LOCAL_DATABASE_URL)"
	$(MAKE) seed DATABASE_URL="$(LOCAL_DATABASE_URL)"

local-reset:
	@echo "WARNING: local-reset deletes the local PostgreSQL Docker volume and all local data."
	docker compose down -v
	$(MAKE) local-setup DATABASE_URL="$(LOCAL_DATABASE_URL)"

smoke-auth:
	BASE_URL="$(BASE_URL)" SMOKE_AUTH_BASE_URL="$(SMOKE_AUTH_BASE_URL)" SMOKE_AUTH_EMAIL="$(SMOKE_AUTH_EMAIL)" SMOKE_AUTH_PASSWORD="$(SMOKE_AUTH_PASSWORD)" go run ./cmd/smoke-auth

postman-test:
	$(NEWMAN) run "$(POSTMAN_COLLECTION)" -e "$(POSTMAN_ENV)" --env-var "baseUrl=$(POSTMAN_BASE_URL)" --env-var "email=$(POSTMAN_EMAIL)" --env-var "password=$(POSTMAN_PASSWORD)"

smoke-local:
	@./scripts/smoke-local.sh

# ---------- testing ----------

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration -timeout 300s ./...

# ---------- static analysis ----------

vet:
	go vet ./...

# ---------- OpenAPI ----------

openapi:
	go run ./cmd/openapi

openapi-check: openapi
	@echo "==> Checking openapi/openapi.json for uncommitted drift…"
	@git diff --exit-code -- openapi/openapi.json || { echo "openapi/openapi.json is out of date. Run 'make openapi' and commit the result."; exit 1; }

openapi-types:
	@mkdir -p "$(dir $(OPENAPI_TYPESCRIPT_OUT))"
	$(OPENAPI_TYPESCRIPT) openapi/openapi.json -o "$(OPENAPI_TYPESCRIPT_OUT)"

openapi-types-check: openapi-types
	@echo "==> Checking generated OpenAPI TypeScript types for drift…"
	@git diff --exit-code -- "$(OPENAPI_TYPESCRIPT_OUT)" || { echo "$(OPENAPI_TYPESCRIPT_OUT) is out of date. Run 'make openapi-types' and commit the result."; exit 1; }
	@test -z "$$(git status --porcelain -- "$(OPENAPI_TYPESCRIPT_OUT)" | grep -E '^.[^ ]' || true)" || { echo "$(OPENAPI_TYPESCRIPT_OUT) is untracked or out of date. Run 'make openapi-types' and commit the result."; git status --short -- "$(OPENAPI_TYPESCRIPT_OUT)"; exit 1; }

# ---------- aggregate ----------

check: fmt-check vet sqlc-check test test-race test-integration openapi-check openapi-types-check
	@echo "==> All checks passed."

dev:
	air -c .air.toml

# ---------- Docker ----------

docker-build:
	docker build -t wow-dashboard-api:local .

docker-run:
	docker run --rm -p 7272:7272 --env-file .env wow-dashboard-api:local
