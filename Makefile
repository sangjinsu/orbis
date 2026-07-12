.PHONY: test run lint smoke

test:
	go test ./...

run:
	go run ./cmd/orbis serve

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...

smoke:
	go run ./cmd/orbis ws smoke
