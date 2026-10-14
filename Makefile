BIN := $(notdir $(CURDIR))

all:
	$(MAKE) build
	$(MAKE) run

.PHONY: build
build:
	go build -o $(BIN) ./cmd/$(BIN)

.PHONY: run
run: build
	./$(BIN)

.PHONY: test
test:
	go test -v ./...

.PHONY: fuzz
fuzz:
	go test ./... -run XXX -fuzz . -fuzztime 60s

.PHONY: install
install: build
	cp $(BIN) ~/.local/bin/

.PHONY: uninstall
uninstall:
	rm -f ~/.local/bin/$(BIN)

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: install-hooks
install-hooks:
	lefthook install
