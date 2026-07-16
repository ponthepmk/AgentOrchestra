.PHONY: build test vet run-mcp clean

build:
	go build -o bin/ao ./cmd/ao

test:
	go test ./...

vet:
	go vet ./...

run-mcp: build
	./bin/ao mcp

clean:
	rm -rf bin
