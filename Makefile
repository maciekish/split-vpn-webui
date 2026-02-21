BINARY := split-vpn-webui
CMD := ./cmd/splitvpnwebui
DIST_DIR := dist

.PHONY: test build-amd64 build-arm64 build install

test:
	go test ./...

build-amd64:
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY)-linux-amd64 $(CMD)

build-arm64:
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY)-linux-arm64 $(CMD)

build: build-amd64 build-arm64

install:
	bash ./install.sh
