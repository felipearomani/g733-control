VERSION ?= $(shell date +%Y.%m.%d)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test vet clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/g733 ./cmd/g733

# Installs into $(go env GOPATH)/bin (~/go/bin), which is on PATH.
install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/g733

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin
