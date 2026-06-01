VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build build-benchtools test vet clean docs

build:
	go build -ldflags "-X main.version=$(VERSION)" -o claumon .

# Same as build, but includes the dev-only `bench` and `diagnostics`
# subcommands (excluded from release builds via the benchtools tag).
build-benchtools:
	go build -tags benchtools -ldflags "-X main.version=$(VERSION)" -o claumon .

test:
	go test -v -race -count=1 ./...

vet:
	go vet ./...

clean:
	rm -f claumon
	rm -f internal/forecast/MODEL.pdf

# Rebuild internal/forecast/MODEL.pdf from MODEL.tex. Requires `tectonic`
# (brew install tectonic). The PDF is committed so a working tree without
# tectonic still has the rendered spec.
docs: internal/forecast/MODEL.pdf

internal/forecast/MODEL.pdf: internal/forecast/MODEL.tex
	cd internal/forecast && tectonic MODEL.tex
	rm -f internal/forecast/MODEL.log
