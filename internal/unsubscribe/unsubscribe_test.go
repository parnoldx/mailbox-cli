package unsubscribe

import "testing"

func TestOf(t *testing.T) {
	cases := []struct {
		name                                 string
		listUnsubscribe, listUnsubPost, html string
		want                                 Kind
		wantURL, wantTo, wantSubj, wantBody  string
	}{
		{
			name:            "one-click",
			listUnsubscribe: "<https://list.example/u/1>, <mailto:leave@example.com>",
			listUnsubPost:   "List-Unsubscribe=One-Click",
			want:            OneClick, wantURL: "https://list.example/u/1",
		},
		{
			name:            "link only, no one-click header",
			listUnsubscribe: "<https://list.example/u/1>",
			want:            Link, wantURL: "https://list.example/u/1",
		},
		{
			name:            "mailto only",
			listUnsubscribe: "<mailto:leave@example.com?subject=unsubscribe&body=please%20remove%20me>",
			want:            Email, wantTo: "leave@example.com", wantSubj: "unsubscribe",
			wantBody: "please remove me",
		},
		{
			name: "body link, English",
			html: `<p>Not interested? <a href="https://x.example/opt-out?id=9">Unsubscribe</a></p>`,
			want: Link, wantURL: "https://x.example/opt-out?id=9",
		},
		{
			name: "body link, German",
			html: `<a href="https://x.example/abm?id=9">Hier abmelden</a>`,
			want: Link, wantURL: "https://x.example/abm?id=9",
		},
		{
			name: "nothing to go on",
			html: `<a href="https://x.example/other">Read more</a>`,
			want: None,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Of(c.listUnsubscribe, c.listUnsubPost, c.html)
			if got.Kind != c.want {
				t.Fatalf("kind = %q, want %q", got.Kind, c.want)
			}
			if got.URL != c.wantURL {
				t.Fatalf("url = %q, want %q", got.URL, c.wantURL)
			}
			if got.To != c.wantTo {
				t.Fatalf("to = %q, want %q", got.To, c.wantTo)
			}
			if got.Subject != c.wantSubj {
				t.Fatalf("subject = %q, want %q", got.Subject, c.wantSubj)
			}
			if got.Body != c.wantBody {
				t.Fatalf("body = %q, want %q", got.Body, c.wantBody)
			}
		})
	}
}
