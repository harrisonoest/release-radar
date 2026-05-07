.PHONY: build test vet clean install run

BINARY := release-radar
CMD    := .

build:
	go build -o $(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

lint: vet
	gofmt -l .

clean:
	rm -f $(BINARY)

install: build
	install -m 755 $(BINARY) $(HOME)/.local/bin/$(BINARY)

run: build
	./$(BINARY) $(ARGS)
