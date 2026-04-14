# Build the ztabs background plugin for zellij.
#
# Requires a rustup-managed toolchain with the wasm32-wasip1 target. Homebrew
# rust ships only the host target and lacks wasm std, so we force `cargo` to
# find rustup's rustc/cargo by prepending them to PATH for the build.

RUSTUP_BIN ?= /opt/homebrew/opt/rustup/bin
TARGET     := wasm32-wasip1
WASM       := target/$(TARGET)/release/ztabs-plugin.wasm

.PHONY: build release debug clean

build: release

release:
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --release --target $(TARGET)
	@echo "Built: $(abspath $(WASM))"

debug:
	PATH="$(RUSTUP_BIN):$$PATH" cargo build --target $(TARGET)

clean:
	PATH="$(RUSTUP_BIN):$$PATH" cargo clean
