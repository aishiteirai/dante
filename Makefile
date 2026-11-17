PREFIX ?= $(HOME)/.local
PYTHON ?= python3

.PHONY: install install-python install-tui build test clean

install: install-python install-tui

install-python:
	$(PYTHON) -m pip install -e .

build:
	go build -o revisor-tui ./cmd/revisor-tui

install-tui: build
	install -d $(PREFIX)/bin
	install -m 0755 revisor-tui $(PREFIX)/bin/revisor-tui

test:
	$(PYTHON) -m pytest -q
	go vet ./... && go test ./...

clean:
	rm -f revisor-tui
