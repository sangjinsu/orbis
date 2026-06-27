.PHONY: test run lint smoke

test:
	go test ./...

run:
	go run ./cmd/orbis serve

lint:
	go test ./...

smoke:
	go run ./cmd/orbis ws smoke
