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

# Runs conversions through the real emulators in Docker. STATES and ROMS are folders; CORE is an
# optional extra mGBA libretro core, such as a Steam Deck's.
.PHONY: test-realcore
test-realcore:
	test/realcore/run.sh "$(STATES)" "$(ROMS)" $(CORE)

# The Game Boy checks: battery saves on test cartridges through every core, and Gambatte states.
# CORES is a folder of RetroArch's GB cores, such as a Steam Deck's; STATES and ROMS optionally
# Gambatte states and their ROMs.
.PHONY: test-realcore-gb
test-realcore-gb:
	test/realcore/run-saves.sh "$(CORES)" $(if $(STATES),"$(STATES)" "$(ROMS)")

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
