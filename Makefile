# Instala tudo num ambiente virtual dentro do projeto (.venv), sem tocar no Python do
# sistema (PEP 668 / "externally-managed-environment").
PREFIX ?= $(HOME)/.local
PYTHON ?= python3
VENV   ?= $(CURDIR)/.venv
VPY     = $(VENV)/bin/python

.PHONY: install venv install-python install-tui build test clean

install: install-python install-tui
	@echo
	@echo "Pronto. Comandos instalados em $(PREFIX)/bin: revisor, revisor-tui"
	@case ":$$PATH:" in *":$(PREFIX)/bin:"*) ;; *) \
	  echo "Atenção: $(PREFIX)/bin não está no PATH. Adicione ao seu shell:"; \
	  echo '  export PATH="$(PREFIX)/bin:$$PATH"';; esac

$(VPY):
	@$(PYTHON) -m venv $(VENV) || { \
	  echo; echo "Não consegui criar o ambiente virtual."; \
	  echo "No Debian/Ubuntu instale o módulo venv:  sudo apt install python3-venv"; \
	  echo "Ou, sem root, com o uv (https://docs.astral.sh/uv/):  uv venv --seed .venv && make install"; \
	  rm -rf $(VENV); exit 1; }
	$(VPY) -m pip install -q --upgrade pip

venv: $(VPY)

install-python: $(VPY)
	$(VPY) -m pip install -q -e '.[dev]'
	install -d $(PREFIX)/bin
	ln -sf $(VENV)/bin/revisor $(PREFIX)/bin/revisor

# O binário já sai apontando para o Python do .venv (pode trocar com --python ou REVISOR_PYTHON).
build:
	go build -ldflags "-X main.defaultPython=$(VPY)" -o revisor-tui ./cmd/revisor-tui

install-tui: build
	install -d $(PREFIX)/bin
	install -m 0755 revisor-tui $(PREFIX)/bin/revisor-tui

test: $(VPY)
	$(VPY) -m pytest -q
	go vet ./... && go test ./...

clean:
	rm -rf revisor-tui $(VENV)
