BINARY_NAME := ssh2socks5
ANDROID_PKG := com.example.ssh2socks5
ANDROID_API := 30
ANDROID_ARCH := arm64
GO_FILES := $(shell find . -name '*.go')
ANDROID_DIR := android
CMD_DIR := cmd/ssh2socks5

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

PARAMS = -lport=1082 \
         -host=35.193.63.104 \
         -user=bg \
         -key=/home/bg/Documents/code/backup/.ssh/google-france-key \
				 -proxyType=socks5

.PHONY: run
run: build-go
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
