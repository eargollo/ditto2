.PHONY: build test run docker-build tailwind lint tidy

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

run: build
	./$(BINARY) --config config.yaml.example

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
