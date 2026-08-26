package cli

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	"mailbox/src/internal/config"
	"mailbox/src/internal/format"
	"mailbox/src/internal/mail"
	"mailbox/src/internal/sieve"
)

// sweepAsideLoop returns due Aside messages to the Inbox every 30 minutes.
func sweepAsideLoop(acct *config.Account) {
	for {
		m := mail.New(acct)
		returned, err := m.SweepAside(time.Now())
		m.Close()
		if err != nil {
			log.Printf("serve: aside sweep: %v", err)
		} else {
			for _, r := range returned {
				log.Printf("serve: aside: %s returned to Inbox (was due %s)", r.ID, r.Due.Format(time.RFC3339))
			}
		}
		time.Sleep(30 * time.Minute)
	}
}

func newSieveServer(acct *config.Account) sieve.Server {
	sievePort := 4190
	if raw := os.Getenv("MAILBOX_SIEVE_PORT"); raw != "" {
		sievePort, _ = strconv.Atoi(raw)
	}
	return sieve.Server{
		Host:     pickEnv("MAILBOX_SIEVE_HOST", acct.IMAPHost),
		Port:     sievePort,
		UseTLS:   true,
		Email:    acct.Email,
		Password: acct.Password,
	}
}

// cmdServe runs the mail routing service: watch routing folders,
// keep the "logic" Sieve script in sync, optional web UI.
func cmdServe(flags *parsed, out *format.Output) (int, error) {
	acct, err := config.LoadAccount(false, false)
	if err != nil {
		return 0, err
	}

	interval := 30 * time.Second
	if raw := flags.one("interval"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return 0, usageErr("--interval must be a positive integer (seconds)")
		}
		interval = time.Duration(n) * time.Second
	}

	server := newSieveServer(acct)

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

	go sweepAsideLoop(acct)

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

// --- sieve script management ---

func cmdSieve(cmd string, flags *parsed, out *format.Output) (int, error) {
	acct, err := config.LoadAccount(false, false)
	if err != nil {
		return 0, err
	}
	server := newSieveServer(acct)
	switch cmd {
	case "list":
		names, active, err := server.ListScripts()
		if err != nil {
			return 0, err
		}
		rows := make([]*format.OM, 0, len(names))
		for _, name := range names {
			rows = append(rows, format.NewOM("name", name, "active", name == active))
		}
		return format.WriteList(rows, []col{{"name", "Name"}, {"active", "Active"}}, out), nil
	case "get":
		name := sieve.ScriptName
		if len(flags.positional) > 0 {
			name = flags.positional[0]
		}
		content, err := server.GetScriptNamed(name)
		if err != nil {
			return 0, err
		}
		if path := flags.one("output"); path != "" {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return 0, err
			}
			return format.WriteOK(format.NewOM("name", name, "path", path), out, ""), nil
		}
		if out.JSON || out.Quiet {
			return format.WriteOK(format.NewOM("name", name, "content", content), out, ""), nil
		}
		fmt.Println(content)
		return 0, nil
	case "put":
		if len(flags.positional) != 2 {
			return printUsage("sieve", "put"), nil
		}
		name, file := flags.positional[0], flags.positional[1]
		var content []byte
		if file == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return 0, err
			}
			content = data
		} else {
			var err error
			content, err = os.ReadFile(file)
			if err != nil {
				return 0, err
			}
		}
		if err := server.PutScriptNamed(name, string(content)); err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("name", name, "bytes", len(content)), out, ""), nil
	case "activate":
		if len(flags.positional) != 1 {
			return printUsage("sieve", "activate"), nil
		}
		name := flags.positional[0]
		if err := server.ActivateScript(name); err != nil {
			return 0, err
		}
		return format.WriteOK(format.NewOM("name", name, "active", true), out, ""), nil
	default:
		return printUsage("sieve", cmd), nil
	}
}
