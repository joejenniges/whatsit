APP_NAME := RadioTranscriber
VERSION := 1.0.0

# MSYS2 ucrt64 provides whisper.cpp, ggml via pkg-config.
export PKG_CONFIG_PATH := /c/msys64/ucrt64/lib/pkgconfig

build:
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
	go build -ldflags "-s -w -H windowsgui" \
	-o $(APP_NAME).exe ./cmd/transcriber/

run:
	go run ./cmd/transcriber/

test:
	CGO_ENABLED=1 go test ./...

test-whisper:
	CGO_ENABLED=1 go test -tags whisper -v ./internal/transcriber/

DIST_DIR := dist
DLLS := libwhisper-1.dll ggml.dll ggml-base.dll ggml-cpu.dll ggml-vulkan.dll \
        libchromaprint.dll libgcc_s_seh-1.dll libstdc++-6.dll libwinpthread-1.dll

dist: build
	mkdir -p $(DIST_DIR)
	cp $(APP_NAME).exe $(DIST_DIR)/
	@for dll in $(DLLS); do \
		cp /c/msys64/ucrt64/bin/$$dll $(DIST_DIR)/ 2>/dev/null || echo "WARN: $$dll not found"; \
	done
	cd $(DIST_DIR) && zip -9 ../$(APP_NAME)-$(VERSION)-win64.zip *
	@echo "Created $(APP_NAME)-$(VERSION)-win64.zip"

clean:
	rm -f $(APP_NAME).exe
	rm -rf $(DIST_DIR)
	rm -f $(APP_NAME)-*-win64.zip
