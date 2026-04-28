BINARY := yaad
PKG    := ./cmd/yaad
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build run test clean install release

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(PKG)

run: build
	./$(BINARY)

test:
	CGO_ENABLED=0 go test -count=1 ./...

clean:
	rm -f $(BINARY)

install:
	CGO_ENABLED=0 go install $(LDFLAGS) $(PKG)

# Cross-compile release binaries
release:
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o dist/yaad_darwin_amd64  $(PKG)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o dist/yaad_darwin_arm64  $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o dist/yaad_linux_amd64   $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o dist/yaad_linux_arm64   $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64  go build $(LDFLAGS) -o dist/yaad_windows_amd64.exe $(PKG)
