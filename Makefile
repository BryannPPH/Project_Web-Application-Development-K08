ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: app-up app-down app-status app-logs docker-bootstrap-admin db-up db-down db-status migrate-up migrate-status bootstrap-admin backend-run backend-test backend-integration-test frontend-install frontend-dev frontend-check

app-up:
	docker compose up --build -d --wait

app-down:
	docker compose down

app-status:
	docker compose ps -a

app-logs:
	docker compose logs -f backend frontend

docker-bootstrap-admin:
	docker compose run --rm bootstrap-admin

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
	@test -n "$(PLATFORM_DATABASE_URL)" || (echo "PLATFORM_DATABASE_URL wajib diisi" && exit 1)
	@cd backend && TEST_PLATFORM_DATABASE_URL="$(PLATFORM_DATABASE_URL)" \
		TEST_TARGET_DATABASE_URL="$(TARGET_DATABASE_URL)" \
		TEST_TARGET_DB_HOST=localhost TEST_TARGET_DB_PORT="$(TARGET_DB_PORT)" \
		TEST_TARGET_DB_NAME="$(TARGET_DB_NAME)" TEST_TARGET_DB_USER="$(TARGET_DB_USER)" \
		TEST_TARGET_DB_PASSWORD="$(TARGET_DB_PASSWORD)" \
		go test ./tests/integration -count=1 -v

frontend-install:
	cd frontend && bun install --frozen-lockfile

frontend-dev:
	cd frontend && bun run dev

frontend-check:
	cd frontend && bun run typecheck && bun run lint && bun run test && bun run build

