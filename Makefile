APP := openclaw-node-kobo
PKG := ./cmd/openclaw-node-kobo

.PHONY: build build-arm test clean

build:
	go build -o $(APP) $(PKG)

build-arm:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o $(APP) $(PKG)

test:
	go test ./...

clean:
	rm -f $(APP)
