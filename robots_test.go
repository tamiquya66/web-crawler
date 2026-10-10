package main

import (
	"bufio"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseRobots(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "basic",
			input: "User-agent: *\nDisallow: /admin/\nDisallow: /search",
			want:  []string{"/admin/", "/search"},
		},
		{
			name:  "other agent ignored",
			input: "User-agent: Googlebot\nDisallow: /private/\n\nUser-agent: *\nDisallow: /tmp/",
			want:  []string{"/tmp/"},
		},
		{
			name:  "several agents in one group",
			input: "User-agent: Googlebot\nUser-agent: *\nDisallow: /tmp/",
			want:  []string{"/tmp/"},
		},
		{
			name:  "new group resets",
			input: "User-agent: *\nDisallow: /a/\nUser-agent: Googlebot\nDisallow: /b/",
			want:  []string{"/a/"},
		},
		{
			name:  "comments and case",
			input: "# rules\nUSER-AGENT: *   # everyone\ndisallow: /x/ # hidden",
			want:  []string{"/x/"},
		},
		{
			name:  "empty disallow",
			input: "User-agent: *\nDisallow:",
			want:  nil,
		},
		{
			name:  "windows line endings",
			input: "User-agent: *\r\nDisallow: /win/\r\n",
			want:  []string{"/win/"},
		},
		{
			name:  "empty file",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := parseRobots(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("parseRobots: unexpected error: %v", err)
			}
			if !slices.Equal(rules.disallow, tt.want) {
				t.Errorf("disallow = %q, want %q", rules.disallow, tt.want)
			}
		})
	}
}

func TestParseRobotsTooLongLine(t *testing.T) {
	// line is longer than limit buifo.Scanner (64kB)
	input := "User-agent: *\nDisallow: /" + strings.Repeat("a", 70*1024)

	rules, err := parseRobots(strings.NewReader(input))
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("error = %v, want bufio.ErrTooLong", err)
	}
	if len(rules.disallow) != 0 {
		t.Errorf("on error rules must be empty, got %q", rules.disallow)
	}
}

func TestRobotsAllowed(t *testing.T) {
	rules := &robotsRules{disallow: []string{"/admin/", "/search"}}

	tests := []struct {
		link string
		want bool
	}{
		{"https://go.dev/", true},
		{"https://go.dev/admin/", false},
		{"https://go.dev/admin/users", false},
		{"https://go.dev/administrator", true}, // "/admin/" with a slash does not match
		{"https://go.dev/search?q=go", false},  // the path is checked including parameters
		{"https://go.dev/searching", false},    // the rule is a prefix
		{"https://go.dev/doc/search", true},    // the prefix is ​​calculated from the beginning of the path
	}

	for _, tt := range tests {
		t.Run(tt.link, func(t *testing.T) {
			if got := rules.allowed(tt.link); got != tt.want {
				t.Errorf("allowed(%q) = %v, want %v", tt.link, got, tt.want)
			}
		})
	}
}