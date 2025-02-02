# Defining variables
BINARY_NAME := ssh2socks5
ANDROID_PKG := com.example.ssh2socks5
ANDROID_API := 30
ANDROID_ARCH := arm64
GO_FILES := $(shell find . -name '*.go')
ANDROID_DIR := android

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
	go build -tags='!android' -o $(BINARY_NAME) .

.PHONY: build-android
build-android:
	# Clean Android build cache
	# cd $(ANDROID_DIR) && ./gradlew clean --no-daemon
	# rm -rf $(HOME)/.gradle/caches/
	mkdir -p android/app/libs

	# Initialize gomobile
	gomobile init -v

	# Build AAR file
	GOPATH=$(HOME)/go \
	GOCACHE=$(HOME)/.cache/go-build \
	GOMODCACHE=$(HOME)/go/pkg/mod \
	gomobile bind -v \
	-target=android/$(ANDROID_ARCH) \
	-androidapi $(ANDROID_API) \
	-o $(ANDROID_DIR)/app/libs/proxy.aar \
	./mobile ./proxy

	# Build Android APK
	cd $(ANDROID_DIR) && ./gradlew build --no-daemon
	cp $(ANDROID_DIR)/app/build/outputs/apk/debug/app-debug.apk ssh2socks5.apk

.PHONY: build-all
build-all: build-go build-android

.PHONY: test
test:
	go test -v ./...

.PHONY: clean
clean:
	rm -f $(BINARY_NAME)
	rm -f ssh2socks5.apk
	rm -rf $(ANDROID_DIR)/app/maven/proxy.aar
	rm -rf $(ANDROID_DIR)/app/build
	rm -rf $(HOME)/.gradle/caches
	cd $(ANDROID_DIR) && ./gradlew clean --no-daemon

.PHONY: run
run: build-go
	./$(BINARY_NAME) -lport=1081 \
		-host=35.193.63.104 \
		-user=bg \
		-key=/home/bg/Documents/code/backup/.ssh/google-france-key


log/crash:
	adb logcat -b crash

log/cat:
	adb logcat | grep com.example.minimal
