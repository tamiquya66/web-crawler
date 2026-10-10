package main

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// parse html and return an html, title and all hrefs
func parsePage(body []byte) (string, []string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	return findTitle(doc), collectLinks(doc, nil), nil
}

// recursively finds first <title> tag and returns as string value
func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			// remove spaces and /n in the title
			return strings.Join(strings.Fields(n.FirstChild.Data), " ")
		}
		return ""
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if title := findTitle(c); title != "" {
			return title
		}
	}
	return ""
}

// recursively walk all the html tree section in order to find any <a> tag that contains href (url link)
func collectLinks(n *html.Node, links []string) []string {
	// check whether this node is an <a> tag
	if n.Type == html.ElementNode && n.Data == "a" {
		for _, attr := range n.Attr {
			if attr.Key == "href" {
				links = append(links, attr.Val)
			}
		}
	}

	// walk all the childs
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		links = collectLinks(c, links)
	}

	return links
}

// returns href transformed into full addr w/out fragments
// returns false if the link is invalid
func resolveLink(base *url.URL, href string) (string, bool) {
	// resolving the link
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", false
	}
	// transform into full addr
	abs := base.ResolveReference(ref)

	// only web-pages
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return "", false
	}

	if abs.Hostname() != base.Hostname() {
		return "", false
	}

	abs.Fragment = ""

	if abs.Path == "" {
		abs.Path = "/"
	}

	return abs.String(), true
}

// checks the startURL and normalizing it as other links
func normalizeLink(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("need full address: https://example.com, received: %q", raw)
	}

	link, ok := resolveLink(u, raw)
	if !ok {
		return "", fmt.Errorf("not able to resolve link: %q", raw)
	}
	return link, nil
}

// handles all the links and gets rid of the duplicates
func cleanLinks(base *url.URL, hrefs []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, href := range hrefs {
		link, ok := resolveLink(base, href)
		if !ok {
			continue
		}
		if seen[link] {
			continue
		}
		seen[link] = true
		result = append(result, link)
	}
	return result
}
