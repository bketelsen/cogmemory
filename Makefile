BINARY := cogmemory
INSTALL_DIR := $(HOME)/.local/bin
PREVIOUS_SUFFIX := .previous
INSTALLED_BINARY := $(INSTALL_DIR)/$(BINARY)
PREVIOUS_BINARY := $(INSTALLED_BINARY)$(PREVIOUS_SUFFIX)
TMP_BINARY := $(INSTALLED_BINARY).tmp
SYSTEMD_USER_DIR := $(HOME)/.config/systemd/user

.PHONY: build test install install-versioned rollback-install install-service clean

build:
	go build -o $(BINARY) .

test:
	go test -race ./...

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(TMP_BINARY)
	mv -f $(TMP_BINARY) $(INSTALLED_BINARY)
	@echo "Installed to $(INSTALLED_BINARY)"

install-versioned: build
	mkdir -p $(INSTALL_DIR)
	@if [ -f "$(INSTALLED_BINARY)" ]; then \
		cp "$(INSTALLED_BINARY)" "$(PREVIOUS_BINARY)"; \
		echo "Backed up $(INSTALLED_BINARY) → $(PREVIOUS_BINARY)"; \
	fi
	cp $(BINARY) $(TMP_BINARY)
	mv -f $(TMP_BINARY) $(INSTALLED_BINARY)
	@echo "Installed to $(INSTALLED_BINARY)"

rollback-install:
	@test -f "$(PREVIOUS_BINARY)" \
		|| { echo "✗ No previous memory-service binary to roll back to" >&2; exit 1; }
	cp "$(PREVIOUS_BINARY)" "$(TMP_BINARY)"
	mv -f $(TMP_BINARY) $(INSTALLED_BINARY)
	@echo "Rolled back to $(PREVIOUS_BINARY)"

install-service:
	mkdir -p $(SYSTEMD_USER_DIR)
	cp deploy/cogmemory.service $(SYSTEMD_USER_DIR)/cogmemory.service
	@if command -v systemctl >/dev/null 2>&1; then \
		systemctl --user daemon-reload; \
	fi
	@echo "Installed service to $(SYSTEMD_USER_DIR)/cogmemory.service"
	@echo "Enable with: systemctl --user enable --now cogmemory"

clean:
	rm -f $(BINARY)
