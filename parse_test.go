package main

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// parses the address or interrupts the test.
func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestResolveLink(t *testing.T) {
	base := mustParseURL(t, "https://go.dev/doc/tutorial/")

	tests := []struct {
		name   string
		href   string
		want   string
		wantOK bool
	}{
		{"root path", "/blog/", "https://go.dev/blog/", true},
		{"relative path", "getting-started", "https://go.dev/doc/tutorial/getting-started", true},
		{"parent dir", "../install", "https://go.dev/doc/install", true},
		{"protocol relative", "//go.dev/learn/", "https://go.dev/learn/", true},
		{"query only", "?page=2", "https://go.dev/doc/tutorial/?page=2", true},
		{"fragment only", "#top", "https://go.dev/doc/tutorial/", true},
		{"fragment removed", "../install#linux", "https://go.dev/doc/install", true},
		{"spaces trimmed", "  /blog/  ", "https://go.dev/blog/", true},
		{"empty path becomes slash", "https://go.dev", "https://go.dev/", true},
		{"other host", "https://github.com/golang/go", "", false},
		{"mailto", "mailto:hello@go.dev", "", false},
		{"javascript", "javascript:void(0)", "", false},
		{"archive skipped", "/dl/go1.22.0.linux-amd64.tar.gz", "", false},
		{"uppercase pdf skipped", "/files/Report.PDF", "", false},
		{"html extension kept", "/doc/page.html", "https://go.dev/doc/page.html", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveLink(base, tt.href)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("resolveLink(%q) = (%q, %v), want (%q, %v)",
					tt.href, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestNormalizeStartURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"adds slash", "https://go.dev", "https://go.dev/", false},
		{"keeps slash", "https://go.dev/", "https://go.dev/", false},
		{"removes fragment", "https://go.dev/doc#install", "https://go.dev/doc", false},
		{"trims spaces", "  https://go.dev/  ", "https://go.dev/", false},
		{"no scheme", "go.dev", "", true},
		{"wrong scheme", "ftp://go.dev/", "", true},
		{"no host", "https://", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeLink(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeStartURL(%q) error = %v, wanterr %v",
					tt.raw, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("normalizeStartURL(%q) = %q, want %q",
					tt.raw, got, tt.want)
			}
		})
	}
}

func TestFindTitle(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"simple", "<html><head><title>Go</title></head></html>", "Go"},
		{"whitespace collapsed", "<title>\n  The Go\n  Programming   Language\n</title>", "The Go Programming Language"},
		{"no title", "<html><body><p>text</p></body></html>", ""},
		{"empty title", "<title></title>", ""},
		{"first title wins", "<title>First</title><title>Second</title>", "First"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(tt.html))
			if err != nil {
				t.Fatalf("html.Parse: %v", err)
			}
			if got := findTitle(doc); got != tt.want {
				t.Errorf("findTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePage(t *testing.T) {
	body := `<html>
		<head><title>Test page</title><link href="/style.css"></head>
		<body>
			<a href="/a">A</a>
			<p>text <a href="/b">B</a></p>
			<a>no href</a>
		</body>
	</html>`

	title, hrefs, err := parsePage([]byte(body))
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}

	if title != "Test page" {
		t.Errorf("title = %q, want %q", title, "Test page")
	}

	want := []string{"/a", "/b"}
	if !slices.Equal(hrefs, want) {
		t.Errorf("hrefs = %v, want %v", hrefs, want)
	}
}
