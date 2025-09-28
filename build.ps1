$env:GOOS="linux"
$env:GOARCH="arm64"
$env:CGO_ENABLED="0"
go build -o bin/split-vpn-webui ./cmd/splitvpnwebui