package main

import (
	"fmt"
	"os"
	"time"
	"context"
	"os/signal"
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

	saveBuffer = 100
	dbBatchSize = 50
	dbFlushInterval = 2 * time.Second
	dbTimeout = 10 * time.Second
)

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

	pool, err := connectDB(ctx)
	if err != nil {
		log.Fatalf("unnable to connect to the database: %v", err)
	}
	defer pool.Close()

	if err := ensureSchema(ctx, pool); err != nil {
		log.Fatalf("unnable to load tables: %v", err)
	}

	start := time.Now()
	crawl(ctx, pool, os.Args[1])
	fmt.Println("time of work:", time.Since(start).Round(time.Millisecond))
}