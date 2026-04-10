GO_MODULE := ./go/akb/...
BIN       := bin/akb
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint fmt vet integration docker-build docker-run release-snapshot clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./go/akb/cmd/stdio/

test:
	go test -race $(GO_MODULE)

lint:
	golangci-lint run $(GO_MODULE)

fmt:
	cd go/akb && go fmt ./...

vet:
	go vet $(GO_MODULE)

integration:
	go test -race -tags=integration $(GO_MODULE)

docker-build:
	docker build -t akb:local .

docker-run:
	docker run --rm -i akb:local local

release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f $(BIN)
