GO_MODULE := ./go/akb/...
BIN       := bin/akb

.PHONY: build test lint fmt vet clean

build:
	go build -o $(BIN) ./go/akb/cmd/stdio/

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

clean:
	rm -f $(BIN)
