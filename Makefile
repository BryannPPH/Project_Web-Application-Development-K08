ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: db-up db-down db-status migrate-up migrate-status bootstrap-admin backend-run backend-test backend-integration-test frontend-dev frontend-check

db-up:
	docker compose up -d --wait platform-db target-db

db-down:
	docker compose down

db-status:
	docker compose ps

migrate-up:
	cd backend && go run ./cmd/migrate up

migrate-status:
	cd backend && go run ./cmd/migrate status

bootstrap-admin:
	cd backend && go run ./cmd/bootstrap-admin

backend-run:
	cd backend && go run ./cmd/api

backend-test:
	cd backend && go test ./...

backend-integration-test:
	@cd backend && TEST_PLATFORM_DATABASE_URL="$(PLATFORM_DATABASE_URL)" go test ./tests/integration -count=1 -v

frontend-dev:
	cd frontend && bun run dev

frontend-check:
	cd frontend && bun run typecheck && bun run lint && bun run test && bun run build

