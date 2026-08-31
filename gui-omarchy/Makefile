# mailbox-omarchy — build, run, test, install the Omarchy-styled Qt client.
#
# The build is CMake + Ninja (see README). This file just records the commands
# that are easy to forget and pins where `install` puts the binary: the
# mailbox.email notification widget shells out to `mailbox-omarchy` by name, so
# it has to land somewhere a login shell's PATH already covers.

PREFIX ?= $(HOME)/.local
BUILD  ?= build
BIN    := $(BUILD)/mailbox-omarchy
ARGS   ?=

# Qt's QML tools are not always on PATH; fall back to the versioned location.
QMLFORMAT ?= $(shell command -v qmlformat 2>/dev/null || echo /usr/lib/qt6/bin/qmlformat)

.DEFAULT_GOAL := build
.PHONY: build run test install clean

# Configure once (and whenever CMakeLists.txt changes); Ninja reconfigures
# itself for every other CMake edit.
$(BUILD)/build.ninja: CMakeLists.txt
	cmake -B $(BUILD) -G Ninja

build: $(BUILD)/build.ninja
	cmake --build $(BUILD)

# make run ARGS='--open 123 --ql'   jump straight into a message
# make run ARGS='--compose'         straight into the composer
run: build
	$(BIN) $(ARGS)

# No unit suite: the C++ compile is the real check, and this adds a parse gate
# over every QML file so a broken binding fails here instead of at launch.
test: build
	@for f in qml/*.qml; do \
		$(QMLFORMAT) -n "$$f" >/dev/null || { echo "qml parse failed: $$f"; exit 1; }; \
	done
	@echo "ok: binary built, $$(ls qml/*.qml | wc -l) QML files parse"

# Drops the binary at $(PREFIX)/bin/mailbox-omarchy. ~/.local/bin is on PATH
# for a login shell, which is how the notification widget launches it.
install: build
	cmake --install $(BUILD) --prefix $(PREFIX)

clean:
	rm -rf $(BUILD)
