package sieve

import (
	"crypto/tls"
	"fmt"
	"log"

	"go.guido-berhoerster.org/managesieve"
)

// Server locates the ManageSieve endpoint. TLS is opportunistic STARTTLS,
// like the old helper.
type Server struct {
	Host     string
	Port     int
	UseTLS   bool
	Email    string
	Password string
}

func (s Server) addr() string { return fmt.Sprintf("%s:%d", s.Host, s.Port) }

func (s Server) dial() (*managesieve.Client, error) {
	client, err := managesieve.Dial(s.addr())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Sieve server: %w", err)
	}
	if s.UseTLS && client.SupportsTLS() {
		if err := client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to start TLS: %w", err)
		}
	}
	auth := managesieve.PlainAuth("", s.Email, s.Password, s.Host)
	if err := client.Authenticate(auth); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to authenticate to Sieve server: %w", err)
	}
	return client, nil
}

// GetScript fetches the logic script body.
func (s Server) GetScript() (string, error) {
	client, err := s.dial()
	if err != nil {
		return "", err
	}
	defer client.Close()
	content, err := client.GetScript(ScriptName)
	if err != nil {
		return "", fmt.Errorf("failed to get %s script: %w", ScriptName, err)
	}
	return content, nil
}

// PutScript uploads the logic script body.
func (s Server) PutScript(content string) error {
	client, err := s.dial()
	if err != nil {
		return err
	}
	defer client.Close()
	warnings, err := client.PutScript(ScriptName, content)
	if err != nil {
		return fmt.Errorf("failed to upload %s script: %w", ScriptName, err)
	}
	if warnings != "" {
		log.Printf("sieve: script warnings: %s", warnings)
	}
	return nil
}

// EnsureScript creates the default logic script if the server has none.
func (s Server) EnsureScript() error {
	client, err := s.dial()
	if err != nil {
		return err
	}
	defer client.Close()
	scripts, _, err := client.ListScripts()
	if err != nil {
		return fmt.Errorf("failed to list Sieve scripts: %w", err)
	}
	for _, name := range scripts {
		if name == ScriptName {
			return nil
		}
	}
	log.Printf("sieve: no %s script on server, creating default", ScriptName)
	warnings, err := client.PutScript(ScriptName, GenerateScript(NewLists()))
	if err != nil {
		return fmt.Errorf("failed to upload default %s script: %w", ScriptName, err)
	}
	if warnings != "" {
		log.Printf("sieve: script warnings: %s", warnings)
	}
	return nil
}

// SyncMovement folds one movement into the server's lists (get → apply → put),
// skipping the upload when nothing changed.
func (s Server) SyncMovement(mv Movement) error {
	content, err := s.GetScript()
	if err != nil {
		return err
	}
	lists, err := ParseScript(content)
	if err != nil {
		return err
	}
	updated := lists.Clone()
	if !Apply(updated, mv) {
		return nil
	}
	if err := s.PutScript(GenerateScript(updated)); err != nil {
		return err
	}
	log.Printf("sieve: updated %s list with %s", mv.Folder, mv.Address)
	return nil
}
