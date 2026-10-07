package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type task struct {
	url   string
	depth int
}

type result struct {
	task   task
	page  pageData
	err    error
	worker int
}

// worker claims a task, waits for the limiter access, downloads the pages and then sends the results
func worker(ctx context.Context, id int, tasks <-chan task, results chan<- result, limiter <- chan time.Time, wg *sync.WaitGroup) {
	defer wg.Done()
	for t := range tasks {
		select {
		case <- limiter:
		case <- ctx.Done():
		}

		page, err := getPage(ctx, t.url)
		results <- result{task: t, page: page, err: err, worker: id}
	}
}

func crawl(ctx context.Context, pool *pgxpool.Pool, startURL string) {
	robots, err := fetchRobots(ctx, startURL)
	if err != nil {
		log.Printf("robots.txt isn't downloaded, so let it be so: %v", err)
	}
	log.Printf("number of prohibitions in robots.txt: %d", len(robots.disallow))

	if !robots.allowed(startURL) {
		log.Printf("start URL is not allowed in robots.txt: %s", startURL)
		return
	}

	saves := make(chan pageRecord, saveBuffer)
	writerDone := make(chan struct{})
	go writer(pool, saves, writerDone)

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
					// we are not saving interrupted pages: it's not a site's problem
					// we interrupted them
					log.Printf("[w%d][%d] interrupted: %s", r.worker, t.depth, t.url)
					continue
				}
				log.Printf("[w%d][%d] error: %s: %v", r.worker, t.depth, t.url, r.err)

				saves <- pageRecord{
					url: t.url,
					depth: t.depth,
					status: r.page.status,
					errText: r.err.Error(),
					fetchedAt: time.Now(),
				}
				continue
			}

			saves <- pageRecord{
				url: t.url,
				depth: t.depth,
				status: r.page.status,
				title: r.page.title,
				links: r.page.links,
				fetchedAt: time.Now(),
			}

			if t.depth >= maxDepth {
				log.Printf("[w%d][%d] %s (links: %d, max depth)",
					r.worker, t.depth, t.url, len(r.page.links))
				continue
			}

			added := 0
			blocked := 0
			for _, link := range r.page.links {
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
				r.worker, t.depth, t.url, len(r.page.links), added, blocked,len(queue), inFlight)
		}
	}
	close(tasks)
	wg.Wait()

	close(saves)
	<-writerDone

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