BINARY_NAME := ssh2socks5
ANDROID_PKG := com.example.ssh2socks5
ANDROID_API := 30
ANDROID_ARCH := arm64
GO_FILES := $(shell find . -name '*.go')
ANDROID_DIR := android
CMD_DIR := cmd/ssh2socks5

# Извлечение информации из SSH config (улучшенный вариант)
SSH_HOST_NAME := google-seoul
SSH_CONFIG_PATH := $(HOME)/.ssh/config
SSH_HOST := $(shell grep -A10 "^Host $(SSH_HOST_NAME)$$" $(SSH_CONFIG_PATH) | grep "HostName" | head -n1 | awk '{print $$2}')
SSH_USER := $(shell grep -A10 "^Host $(SSH_HOST_NAME)$$" $(SSH_CONFIG_PATH) | grep "User" | head -n1 | awk '{print $$2}')
SSH_KEY := $(shell grep -A10 "^Host $(SSH_HOST_NAME)$$" $(SSH_CONFIG_PATH) | grep "IdentityFile" | head -n1 | awk '{print $$2}')

# Раскрываем путь к ключу, если он содержит ~
SSH_KEY_EXPANDED := $(shell echo $(SSH_KEY) | sed 's:^~:$(HOME):')

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build-go      - Build the Go SSH proxy binary"
	@echo "  build-android - Build the Android APK"
	@echo "  build-all     - Build both Go binary and Android APK"
	@echo "  test          - Run Go tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  run           - Run the Go SSH proxy"
	@echo "  ssh-info      - Display extracted SSH connection info"

.PHONY: ssh-info
ssh-info:
	@echo "SSH Host: $(SSH_HOST)"
	@echo "SSH User: $(SSH_USER)"
	@echo "SSH Key:  $(SSH_KEY)"
	@echo "SSH Key (expanded): $(SSH_KEY_EXPANDED)"

.PHONY: build-go
build-go:
	go build -tags='!android' -o bin/$(BINARY_NAME) ./$(CMD_DIR)

.PHONY: build-android
build-android:
	mkdir -p android/app/libs
	gomobile init -v
	GOPATH=$(HOME)/go \
	GOCACHE=$(HOME)/.cache/go-build \
	GOMODCACHE=$(HOME)/go/pkg/mod \
	gomobile bind -v \
	-target=android/$(ANDROID_ARCH) \
	-androidapi $(ANDROID_API) \
	-o $(ANDROID_DIR)/app/libs/proxy.aar \
	./mobile ./proxy
	cd $(ANDROID_DIR) && ./gradlew build --no-daemon
	cp $(ANDROID_DIR)/app/build/outputs/apk/debug/app-debug.apk ssh2socks5.apk

.PHONY: build-all
build-all: build-go build-android

.PHONY: test
test:
	go test -v ./...

.PHONY: clean
clean:
	rm -f bin/$(BINARY_NAME)
	rm -f ssh2socks5.apk
	rm -rf $(ANDROID_DIR)/app/maven/proxy.aar
	rm -rf $(ANDROID_DIR)/app/build
	rm -rf $(HOME)/.gradle/caches
	cd $(ANDROID_DIR) && ./gradlew clean --no-daemon

# Определение параметров с правильными значениями
PARAMS = -lport=1082 \
         -host=$(SSH_HOST) \
         -user=$(SSH_USER) \
         -key=$(SSH_KEY_EXPANDED) \
         -proxyType=socks5

.PHONY: run
run: build-go
	@echo "Running with parameters:"
	@echo "  Host: $(SSH_HOST)"
	@echo "  User: $(SSH_USER)"
	@echo "  Key:  $(SSH_KEY_EXPANDED)"
	./bin/$(BINARY_NAME) $(PARAMS)

nix-run:
	nix run .#ssh2socks5 -- $(PARAMS)

.PHONY: log/crash
log/crash:
	adb logcat -b crash

.PHONY: log/cat
log/cat:
	adb logcat | grep com.example.minimal

update/gomod:
	gomod2nix generate
