package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"context"
	"os/signal"
	"bufio"

	"golang.org/x/net/html"
	"log"
)

const (
	maxDepth = 2
	maxPages = 100
	workers  = 5
	requestTimeout = 10 * time.Second
	requestDelay = 500 * time.Millisecond
	maxBodySize = 5 * 1024 * 1024 // 5 megabytes
	userAgent = "LearningCrawler/0.1"
	maxDuration = 1 * time.Minute
	maxRobotsSize = 500 * 1024 // max robots.txt size
)

var client = &http.Client{Timeout: requestTimeout}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("using: go run . <url>")
		os.Exit(1)
	}

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()

	start := time.Now()
	crawl(ctx, os.Args[1])
	fmt.Println("time of work:", time.Since(start).Round(time.Millisecond))
}

type task struct {
	url   string
	depth int
}

type result struct {
	task   task
	links  []string
	err    error
	worker int
}

func worker(ctx context.Context, id int, tasks <-chan task, results chan<- result, limiter <- chan time.Time, wg *sync.WaitGroup) {
	defer wg.Done()
	for t := range tasks {
		select {
		case <- limiter:
		case <- ctx.Done():
		}

		links, err := getLinks(ctx, t.url)
		results <- result{task: t, links: links, err: err, worker: id}
	}
}

func crawl(ctx context.Context, startURL string) {
	robots, err := fetchRobots(ctx, startURL)
	if err != nil {
		log.Printf("robots.txt isn't downloaded, so let it be so: %v", err)
	}
	log.Printf("number of prohibitions in robots.txt: %d", len(robots.disallow))

	if !robots.allowed(startURL) {
		log.Printf("start URL is not allowed in robots.txt: %s", startURL)
		return
	}

	tasks := make(chan task)
	results := make(chan result)
	ticker := time.NewTicker(requestDelay)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go worker(ctx, i, tasks, results, ticker.C, &wg)
	}

	seen := map[string]bool{startURL: true}
	queue := []task{{url: startURL, depth: 0}}
	pages := 0
	inFlight := 0

	done := ctx.Done() // cancelation channel
	stopping := false // "stop" mode

	for (!stopping && len(queue) > 0 && pages < maxPages) || inFlight > 0 {
		var sendCh chan task
		var next task
		if !stopping && len(queue) > 0 && pages < maxPages {
			sendCh = tasks
			next = queue[0]
		}

		select {
		case sendCh <- next:
			queue = queue[1:]
			inFlight++
			pages++

		case <- done:
			stopping = true
			done = nil
			log.Printf("stopping (%v), waiting for other requests to finish: %d", ctx.Err(), inFlight)

		case r := <-results:
			inFlight--
			t := r.task

			if r.err != nil {
				if ctx.Err() != nil {
					log.Printf("[w%d][%d] interrupted: %s", r.worker, t.depth, t.url)
				} else {
					log.Printf("[w%d][%d] error: %s: %v", r.worker, t.depth, t.url, r.err)
				}
				continue
			}

			if t.depth >= maxDepth {
				log.Printf("[w%d][%d] %s (links: %d, max depth)",
					r.worker, t.depth, t.url, len(r.links))
				continue
			}

			added := 0
			blocked := 0
			for _, link := range r.links {
				if seen[link] {
					continue
				}
				seen[link] = true
				if !robots.allowed(link){
					blocked++
					continue
				}
				queue = append(queue, task{url: link, depth: t.depth + 1})
				added++
			}

			log.Printf("[w%d][%d] %s (links: %d, new: %d, prohibited: %d, in queue: %d, in work: %d)",
				r.worker, t.depth, t.url, len(r.links), added, blocked,len(queue), inFlight)
		}
	}
	close(tasks)
	wg.Wait()

	fmt.Println("downloaded pages:", pages)
	fmt.Println("left in queue:", len(queue))

	switch ctx.Err() {
	case context.DeadlineExceeded:
		fmt.Println("stopping reason: execution time expired")
	case context.Canceled:
		fmt.Println("stopping reason: interrupted (Ctrl+C)")
	default:
		if len(queue) == 0 {
			fmt.Println("stopping reason: covered everything possible")
		} else {
			fmt.Println("stopping reason: reached page limit")
		}
	}
}

func getLinks(ctx context.Context, pageURL string) ([]string, error) {
	body, base, err := fetch(ctx, pageURL)
	if err != nil {
		return nil, err
	}

	hrefs, err := extractLinks(body)
	if err != nil {
		return nil, err
	}

	return cleanLinks(base, hrefs), nil
}

// check whether the page is correct
// returns html and final addr
func fetch(ctx context.Context, pageURL string) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		return nil, nil, fmt.Errorf("it's not a html, it's a %s", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, nil, err
	}

	return body, resp.Request.URL, nil
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

// collect everything together
// transforms bytes into reader, then shifts it to the parser
// parses html
func extractLinks(body []byte) ([]string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return collectLinks(doc, nil), nil
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

	return abs.String(), true
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