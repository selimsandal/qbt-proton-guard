.PHONY: build test cross-build clean

build:
	go build -o build/qbt-proton-guard ./cmd/qbt-proton-guard

test:
	go test ./...
	go vet ./...

cross-build:
	scripts/release-build.sh

clean:
	rm -rf build
