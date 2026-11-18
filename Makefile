VERSION := $(shell cat VERSION)
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: ci build test vet dist clean

ci: vet test

build:
	go build $(GOFLAGS) -o bin/second-edit ./cmd/second-edit

test:
	go test ./...

vet:
	go vet ./...

dist:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/second-edit-darwin-arm64 ./cmd/second-edit
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/second-edit-linux-amd64 ./cmd/second-edit
	cd bin && shasum -a 256 second-edit-darwin-arm64 second-edit-linux-amd64 > checksums.txt

clean:
	rm -rf bin dist demo-out
