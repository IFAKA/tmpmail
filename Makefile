BINARY=tmpmail
.PHONY: build test run fmt clean
build:
	go build -trimpath -ldflags="-s -w" -o bin/$(BINARY) ./cmd/tmpmail
test:
	go test ./...
run:
	go run ./cmd/tmpmail
fmt:
	gofmt -w cmd internal
clean:
	rm -rf bin dist
