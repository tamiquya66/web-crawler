package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var client = &http.Client{
	Timeout: requestTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Hostname() != via[0].URL.Hostname() {
			return http.ErrUseLastResponse // we don't go to the other domain
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

type pageData struct {
	status int
	title  string
	links  []string
}

func getPage(ctx context.Context, pageURL string) (pageData, error) {
	body, base, status, err := fetch(ctx, pageURL)
	if err != nil {
		return pageData{status: status}, err
	}

	title, hrefs, err := parsePage(body)
	if err != nil {
		return pageData{status: status}, err
	}

	return pageData{
		status: status,
		title:  title,
		links:  cleanLinks(base, hrefs),
	}, nil
}

// check whether the page is correct
// returns html and final addr
func fetch(ctx context.Context, pageURL string) ([]byte, *url.URL, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, resp.StatusCode, fmt.Errorf("bad status: %s", resp.Status)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		return nil, nil, resp.StatusCode, fmt.Errorf("it's not a html, it's a %s", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, nil, resp.StatusCode, err
	}

	return body, resp.Request.URL, resp.StatusCode, nil
}
