# mailbox — build, test, install.
#
# There is no code generation and no vendoring here: `go build` is the build,
# and this file only writes down the flags that are easy to forget.

GO      ?= go
PREFIX  ?= $(HOME)/.local
SKILLS  ?= $(HOME)/.agents/skills
CLAUDE  ?= $(HOME)/.claude/skills
BIN     := bin/mailbox

# Where the Quickshell bar widgets go. Not a $(PREFIX) thing: Omarchy loads
# plugins from this fixed per-user directory, not from XDG data dirs.
OMARCHY_PLUGINS ?= $(HOME)/.config/omarchy/plugins

# What `mailbox version` reports. A binary built by hand says (devel) rather
# than claiming a release it is not.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo '(devel)')
LDFLAGS := -X 'mailbox/internal/cli.Version=$(VERSION)'

# Which live suite `make live` runs. It is one package on purpose — see below.
LIVE ?= ./internal/sievedrv/

.DEFAULT_GOAL := build
.PHONY: build test live vet fmt install install-gui install-plugins install-all \
	update-daemon update-daemon-local update-daemon-vps register-mailto skill

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/mailbox

test:
	$(GO) test ./...

# The live suites talk to the real account in the config, so they are named one
# at a time rather than run as a set:
#
#   make live LIVE=./internal/sievedrv/   writes a scratch sieve script, never
#                                         activates it, deletes it
#   make live LIVE=./internal/davdrv/     reads the calendars; -run TestLiveWrite
#                                         creates and removes its own task list
#   make live LIVE=./internal/imapdrv/    creates and destroys scratch folders,
#                                         and TestLiveSend sends a real mail
#
# `go test -tags live ./...` would run all of them, including that send. That is
# why this target has a default of one package and not a wildcard.
live:
	$(GO) test -tags live -v $(LIVE)

# Both tag sets: the live files only compile under `-tags live`, so vetting
# without it leaves the ones most likely to have gone stale unchecked.
vet:
	$(GO) vet ./...
	$(GO) vet -tags live ./...
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

fmt:
	gofmt -w cmd internal

# Installs the CLI to ~/.local/bin, which is on PATH. Note that this repo's own
# bin/ is earlier on PATH than that, so `make build` is what changes which
# binary your shell runs here; install is for having it outside this directory.
#
# This is the CLI on its own — the common case, and the only part that is pure
# Go. The GUI (Qt 6 + WebEngine) and the Omarchy widgets are opt-in: see
# `install-gui`, `install-plugins`, or `install-all` for the lot.
install: build
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/mailbox

# The desktop client. Delegates to gui/'s own CMake build, which also drops the
# .desktop entry and the hicolor icon so `mailbox-gui` shows up in the launcher
# and the mailbox.email widget can shell out to it by name. Needs Qt 6 with
# WebEngineQuick, PdfQuick and QuickDialogs2 — that is why it is not in
# `install`.
install-gui:
	$(MAKE) -C gui install PREFIX=$(PREFIX)

# The Quickshell bar widgets (calendar + mail notifications). Omarchy reads
# them straight from $(OMARCHY_PLUGINS). `cp -rT` writes the tree at exactly
# that path — overwriting an earlier copy in place rather than nesting a
# second one inside it the way a plain `cp -r` would on the second run — and
# it never touches sibling plugins. Enable them afterwards with
# `omarchy plugin enable mailbox.clock mailbox.email`.
install-plugins:
	mkdir -p $(OMARCHY_PLUGINS)
	cp -rT plugins/mailbox.clock $(OMARCHY_PLUGINS)/mailbox.clock
	cp -rT plugins/mailbox.email $(OMARCHY_PLUGINS)/mailbox.email

# Everything this repo installs: CLI, agent skill, desktop client, widgets.
install-all: install skill install-gui install-plugins

# One account, two Daemons (ADR-0025): the home machine and the always-on VPS.
# They coordinate only through server-side records, never with each other
# directly, so a fix that lands on just one leaves the other free to keep
# acting on the behavior it replaced. This updates both.
update-daemon: update-daemon-local update-daemon-vps

# The home machine's own Daemon, under systemd --user. install rebuilds the
# binary; the restart is what actually swaps it in, since the running process
# does not notice a new file under it (mailbox.socket owns the unit's
# lifecycle across this, per ADR-0012 — only the process behind it cycles).
update-daemon-local: install
	systemctl --user restart mailbox.service

# The VPS Daemon: no user session and no `make install` path there (ADR-0025),
# so it is a manual deploy — cross-compiled here, shipped over ssh, swapped in
# stopped rather than live so nothing runs half-updated. $(VPS) is an ssh host
# alias, not a hostname: the address, user and key live in ~/.ssh/config, not
# in this repo. Restarting a remote service is the kind of action worth
# running by hand rather than unattended — see docs/adr/0025 before changing
# how this deploys.
VPS ?= misc
update-daemon-vps:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o /tmp/mailbox-vps ./cmd/mailbox
	scp /tmp/mailbox-vps $(VPS):/root/mailbox.new
	ssh $(VPS) 'systemctl stop mailbox.service && mv /root/mailbox.new /root/mailbox && chmod 755 /root/mailbox && systemctl start mailbox.service'

# Make mailbox-gui the system's default handler for mailto: links. Kept out
# of install-gui on purpose: that is a desktop-wide preference, not something
# an install should seize. The .desktop already declares the capability
# (MimeType=x-scheme-handler/mailto), so without this the client still shows
# up as a choice — this just makes it the default. Needs install-gui first.
register-mailto:
	xdg-mime default mailbox-gui.desktop x-scheme-handler/mailto

# Installs the agent skill. It is short by design: it carries what the binary
# cannot say about itself and sends the agent to `mailbox help` for the command
# surface, so a new command changes nothing here and there is nothing to
# regenerate. The last skill drifted because it copied the surface out.
#
# Its home is ~/.agents/skills, where every other skill on this machine lives,
# and ~/.claude/skills/mailbox is the symlink Claude Code reads it through —
# which is how the rest of them are wired. The last skill was two real copies in
# those two places, and they were free to disagree. This target refuses to
# delete anything: a directory where the symlink belongs is a decision for a
# person, not for make.
skill:
	install -Dm644 skill/SKILL.md $(SKILLS)/mailbox/SKILL.md
	@if [ -e $(CLAUDE)/mailbox ] && [ ! -L $(CLAUDE)/mailbox ]; then \
		echo "$(CLAUDE)/mailbox is a directory, not a symlink."; \
		echo "Remove it and run make skill again."; exit 1; \
	fi
	ln -sfn ../../.agents/skills/mailbox $(CLAUDE)/mailbox
