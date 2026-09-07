.PHONY: build test cross-build clean

build:
	go build -o build/qbt-proton-guard ./cmd/qbt-proton-guard

test:
	go test ./...
	go vet ./...

cross-build:
	mkdir -p build
	GOOS=darwin GOARCH=arm64 go build -o build/qbt-proton-guard-darwin-arm64 ./cmd/qbt-proton-guard
	GOOS=darwin GOARCH=amd64 go build -o build/qbt-proton-guard-darwin-amd64 ./cmd/qbt-proton-guard
	GOOS=linux GOARCH=amd64 go build -o build/qbt-proton-guard-linux-amd64 ./cmd/qbt-proton-guard
	GOOS=windows GOARCH=amd64 go build -o build/qbt-proton-guard-windows-amd64.exe ./cmd/qbt-proton-guard

clean:
	rm -rf build
