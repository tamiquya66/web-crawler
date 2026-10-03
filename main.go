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

	"golang.org/x/net/html"
)

const (
	maxDepth = 2
	maxPages = 100
	workers  = 5
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("using: go run . <url>")
		os.Exit(1)
	}
	start := time.Now()
	crawl(os.Args[1])
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

func worker(id int, tasks <-chan task, results chan<- result, wg *sync.WaitGroup) {
	defer wg.Done()
	for t := range tasks {
		links, err := getLinks(t.url)
		results <- result{task: t, links: links, err: err, worker: id}
	}
}

func crawl(startURL string) {
	tasks := make(chan task)
	results := make(chan result)

	var wg sync.WaitGroup
	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go worker(i, tasks, results, &wg)
	}

	seen := map[string]bool{startURL: true}
	queue := []task{{url: startURL, depth: 0}}
	pages := 0
	inFlight := 0

	for (len(queue) > 0 && pages < maxPages) || inFlight > 0 {
		var sendCh chan task
		var next task
		if len(queue) > 0 && pages < maxPages {
			sendCh = tasks
			next = queue[0]
		}

		select {
		case sendCh <- next:
			queue = queue[1:]
			inFlight++
			pages++

		case r := <-results:
			inFlight--
			t := r.task

			if r.err != nil {
				fmt.Printf("[w%d][%d] error: %s: %v\n", r.worker, t.depth, t.url, r.err)
				continue
			}

			if t.depth >= maxDepth {
				fmt.Printf("[w%d][%d] %s (links: %d, max depth)\n",
					r.worker, t.depth, t.url, len(r.links))
				continue
			}

			added := 0
			for _, link := range r.links {
				if seen[link] {
					continue
				}
				seen[link] = true
				queue = append(queue, task{url: link, depth: t.depth + 1})
				added++
			}

			fmt.Printf("[w%d][%d] %s (links: %d, new: %d, in queue: %d, in work: %d)\n",
				r.worker, t.depth, t.url, len(r.links), added, len(queue), inFlight)
		}
	}
	close(tasks)
	wg.Wait()

	fmt.Println("downloaded pages:", pages)
	fmt.Println("left in queue:", len(queue))
}

func getLinks(pageURL string) ([]string, error) {
	body, base, err := fetch(pageURL)
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
func fetch(pageURL string) ([]byte, *url.URL, error) {
	resp, err := http.Get(pageURL)
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

	body, err := io.ReadAll(resp.Body)
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
