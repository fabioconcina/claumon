VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o claumon .

test:
	go test -v -race -count=1 ./...

vet:
	go vet ./...

clean:
	rm -f claumon
