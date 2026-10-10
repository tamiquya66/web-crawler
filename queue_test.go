package main

import "testing"

func TestSitePrefix(t *testing.T) {
	tests := []struct {
		start string
		want  string
	}{
		{"https://go.dev/", "https://go.dev/"},
		{"https://go.dev/doc/install", "https://go.dev/"},
		{"http://localhost:8080/page?x=1", "http://localhost:8080/"},
	}

	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			if got := sitePrefix(tt.start); got != tt.want {
				t.Errorf("sitePrefix(%q) = %q, want %q", tt.start, got, tt.want)
			}
		})
	}
}
