VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY = tnt
GOBIN = $(HOME)/go/bin

.PHONY: build install clean tidy

build:
	go build -ldflags "-X github.com/arturgoms/tnt/cmd.Version=$(VERSION)" -o $(BINARY) .

install: build
	mv $(BINARY) $(GOBIN)/$(BINARY)
	@echo "installed $(GOBIN)/$(BINARY)"

clean:
	rm -f $(BINARY)

tidy:
	go mod tidy
