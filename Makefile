PREFIX ?= $(HOME)/.local
PYTHON ?= python3

.PHONY: install install-python install-tui build test clean

install: install-python install-tui

install-python:
	$(PYTHON) -m pip install -e .

build:
	cd tui && go build -o revisor-tui .

install-tui: build
	install -d $(PREFIX)/bin
	install -m 0755 tui/revisor-tui $(PREFIX)/bin/revisor-tui

test:
	$(PYTHON) -m pytest -q
	cd tui && go vet ./... && go test ./...

clean:
	rm -f tui/revisor-tui
