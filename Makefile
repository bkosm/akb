GO_MODULE := ./go/akb/...
BIN       := bin/akb

.PHONY: build test lint fmt vet integration docker-build docker-run clean

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

docker-build:
	docker build -t akb:local .

docker-run:
	docker run --rm -i akb:local local

clean:
	rm -f $(BIN)
