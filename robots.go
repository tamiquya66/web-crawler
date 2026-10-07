package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// to ban from robots.txt for all robots (User-agent: *)
type robotsRules struct {
	disallow []string
}

// is it allowed to visit the addr
func (r* robotsRules) allowed(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	path := u.RequestURI()
	for _, prefix := range r.disallow {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// fetchRobots downloads and fetches robots.txt from site
func fetchRobots(ctx context.Context, startURL string) (*robotsRules, error) {
	empty := &robotsRules{}

	start, err := url.Parse(startURL)
	if err != nil {
		return empty, err
	}
	robotsURL := start.Scheme + "://" + start.Host + "/robots.txt"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return empty, nil
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("bad status: %s", resp.Status)
	}

	return parseRobots(io.LimitReader(resp.Body, maxRobotsSize))
}

// reads robots.txt by line and collects disallows for User-agent: *
func parseRobots(r io.Reader) (*robotsRules, error) {
	rules := &robotsRules{}
	applies := false // does the current group belong to us
	lastWasUA := false // was the previous line also a User-agent

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		// cutting off everything thats after #
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// split by key: value
		key, value, ok := strings.Cut(line, "")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "user-agent":
			if !lastWasUA {
				applies = false // new group starts
			}
			if value == "*" {
				applies = true
			}
			lastWasUA = true
		case "disallow":
			lastWasUA = false
			if applies && value != "" {
				rules.disallow = append(rules.disallow, value)
			}
		default:
			lastWasUA = false
		}
	}
	if err := scanner.Err(); err != nil {
		return &robotsRules{}, err
	}

	return rules, nil
}