SHELL := /bin/bash
VERSION := $(shell tr -d '[:space:]' < VERSION)
PREFIX ?= /opt/system-maintenance
CONFIG_DIR ?= /etc/system-maintenance
HOSTS ?=
CERT ?=
KEY ?=
ADMIN_USER ?= $(shell id -un)
GO_CACHE ?= /tmp/system-maintenance-go-cache
LDFLAGS := -s -w -X main.version=$(VERSION)
BPF_CLANG ?= clang
BPFTOOL ?= bpftool
BPF_ARCH ?= x86
BPF_OBJECT := build/network_accounting.bpf.o

.PHONY: all help frontend frontend-deps bpf build test install setup upgrade uninstall purge start stop restart status logs cert-generate cert-install admin-add admin-remove admin-list clean

all: build

help:
	@echo "Servora v$(VERSION)"
	@echo
	@echo "Build:          make build | make test | make frontend-deps"
	@echo "Exact network:  make bpf  (requires clang, llvm, libbpf-dev, bpftool)"
	@echo "First setup:    make setup HOSTS=\"192.168.2.10,monitor.local\""
	@echo "Install only:   make install"
	@echo "Upgrade:        make upgrade"
	@echo
	@echo "Authorize current Linux user: make admin-add"
	@echo "Authorize another user:       make admin-add ADMIN_USER=username"
	@echo "Remove an administrator:      make admin-remove ADMIN_USER=username"
	@echo "List administrators:          make admin-list"
	@echo
	@echo "Generate TLS:    make cert-generate HOSTS=\"IP,DNS\""
	@echo "Install TLS:     make cert-install CERT=/path/fullchain.pem KEY=/path/privkey.pem"
	@echo "Lifecycle:       make start | stop | restart | status | logs"
	@echo "Removal:         make uninstall | make purge"

frontend:
	cd web && test -x node_modules/.bin/vite || npm ci
	cd web && npm run build

frontend-deps:
	cd web && npm ci

build/vmlinux.h:
	@command -v $(BPF_CLANG) >/dev/null || (echo "Missing clang. Install: sudo apt-get install -y clang llvm libbpf-dev" >&2; exit 2)
	@command -v $(BPFTOOL) >/dev/null || (echo "Missing bpftool." >&2; exit 2)
	mkdir -p build
	$(BPFTOOL) btf dump file /sys/kernel/btf/vmlinux format c > $@

$(BPF_OBJECT): internal/agent/bpf/network_accounting.c build/vmlinux.h
	$(BPF_CLANG) -O2 -g -target bpf -D__TARGET_ARCH_$(BPF_ARCH) \
	  -Ibuild -I/usr/include/$(shell uname -m)-linux-gnu \
	  -c $< -o $@
	@if command -v llvm-strip >/dev/null; then llvm-strip -g $@; fi

bpf: $(BPF_OBJECT)

build: frontend bpf
	mkdir -p build
	GOCACHE=$(GO_CACHE) CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o build/system-maintenance-monitor ./cmd/system-maintenance-monitor
	GOCACHE=$(GO_CACHE) CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "$(LDFLAGS)" -o build/system-maintenance-agent ./cmd/system-maintenance-agent

test:
	GOCACHE=$(GO_CACHE) go test ./...
	cd web && npm run lint
	cd web && npm test
	cd web && npm run build

install: build
	sudo ./install.sh

setup: install cert-generate
	sudo systemctl enable --now system-maintenance-agent.service system-maintenance-monitor.service system-maintenance.timer

upgrade: install
	sudo systemctl restart system-maintenance-agent.service system-maintenance-monitor.service

cert-generate:
	sudo $(PREFIX)/bin/generate-certificate "$(CONFIG_DIR)/tls/server.crt" "$(CONFIG_DIR)/tls/server.key" "$(HOSTS)"
	sudo chown root:system-maintenance-agent "$(CONFIG_DIR)/tls/server.key"
	sudo systemctl try-restart system-maintenance-monitor.service

cert-install:
	@test -n "$(CERT)" -a -n "$(KEY)" || (echo "Usage: make cert-install CERT=/path/fullchain.pem KEY=/path/privkey.pem" >&2; exit 2)
	sudo install -m 0644 "$(CERT)" "$(CONFIG_DIR)/tls/server.crt"
	sudo install -m 0640 -o root -g system-maintenance-agent "$(KEY)" "$(CONFIG_DIR)/tls/server.key"
	sudo systemctl restart system-maintenance-monitor.service

admin-add:
	sudo ./bin/manage-admin add "$(ADMIN_USER)"

admin-remove:
	@test -n "$(ADMIN_USER)" || (echo "Usage: make admin-remove ADMIN_USER=username" >&2; exit 2)
	sudo ./bin/manage-admin remove "$(ADMIN_USER)"

admin-list:
	./bin/manage-admin list

start:
	sudo systemctl start system-maintenance-agent.service system-maintenance-monitor.service
stop:
	sudo systemctl stop system-maintenance-monitor.service system-maintenance-agent.service
restart:
	sudo systemctl restart system-maintenance-agent.service system-maintenance-monitor.service
status:
	systemctl status system-maintenance-agent.service system-maintenance-monitor.service --no-pager
logs:
	journalctl -u system-maintenance-agent.service -u system-maintenance-monitor.service -f
uninstall:
	sudo ./uninstall.sh
purge:
	sudo PURGE=true ./uninstall.sh
clean:
	rm -f build/system-maintenance-monitor build/system-maintenance-agent build/network_accounting.bpf.o build/vmlinux.h
