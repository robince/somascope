.DEFAULT_GOAL := help

.PHONY: build dev fmt test frontend frontend-dev ensure-embed-dir help

ensure-embed-dir:
	@mkdir -p internal/web/dist
	@test -n "$$(ls internal/web/dist/ 2>/dev/null)" || printf '%s\n' '<!doctype html><html lang="en"><meta charset="utf-8"><title>somascope</title><body><p>Frontend assets are not built yet. Run the frontend build to embed the real UI.</p></body></html>' > internal/web/dist/stub.html

build: ensure-embed-dir
	go build -o somascope ./cmd/somascope

dev: ensure-embed-dir
	go run ./cmd/somascope

fmt:
	gofmt -w ./cmd ./internal

test: ensure-embed-dir
	go test ./...

frontend:
	rm -rf internal/web/dist
	cp -r frontend/dist internal/web/dist

frontend-dev:
	@echo "Frontend workspace scaffolded in ./frontend; install dependencies before running Vite."

help:
	@printf '%s\n' \
		'build         Build the local binary' \
		'dev           Run the local server' \
		'fmt           Format Go sources' \
		'test          Run Go tests' \
		'frontend-dev  Note about the frontend scaffold'
