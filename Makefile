.PHONY: build run clean test install

BINARY_NAME=ncc
INSTALL_PATH=/usr/local/bin

build:
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/cli

run: build
	./$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)
	go clean

test:
	go test ./...

install: build
	cp $(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	chmod +x $(INSTALL_PATH)/$(BINARY_NAME)

deps:
	go mod download
	go mod tidy