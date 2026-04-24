.PHONY: build run test fmt lint migrate sqlc docker-up docker-down

build:
	go build -o build/server ./cmd/server

run:
	go run ./cmd/server

test:
	go test -v ./...

fmt:
	go fmt ./...
	goimports -w .

lint:
	golangci-lint run

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir migrations -seq $$name

sqlc:
	sqlc generate

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f server
