PREFIX ?= $(HOME)/.local
PYTHON ?= python3
VENV   ?= $(CURDIR)/.venv
VPY     = $(VENV)/bin/python

.PHONY: install build test install-cli test-cli clean

# O dante é um binário Go independente: não precisa de Python.
install: build
	install -d $(PREFIX)/bin
	install -m 0755 dante $(PREFIX)/bin/dante
	rm -f $(PREFIX)/bin/revisor-tui
	@echo
	@echo "Pronto. Abra com:  dante"
	@case ":$$PATH:" in *":$(PREFIX)/bin:"*) ;; *) \
	  echo "Atenção: $(PREFIX)/bin não está no PATH. Adicione ao seu shell:"; \
	  echo '  export PATH="$(PREFIX)/bin:$$PATH"';; esac

build:
	go build -o dante ./cmd/dante

test:
	go vet ./... && go test ./...

# ---- opcional: CLI `revisor` em Python (Claude Agent SDK), para hooks e CI ----

$(VPY):
	@$(PYTHON) -m venv $(VENV) || { \
	  echo; echo "Não consegui criar o ambiente virtual."; \
	  echo "No Debian/Ubuntu instale o módulo venv:  sudo apt install python3-venv"; \
	  echo "Ou, sem root, com o uv (https://docs.astral.sh/uv/):  uv venv --seed .venv && make install-cli"; \
	  rm -rf $(VENV); exit 1; }
	$(VPY) -m pip install -q --upgrade pip

install-cli: $(VPY)
	$(VPY) -m pip install -q -e '.[dev]'
	install -d $(PREFIX)/bin
	ln -sf $(VENV)/bin/revisor $(PREFIX)/bin/revisor

test-cli: $(VPY)
	$(VPY) -m pytest -q

clean:
	rm -rf dante revisor-tui $(VENV)
