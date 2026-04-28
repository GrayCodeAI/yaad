BINARY := yaad
PKG := ./cmd/yaad

.PHONY: build run test clean install

build:
	go build -o $(BINARY) $(PKG)

run: build
	./$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)

install:
	go install $(PKG)
