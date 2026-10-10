package main

import (
	"fmt"
	"os"
	"time"
	"context"
	"os/signal"
	"log"
	"flag"
)

const (
	requestTimeout = 10 * time.Second
	maxBodySize = 5 * 1024 * 1024 // 5 megabytes
	userAgent = "LearningCrawler/0.1"
	maxRobotsSize = 500 * 1024 // max robots.txt size

	saveBuffer = 100
	dbBatchSize = 50
	dbFlushInterval = 2 * time.Second
	dbTimeout = 10 * time.Second
)

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		flag.Usage()
		os.Exit(2)
	}

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	log.Printf("start: %s, depth %d, pages %d, workers %d, pause %v, duration %v",
		cfg.startURL, cfg.maxDepth, cfg.maxPages, cfg.workers, cfg.delay, cfg.duration)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	pool, err := connectDB(ctx)
	if err != nil {
		log.Fatalf("unnable to connect to the database: %v", err)
	}
	defer pool.Close()

	if err := ensureSchema(ctx, pool); err != nil {
		log.Fatalf("unnable to load tables: %v", err)
	}

	start := time.Now()
	crawl(ctx, pool, cfg)
	fmt.Println("time of work:", time.Since(start).Round(time.Millisecond))
}