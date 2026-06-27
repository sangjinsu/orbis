.PHONY: test run lint

test:
	go test ./...

run:
	go run ./cmd/orbis serve

lint:
	go test ./...
