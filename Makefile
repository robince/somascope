.DEFAULT_GOAL := help

.PHONY: build dev fmt test frontend-dev ensure-embed-dir help

ensure-embed-dir:
	@mkdir -p internal/web/dist
	@test -n "$$(ls internal/web/dist/ 2>/dev/null)" || echo ok > internal/web/dist/stub.html

build: ensure-embed-dir
	go build -o somascope ./cmd/somascope

dev: ensure-embed-dir
	go run ./cmd/somascope

fmt:
	gofmt -w ./cmd ./internal

test: ensure-embed-dir
	go test ./...

frontend-dev:
	@echo "Frontend workspace scaffolded in ./frontend; install dependencies before running Vite."

help:
	@printf '%s\n' \
		'build         Build the local binary' \
		'dev           Run the local server' \
		'fmt           Format Go sources' \
		'test          Run Go tests' \
		'frontend-dev  Note about the frontend scaffold'
