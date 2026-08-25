package cli

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"mailbox/src/internal/config"
	"mailbox/src/internal/format"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/sieve"
)

// cmdServe runs the mail routing service: watch routing folders,
// keep the "logic" Sieve script in sync, optional web UI.
func cmdServe(flags *parsed, out *format.Output) (int, error) {
	acct, err := config.LoadAccount(false, false)
	if err != nil {
		return 0, err
	}

	sievePort := 4190
	if raw := os.Getenv("MAILBOX_SIEVE_PORT"); raw != "" {
		sievePort, _ = strconv.Atoi(raw)
	}
	interval := 30 * time.Second
	if raw := flags.one("interval"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return 0, usageErr("--interval must be a positive integer (seconds)")
		}
		interval = time.Duration(n) * time.Second
	}

	server := sieve.Server{
		Host:     pickEnv("MAILBOX_SIEVE_HOST", acct.IMAPHost),
		Port:     sievePort,
		UseTLS:   true,
		Email:    acct.Email,
		Password: acct.Password,
	}

	if flags.has("print") {
		content, err := server.GetScript()
		if err != nil {
			return 0, err
		}
		fmt.Println(content)
		return 0, nil
	}

	log.Printf("serve: %s on %s:%d (sieve %s:%d)", acct.Email, acct.IMAPHost, acct.IMAPPort, server.Host, server.Port)
	if err := server.EnsureScript(); err != nil {
		log.Printf("serve: warning: %v", err)
	}

	if flags.has("web") {
		port := flags.one("web-port")
		if port == "" {
			port = "8080"
		}
		go func() {
			ui := &sieve.WebUI{Server: server}
			if err := ui.ListenAndServe(":" + port); err != nil {
				log.Fatalf("serve: web interface failed: %v", err)
			}
		}()
	}

	for {
		if err := runWatcher(server, acct, interval); err != nil {
			log.Printf("serve: %v", err)
			log.Println("serve: reconnecting in 30 seconds...")
			time.Sleep(30 * time.Second)
		}
	}
}

func runWatcher(server sieve.Server, acct *config.Account, interval time.Duration) error {
	m := mail.New(acct)
	if err := m.Connect(); err != nil {
		return err
	}
	defer m.Close()

	w := &sieve.Watcher{MB: m, Interval: interval}
	if err := w.EnsureFolders(); err != nil {
		return err
	}
	if err := w.Snapshot(); err != nil {
		return err
	}
	log.Printf("serve: watching %d folders every %s", len(sieve.RuleFolders), interval)

	for {
		movements, err := w.Poll()
		if err != nil {
			return err
		}
		for _, mv := range movements {
			log.Printf("serve: %s <- %s", mv.Folder, mv.Address)
			if err := server.SyncMovement(mv); err != nil {
				log.Printf("serve: sieve update failed: %v", err)
			}
		}
		time.Sleep(interval)
	}
}

func pickEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
