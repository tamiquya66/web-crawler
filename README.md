# web-crawler

## Overview
For a Go learning project, I decided to implement a web crawler that can be run locally.
It crawls a single website starting from a given URL, stays within the same host,
and saves the results to PostgreSQL.

## How it works
- **Fetching** — pages are downloaded with a shared HTTP client (request timeout, custom User-Agent, response size limit).
- **Parsing** — HTML is parsed with `golang.org/x/net/html`; the crawler extracts the page title and all `<a href>` links.
- **Link normalization** — relative links are resolved to absolute URLs, fragments are removed, links to other hosts and non-HTTP schemes are dropped.
- **Crawling** — breadth-first traversal with a queue and a "seen" set, limited by maximum depth and number of pages.
- **Concurrency** — a coordinator goroutine owns the queue and distributes tasks to a pool of workers through channels.
- **Politeness** — a shared rate limiter between requests and support for `robots.txt` (`Disallow` rules for `User-agent: *`).
- **Graceful shutdown** — the crawl stops on Ctrl+C or after a time limit (via `context`), finishing in-flight requests and saving collected data.
- **Storage** — a dedicated writer goroutine saves results to PostgreSQL in batches, inside transactions.

## Web archive
Results are stored in two tables:

 `pages:` URL, depth, HTTP status, title, error text, number of links, fetch time
 `links:` which page links to which (`from_url` → `to_url`)
