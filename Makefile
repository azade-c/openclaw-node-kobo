VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test cross clean

build:
	go build ./...

test:
	GOCACHE=/tmp/go-build go test ./... -race -count=1

cross:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="-X main.version=$(VERSION)" -o openclaw-node-kobo-arm7 ./cmd/openclaw-node-kobo
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-X main.version=$(VERSION)" -o openclaw-node-kobo-arm64 ./cmd/openclaw-node-kobo

clean:
	rm -f openclaw-node-kobo-arm7 openclaw-node-kobo-arm64
