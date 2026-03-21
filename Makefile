.PHONY: build test vet clean

build:
	go build -o claumon .

test:
	go test -v -race -count=1 ./...

vet:
	go vet ./...

clean:
	rm -f claumon
