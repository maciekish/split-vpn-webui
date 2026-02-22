BINARY := split-vpn-webui
CMD := ./cmd/splitvpnwebui
DIST_DIR := dist
DEV_HOST ?= root@10.0.0.1
DEV_PORT ?= 22

.PHONY: test build-amd64 build-arm64 build install dev-deploy dev-cleanup dev-uninstall

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

dev-deploy:
	HOST=$(DEV_HOST) SSH_PORT=$(DEV_PORT) bash ./deploy/dev-deploy.sh

dev-cleanup:
	HOST=$(DEV_HOST) SSH_PORT=$(DEV_PORT) bash ./deploy/dev-cleanup.sh

dev-uninstall:
	HOST=$(DEV_HOST) SSH_PORT=$(DEV_PORT) bash ./deploy/dev-uninstall.sh
