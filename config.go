package main

import (
	"errors"
	"flag"
	"fmt"
	"time"
)

// start settings with flags from cmd
type config struct {
	startURL string
	maxDepth int
	maxPages int
	workers  int
	delay    time.Duration
	duration time.Duration
}

// parse flags and check whether they are correct
func parseConfig() (config, error) {
	var cfg config

	flag.IntVar(&cfg.maxDepth, "depth", 2, "maximum depth for crawling")
	flag.IntVar(&cfg.maxPages, "pages", 50, "maximum pages for crawling")
	flag.IntVar(&cfg.workers, "workers", 5, "maximum workers for crawling")
	flag.DurationVar(&cfg.delay, "delay", 500 * time.Millisecond, "pause between requests")	
	flag.DurationVar(&cfg.duration, "duration", 1 * time.Minute, "total work time")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "using: go run . [flags] <url>")
		fmt.Fprintln(flag.CommandLine.Output(), "flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 1 {
		return cfg, errors.New("there need to be a single <url> after flags")
	}

	startURL, err := normalizeLink(flag.Arg(0))
	if err != nil {
		return cfg, err
	}
	cfg.startURL = startURL

	switch {
	case cfg.maxDepth < 0:
		return cfg, errors.New("-depth не может быть отрицательным")
	case cfg.maxPages < 1:
		return cfg, errors.New("-pages должно быть не меньше 1")
	case cfg.workers < 1:
		return cfg, errors.New("-workers должно быть не меньше 1")
	case cfg.delay <= 0:
		return cfg, errors.New("-delay должна быть больше нуля")
	case cfg.duration <= 0:
		return cfg, errors.New("-duration должно быть больше нуля")
	}

	return cfg, nil
}