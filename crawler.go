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
	page   pageData
	err    error
	worker int
}

// worker claims a task, waits for the limiter access, downloads the pages and then sends the results
func worker(ctx context.Context, id int, tasks <-chan task, results chan<- result, limiter <-chan time.Time, wg *sync.WaitGroup) {
	defer wg.Done()
	for t := range tasks {
		select {
		case <-limiter:
		case <-ctx.Done():
		}

		page, err := getPage(ctx, t.url)
		results <- result{task: t, page: page, err: err, worker: id}
	}
}

func crawl(ctx context.Context, pool *pgxpool.Pool, cfg config) {
	startURL := cfg.startURL
	site := sitePrefix(startURL)

	robots, err := fetchRobots(ctx, startURL)
	if err != nil {
		log.Printf("robots.txt can't be loaded, stopping: %v", err)
		return
	}
	log.Printf("number of prohibitions in robots.txt: %d", len(robots.disallow))

	if !robots.allowed(startURL) {
		log.Printf("start URL is not allowed in robots.txt: %s", startURL)
		return
	}

	if err := prepareQueue(ctx, pool, site, startURL, cfg.fresh); err != nil {
		log.Printf("failed to prepare the queue: %v", err)
		return
	}
	if n, err := countPending(pool, site); err == nil {
		log.Printf("in queue on start: %d", n)
	}

	saves := make(chan pageRecord, saveBuffer)
	writerDone := make(chan struct{})
	go writer(pool, saves, writerDone)

	tasks := make(chan task)
	results := make(chan result)
	ticker := time.NewTicker(cfg.delay)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for i := 1; i <= cfg.workers; i++ {
		wg.Add(1)
		go worker(ctx, i, tasks, results, ticker.C, &wg)
	}

	var queue []task
	pages := 0
	inFlight := 0

	done := ctx.Done() // cancelation channel
	stopping := false  // "stop" mode

	for {
		if !stopping && len(queue) == 0 && pages < cfg.maxPages {
			claimed, err := claimTasks(ctx, pool, site, min(cfg.workers, cfg.maxPages-pages))
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("failed to claim tasks from database: %v", err)
				}
				stopping = true
			}
			queue = claimed
		}

		canSend := !stopping && len(queue) > 0
		if !canSend && inFlight == 0 {
			break
		}

		var sendCh chan task
		var next task
		if canSend {
			sendCh = tasks
			next = queue[0]
		}

		select {
		case sendCh <- next:
			queue = queue[1:]
			inFlight++
			pages++
		case <-done:
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
					// task remains 'in progress' and returns at the next launch
					log.Printf("[w%d][%d] interrupted: %s", r.worker, t.depth, t.url)
					continue
				}
				log.Printf("[w%d][%d] error: %s: %v", r.worker, t.depth, t.url, r.err)

				saves <- pageRecord{
					url:       t.url,
					depth:     t.depth,
					status:    r.page.status,
					errText:   r.err.Error(),
					fetchedAt: time.Now(),
				}
				continue
			}

			var added int64
			blocked := 0
			if t.depth < cfg.maxDepth {
				var allowed []string
				for _, link := range r.page.links {
					if robots.allowed(link) {
						allowed = append(allowed, link)
					} else {
						blocked++
					}
				}
				n, err := enqueueLinks(pool, allowed, t.depth+1)
				if err != nil {
					log.Printf("failed to place links in queue: %s: %v", t.url, err)
				}
				added = n
			}

			saves <- pageRecord{
				url:       t.url,
				depth:     t.depth,
				status:    r.page.status,
				title:     r.page.title,
				links:     r.page.links,
				fetchedAt: time.Now(),
			}

			log.Printf("[w%d][%d] %s (links: %d, new: %d, prohibited: %d, in work: %d)",
				r.worker, t.depth, t.url, len(r.page.links), added, blocked, inFlight)
		}
	}
	close(tasks)
	wg.Wait()

	close(saves)
	<-writerDone

	fmt.Println("downloaded pages at this launch:", pages)
	if n, err := countPending(pool, site); err == nil {
		fmt.Println("waiting in queue:", n)
	}

	switch ctx.Err() {
	case context.DeadlineExceeded:
		fmt.Println("stopping reason: execution time expired")
	case context.Canceled:
		fmt.Println("stopping reason: interrupted (Ctrl+C)")
	default:
		if pages >= cfg.maxPages {
			fmt.Println("stopping reason: page limit reached")
		} else {
			fmt.Println("stopping reason: covered everything possible")
		}
	}
}
