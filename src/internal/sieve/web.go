package sieve

import (
	"html/template"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var webTmpl = template.Must(template.New("index").Parse(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Mail Routing - Sieve Lists</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .container { max-width: 800px; margin: 0 auto; }
        .section { margin-bottom: 30px; border: 1px solid #ddd; padding: 20px; border-radius: 5px; }
        .section h2 { margin-top: 0; color: #333; }
        .email-list { margin: 10px 0; }
        .email-item { display: inline-block; background: #f0f0f0; padding: 5px 10px; margin: 2px; border-radius: 3px; font-size: 14px; }
        .remove-btn { color: red; background: none; border: none; padding: 0; margin-left: 5px; font-weight: bold; cursor: pointer; }
        .remove-form { display: inline; }
        .add-form { margin-top: 10px; }
        input[type="email"] { padding: 5px; width: 200px; margin-right: 10px; }
        button { padding: 5px 10px; background: #007bff; color: white; border: none; border-radius: 3px; cursor: pointer; }
        button:hover { background: #0056b3; }
        .description { color: #666; font-size: 14px; margin-top: 5px; }
        .stats { background: #f8f9fa; padding: 15px; border-radius: 5px; margin-bottom: 20px; }
        .search-box { margin: 10px 0; padding: 5px; width: 250px; border: 1px solid #ddd; border-radius: 3px; }
        .hidden { display: none !important; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Mail Routing - Sieve Lists Management</h1>

        <div class="stats">
            <h3>Settings:</h3>
            <p>Sieve Server: {{.SieveHost}}:{{.SievePort}}</p>
            <p>Username: {{.Username}}</p>
            <p>Last updated: {{.LastUpdated.Format "2006-01-02 15:04:05"}}</p>
        </div>

        <div class="section">
            <h2>Blacklist (Discarded Emails)</h2>
            <p class="description">Emails from these addresses are automatically discarded.</p>
            <input type="text" class="search-box" placeholder="Search blacklist..." onkeyup="filterList('blacklist', this.value)">
            <div class="email-list" id="blacklist-container">
                {{range .Blacklist}}
                    <span class="email-item">{{.}}<form method="POST" action="/remove?list=blacklist" class="remove-form" onsubmit="return confirm('Remove {{.}} from blacklist?')"><input type="hidden" name="email" value="{{.}}"><button type="submit" class="remove-btn">×</button></form></span>
                {{end}}
            </div>
            <form method="POST" action="/add?list=blacklist" class="add-form">
                <input type="email" name="email" placeholder="user@example.com" required>
                <button type="submit">Add to Blacklist</button>
            </form>
        </div>

        <div class="section">
            <h2>Whitelist (Direct to Inbox)</h2>
            <p class="description">Emails from these addresses go directly to your inbox without review.</p>
            <input type="text" class="search-box" placeholder="Search whitelist..." onkeyup="filterList('whitelist', this.value)">
            <div class="email-list" id="whitelist-container">
                {{range .Whitelist}}
                    <span class="email-item">{{.}}<form method="POST" action="/remove?list=whitelist" class="remove-form" onsubmit="return confirm('Remove {{.}} from whitelist?')"><input type="hidden" name="email" value="{{.}}"><button type="submit" class="remove-btn">×</button></form></span>
                {{end}}
            </div>
            <form method="POST" action="/add?list=whitelist" class="add-form">
                <input type="email" name="email" placeholder="user@example.com" required>
                <button type="submit">Add to Whitelist</button>
            </form>
        </div>

        <div class="section">
            <h2>Paper Trail (INBOX/Paper Trail)</h2>
            <p class="description">Emails from these addresses go to Paper Trail folder and are marked as read.</p>
            <input type="text" class="search-box" placeholder="Search paper trail..." onkeyup="filterList('papertrail', this.value)">
            <div class="email-list" id="papertrail-container">
                {{range .PaperTrail}}
                    <span class="email-item">{{.}}<form method="POST" action="/remove?list=papertrail" class="remove-form" onsubmit="return confirm('Remove {{.}} from paper trail?')"><input type="hidden" name="email" value="{{.}}"><button type="submit" class="remove-btn">×</button></form></span>
                {{end}}
            </div>
            <form method="POST" action="/add?list=papertrail" class="add-form">
                <input type="email" name="email" placeholder="user@example.com" required>
                <button type="submit">Add to Paper Trail</button>
            </form>
        </div>

        <div class="section">
            <h2>Feed (INBOX/Feed)</h2>
            <p class="description">Newsletters and bulk emails from these addresses go to Feed folder and are marked as read.</p>
            <input type="text" class="search-box" placeholder="Search feed..." onkeyup="filterList('feed', this.value)">
            <div class="email-list" id="feed-container">
                {{range .Feed}}
                    <span class="email-item">{{.}}<form method="POST" action="/remove?list=feed" class="remove-form" onsubmit="return confirm('Remove {{.}} from feed?')"><input type="hidden" name="email" value="{{.}}"><button type="submit" class="remove-btn">×</button></form></span>
                {{end}}
            </div>
            <form method="POST" action="/add?list=feed" class="add-form">
                <input type="email" name="email" placeholder="user@example.com" required>
                <button type="submit">Add to Feed</button>
            </form>
        </div>

        <div class="section">
            <h2>Actions</h2>
            <form method="POST" action="/reload" style="display:inline"><button type="submit">Reload from Sieve Server</button></form>
            <button onclick="location.reload()">Refresh Page</button>
        </div>
    </div>

    <script>
        // Auto-refresh every 30 seconds
        setInterval(function() {
            location.reload();
        }, 30000);

        // Fuzzy search function - simple string matching with case insensitive search
        function fuzzySearch(str, query) {
            if (!query) return true;
            str = str.toLowerCase();
            query = query.toLowerCase();

            // Split query into individual characters and find each in sequence
            let queryIndex = 0;
            for (let char of str) {
                if (char === query[queryIndex]) {
                    queryIndex++;
                    if (queryIndex === query.length) {
                        return true;
                    }
                }
            }
            return false;
        }

        // Filter function for each list
        function filterList(listType, query) {
            const container = document.getElementById(listType + '-container');
            const items = container.getElementsByClassName('email-item');
            let visibleCount = 0;

            for (let item of items) {
                // Get the email text (excluding the × link)
                const emailText = item.textContent.replace('×', '').trim();
                if (fuzzySearch(emailText, query)) {
                    item.classList.remove('hidden');
                    visibleCount++;
                } else {
                    item.classList.add('hidden');
                }
            }
        }
    </script>
</body>
</html>
`))

type pageData struct {
	SieveHost   string
	SievePort   int
	Username    string
	LastUpdated time.Time
	Blacklist   []string
	Whitelist   []string
	PaperTrail  []string
	Feed        []string
}

// WebUI serves the list-management interface on top of the Sieve server.
type WebUI struct {
	Server Server

	mu      sync.Mutex
	lists   *Lists
	fetched time.Time
}

// current returns a private copy of the lists; handlers mutate their copy and
// hand it back to save, so no two requests ever touch the same slice.
func (ui *WebUI) current() (*Lists, time.Time, error) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	lists, fetched, err := ui.cachedLocked()
	if err != nil {
		return nil, time.Time{}, err
	}
	return lists.Clone(), fetched, nil
}

func (ui *WebUI) cachedLocked() (*Lists, time.Time, error) {
	if ui.lists != nil && time.Since(ui.fetched) < 5*time.Second {
		return ui.lists, ui.fetched, nil
	}
	content, err := ui.Server.GetScript()
	if err != nil {
		return nil, time.Time{}, err
	}
	lists, err := ParseScript(content)
	if err != nil {
		return nil, time.Time{}, err
	}
	ui.lists = lists
	ui.fetched = time.Now()
	return lists, ui.fetched, nil
}

// edit runs one read-modify-write under the lock so concurrent adds and
// removes cannot clobber each other.
func (ui *WebUI) edit(change func(*Lists) bool) error {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	lists, _, err := ui.cachedLocked()
	if err != nil {
		return err
	}
	updated := lists.Clone()
	if !change(updated) {
		return nil
	}
	if err := ui.Server.PutScript(GenerateScript(updated)); err != nil {
		return err
	}
	ui.lists = updated
	ui.fetched = time.Now()
	return nil
}

var listKeys = map[string]func(*Lists) *[]string{
	"blacklist":  func(l *Lists) *[]string { return &l.Blacklist },
	"whitelist":  func(l *Lists) *[]string { return &l.Whitelist },
	"papertrail": func(l *Lists) *[]string { return &l.PaperTrail },
	"feed":       func(l *Lists) *[]string { return &l.Feed },
}

// Handler returns the HTTP handler for the UI.
func (ui *WebUI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", ui.handleIndex)
	mux.HandleFunc("/add", ui.handleAdd)
	mux.HandleFunc("/remove", ui.handleRemove)
	mux.HandleFunc("/reload", ui.handleReload)
	return mux
}

// server wraps the handler with timeouts so a stalled client cannot pin a
// connection open indefinitely.
func (ui *WebUI) server() *http.Server {
	return &http.Server{
		Handler:           ui.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// Serve blocks serving the UI on an already-bound listener.
func (ui *WebUI) Serve(listener net.Listener) error {
	return ui.server().Serve(listener)
}

// ListenAndServe blocks serving the UI on addr.
func (ui *WebUI) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("sieve: web interface on http://%s", listener.Addr())
	return ui.Serve(listener)
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func (ui *WebUI) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lists, updated, err := ui.current()
	if err != nil {
		log.Printf("sieve: web: %v", err)
		http.Error(w, "Failed to get current lists", http.StatusInternalServerError)
		return
	}
	data := pageData{
		SieveHost:   ui.Server.Host,
		SievePort:   ui.Server.Port,
		Username:    ui.Server.Email,
		LastUpdated: updated,
		Blacklist:   sortedCopy(lists.Blacklist),
		Whitelist:   sortedCopy(lists.Whitelist),
		PaperTrail:  sortedCopy(lists.PaperTrail),
		Feed:        sortedCopy(lists.Feed),
	}
	w.Header().Set("Content-Type", "text/html")
	if err := webTmpl.Execute(w, data); err != nil {
		log.Printf("sieve: web render: %v", err)
	}
}

func (ui *WebUI) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := r.URL.Query().Get("list")
	email := strings.TrimSpace(r.FormValue("email"))
	get := listKeys[key]
	if get == nil || !ValidAddress(email) {
		http.Error(w, "Missing or invalid list/email parameter", http.StatusBadRequest)
		return
	}
	added := false
	err := ui.edit(func(lists *Lists) bool {
		target := get(lists)
		if contains(*target, email) {
			return false
		}
		*target = append(*target, email)
		added = true
		return true
	})
	if err != nil {
		log.Printf("sieve: web: %v", err)
		http.Error(w, "Failed to update sieve script", http.StatusInternalServerError)
		return
	}
	if added {
		log.Printf("sieve: web: added %s to %s", email, key)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ui *WebUI) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := r.URL.Query().Get("list")
	email := strings.TrimSpace(r.FormValue("email"))
	get := listKeys[key]
	if get == nil || email == "" {
		http.Error(w, "Missing list or email parameter", http.StatusBadRequest)
		return
	}
	removed := false
	err := ui.edit(func(lists *Lists) bool {
		target := get(lists)
		kept := []string{}
		for _, addr := range *target {
			if addr != email {
				kept = append(kept, addr)
			}
		}
		if len(kept) == len(*target) {
			return false
		}
		*target = kept
		removed = true
		return true
	})
	if err != nil {
		log.Printf("sieve: web: %v", err)
		http.Error(w, "Failed to update sieve script", http.StatusInternalServerError)
		return
	}
	if removed {
		log.Printf("sieve: web: removed %s from %s", email, key)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ui *WebUI) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ui.mu.Lock()
	ui.lists = nil
	ui.mu.Unlock()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
