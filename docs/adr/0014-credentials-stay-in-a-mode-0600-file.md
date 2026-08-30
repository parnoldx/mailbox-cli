# Credentials stay in a mode-0600 file

Passwords live in `~/.config/mailbox/config.toml`, mode 0600, in plaintext. We do
not use the Secret Service, although `org.freedesktop.secrets` is running on the
desktop and `secret-tool` is installed.

Recording this because the obvious assumption is the opposite, and someone will
otherwise "fix" it. The Daemon is long-lived and starts at login; a keyring that
is locked at boot means no mail until a human unlocks it, and turns a background
service into something that prompts. On the VPS there is no keyring at all, so
the file path has to exist regardless — and having it exist as a fallback is the
same as having it as the mechanism, minus a second code path.

On a single-user machine with an encrypted disk, a 0600 file is not the weak link.
