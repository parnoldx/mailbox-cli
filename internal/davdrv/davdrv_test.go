package davdrv

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mailbox/internal/sync/davsync"
)

// server answers the four documents this driver sends, the way Dovecot's and
// SOGo's do: paths rather than URLs in hrefs, namespaces on prefixes we did not
// choose, and a deleted object as a bare 404 response.
func server(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body string)) (*Client, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "me@example.org" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		handler(w, r, string(body))
	}))
	t.Cleanup(srv.Close)
	return New(Config{Endpoint: srv.URL + "/dav/", Username: "me@example.org", Password: "secret"}), srv.URL
}

func multiStatus(w http.ResponseWriter, body string) {
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>`+body)
}

func TestCollectionsAreDiscoveredNotConfigured(t *testing.T) {
	c, _ := server(t, func(w http.ResponseWriter, r *http.Request, body string) {
		switch {
		case strings.Contains(body, "current-user-principal"):
			multiStatus(w, `<D:multistatus xmlns:D="DAV:"><D:response>
			  <D:href>/dav/</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>
			  <D:prop><D:current-user-principal><D:href>/dav/principals/me/</D:href></D:current-user-principal></D:prop>
			</D:propstat></D:response></D:multistatus>`)
		case strings.Contains(body, "home-set"):
			multiStatus(w, `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:response>
			  <D:href>/dav/principals/me/</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>
			  <D:prop><C:calendar-home-set><D:href>/dav/calendars/me/</D:href></C:calendar-home-set></D:prop>
			</D:propstat></D:response></D:multistatus>`)
		default:
			multiStatus(w, `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="http://apple.com/ns/ical/">
			  <D:response><D:href>/dav/calendars/me/</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>
			    <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop></D:propstat></D:response>
			  <D:response><D:href>/dav/calendars/me/kalender/</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>
			    <D:prop><D:displayname>Kalender</D:displayname>
			      <D:resourcetype><D:collection/><C:calendar/></D:resourcetype>
			      <A:calendar-color>#3355ffff</A:calendar-color>
			      <C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set>
			  </D:prop></D:propstat></D:response>
			  <D:response><D:href>/dav/calendars/me/aufgaben/</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>
			    <D:prop><D:displayname>Aufgaben</D:displayname>
			      <D:resourcetype><D:collection/><C:calendar/></D:resourcetype>
			      <C:supported-calendar-component-set><C:comp name="VTODO"/></C:supported-calendar-component-set>
			  </D:prop></D:propstat></D:response>
			  <D:response><D:href>/dav/calendars/me/inbox/</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>
			    <D:prop><D:displayname>Inbox</D:displayname>
			      <D:resourcetype><D:collection/><C:schedule-inbox/></D:resourcetype>
			  </D:prop></D:propstat></D:response>
			</D:multistatus>`)
		}
	})

	got, err := c.Collections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("collections = %+v", got)
	}
	if got[0].Name != "Kalender" || got[0].Kind != "events" || got[0].Color != "#3355ffff" {
		t.Fatalf("calendar = %+v", got[0])
	}
	// A calendar that takes VTODO and not VEVENT is a task list, whatever it is
	// called.
	if got[1].Name != "Aufgaben" || got[1].Kind != "tasks" {
		t.Fatalf("task list = %+v", got[1])
	}
	// The href came back as a path and has to be usable as a URL afterwards.
	if !strings.HasPrefix(got[0].URL, "http://") || !strings.HasSuffix(got[0].URL, "/dav/calendars/me/kalender/") {
		t.Fatalf("url = %q", got[0].URL)
	}
	// A scheduling collection is not something anybody can be shown.
	for _, col := range got {
		if strings.Contains(col.URL, "inbox") {
			t.Fatalf("a scheduling collection was offered: %+v", col)
		}
	}
}

func TestSyncReturnsChangesDeletionsAndTheNextToken(t *testing.T) {
	var sent string
	c, base := server(t, func(w http.ResponseWriter, r *http.Request, body string) {
		sent = body
		if r.Method != "REPORT" {
			t.Errorf("method = %s", r.Method)
		}
		multiStatus(w, `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
		  <D:response><D:href>/dav/calendars/me/kalender/a.ics</D:href>
		    <D:propstat><D:status>HTTP/1.1 200 OK</D:status><D:prop>
		      <D:getetag>"1"</D:getetag>
		      <C:calendar-data>BEGIN:VCALENDAR
END:VCALENDAR
</C:calendar-data>
		    </D:prop></D:propstat></D:response>
		  <D:response><D:href>/dav/calendars/me/kalender/b.ics</D:href>
		    <D:status>HTTP/1.1 404 Not Found</D:status></D:response>
		  <D:sync-token>http://example.org/ns/sync/42</D:sync-token>
		</D:multistatus>`)
	})

	got, err := c.Sync(context.Background(), base+"/dav/calendars/me/kalender/", "http://example.org/ns/sync/41")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "<d:sync-token>http://example.org/ns/sync/41</d:sync-token>") {
		t.Fatalf("the request did not carry our token:\n%s", sent)
	}
	if !strings.Contains(sent, "calendar-data") {
		t.Fatal("the data has to be asked for in the same round trip")
	}
	if got.Token != "http://example.org/ns/sync/42" {
		t.Fatalf("token = %q", got.Token)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %+v", got.Items)
	}
	if got.Items[0].ETag != `"1"` || !strings.Contains(got.Items[0].Data, "VCALENDAR") {
		t.Fatalf("changed item = %+v", got.Items[0])
	}
	if !got.Items[1].Deleted {
		t.Fatalf("a 404 response is a deletion: %+v", got.Items[1])
	}
}

func TestAnUnknownTokenAsksToStartAgain(t *testing.T) {
	c, base := server(t, func(w http.ResponseWriter, r *http.Request, body string) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><D:error xmlns:D="DAV:"><D:valid-sync-token/></D:error>`)
	})
	_, err := c.Sync(context.Background(), base+"/dav/calendars/me/kalender/", "stale")
	if !errors.Is(err, davsync.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired: an expired token is a state, not a failure", err)
	}
}

func TestMultiGetAsksForTheObjectsItWasGiven(t *testing.T) {
	var sent string
	c, base := server(t, func(w http.ResponseWriter, r *http.Request, body string) {
		sent = body
		multiStatus(w, `<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
		  <D:response><D:href>/dav/calendars/me/kalender/a.ics</D:href>
		    <D:propstat><D:status>HTTP/1.1 200 OK</D:status><D:prop>
		      <D:getetag>"7"</D:getetag><C:calendar-data>BEGIN:VCALENDAR
END:VCALENDAR
</C:calendar-data>
		    </D:prop></D:propstat></D:response>
		</D:multistatus>`)
	})
	got, err := c.MultiGet(context.Background(), base+"/dav/calendars/me/kalender/",
		[]string{"/dav/calendars/me/kalender/a.ics"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "calendar-multiget") || !strings.Contains(sent, "a.ics") {
		t.Fatalf("request = %s", sent)
	}
	if len(got) != 1 || got[0].ETag != `"7"` || got[0].Data == "" {
		t.Fatalf("objects = %+v", got)
	}
}

func TestAServerErrorIsReportedWithWhatItSaid(t *testing.T) {
	c, base := server(t, func(w http.ResponseWriter, r *http.Request, body string) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "the database is on fire")
	})
	_, err := c.Sync(context.Background(), base+"/x/", "")
	if err == nil || !strings.Contains(err.Error(), "on fire") {
		t.Fatalf("err = %v", err)
	}
}
