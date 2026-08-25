package skill

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed SKILL.md
var files embed.FS

const Marker = ".managed-by-mailbox-cli"

func PackagedSkill() string {
	data, _ := files.ReadFile("SKILL.md")
	return string(data)
}

func Digest(text *string) string {
	raw := []byte(PackagedSkill())
	if text != nil {
		raw = []byte(*text)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

func DefaultTargets(home string) []string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	targets := []string{filepath.Join(home, ".agents", "skills", "mailbox")}
	for _, dir := range []string{".grok", ".claude"} {
		p := filepath.Join(home, dir, "skills")
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			targets = append(targets, filepath.Join(p, "mailbox"))
		}
	}
	return targets
}

type CopyRow struct {
	Path      string
	Installed bool
	Managed   bool
	Current   bool
}

func InstalledCopies(home string) []CopyRow {
	packaged := Digest(nil)
	var rows []CopyRow
	for _, path := range DefaultTargets(home) {
		marker := filepath.Join(path, Marker)
		skillPath := filepath.Join(path, "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			_, statErr := os.Stat(marker)
			rows = append(rows, CopyRow{Path: path, Managed: statErr == nil})
			continue
		}
		text := string(raw)
		rows = append(rows, CopyRow{Path: path, Installed: true, Managed: fileExists(marker), Current: Digest(&text) == packaged})
	}
	return rows
}

func InstallSkill(home string) ([]string, error) {
	dests := DefaultTargets(home)
	for _, dest := range dests {
		if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
			marker := filepath.Join(dest, Marker)
			entries, err := os.ReadDir(dest)
			if err != nil {
				return nil, err
			}
			if len(entries) > 0 && !fileExists(marker) {
				return nil, fmt.Errorf("refusing to overwrite unmanaged skill at %s; add %s to adopt it", dest, Marker)
			}
		}
	}
	text := PackagedSkill()
	var written []string
	for _, dest := range dests {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(text), 0o644); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dest, Marker), []byte("mailbox-cli\n"), 0o644); err != nil {
			return nil, err
		}
		written = append(written, dest)
	}
	return written, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var _ = strings.TrimSpace
