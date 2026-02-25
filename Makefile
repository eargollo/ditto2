.PHONY: build test run docker-build tailwind lint tidy test-regression-local

BINARY := ditto
CMD     := ./cmd/ditto
DOCKER_TAG := ditto:latest

build:
	go build -o $(BINARY) $(CMD)

test:
	go test ./...

test-unit:
	go test ./internal/...

test-regression:
	go test ./tests/regression/...

# test-regression-local builds the binary, writes a temporary /tmp-based
# config, starts the server in the background, waits up to 10s for
# /api/status to respond, runs the full regression suite, then always
# kills the server.  Exit code matches `go test` (0 = pass, non-zero = fail).
TEST_CONFIG := /tmp/ditto-test.yaml

test-regression-local: build
	@printf 'scan_paths:\n  - /tmp\ndb_path: /tmp/ditto-test.db\ntrash_dir: /tmp/ditto-trash\nhttp_addr: ":8080"\nlog_level: info\n' \
		> $(TEST_CONFIG); \
	./$(BINARY) --config $(TEST_CONFIG) & \
	SERVER_PID=$$!; \
	trap "echo '==> Stopping ditto (PID '$$SERVER_PID')'; kill $$SERVER_PID 2>/dev/null; wait $$SERVER_PID 2>/dev/null" EXIT; \
	echo "==> Waiting for ditto to be ready (polling /api/status, up to 10s)..."; \
	for i in $$(seq 1 20); do \
		curl -sf http://localhost:8080/api/status >/dev/null 2>&1 && break; \
		sleep 0.5; \
	done; \
	curl -sf http://localhost:8080/api/status >/dev/null 2>&1 \
		|| { echo "ERROR: ditto server did not become ready after 10s" >&2; exit 1; }; \
	echo "==> Server ready — running regression tests..."; \
	go test ./tests/regression/... -v -timeout 5m; \
	STATUS=$$?; \
	if [ $$STATUS -ne 0 ]; then \
		echo "FAIL: regression tests failed (exit $$STATUS)" >&2; \
	else \
		echo "==> All regression tests passed."; \
	fi; \
	exit $$STATUS

DEV_CONFIG := /tmp/ditto-dev.yaml

# run builds and starts the server with a /tmp-based config suitable for local
# development. Data persists across restarts in /tmp/ditto-dev.db.
run: build
	@printf 'scan_paths:\n  - /tmp\ndb_path: /tmp/ditto-dev.db\ntrash_dir: /tmp/ditto-dev-trash\nhttp_addr: ":8280"\nlog_level: debug\n' \
		> $(DEV_CONFIG)
	./$(BINARY) --config $(DEV_CONFIG)

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

tailwind:
	npx tailwindcss -i web/static/css/tailwind.src.css -o web/static/css/tailwind.css --minify

tailwind-watch:
	npx tailwindcss -i web/static/css/tailwind.src.css -o web/static/css/tailwind.css --watch

docker-build:
	docker build -t $(DOCKER_TAG) .

clean:
	rm -f $(BINARY)
