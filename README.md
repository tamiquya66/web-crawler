# web-crawler

![CI](https://github.com/tamiquya66/web-crawler/actions/workflows/ci.yml/badge.svg)

## Overview
For a Go learning project, I decided to implement a web crawler that can be run locally.
It crawls a single website starting from a given URL, stays within the same host,
and saves the results to PostgreSQL. The crawl state is stored in the database,
so an interrupted crawl can be resumed from where it stopped.

## How it works
- **Fetching** — pages are downloaded with a shared HTTP client (request timeout, custom User-Agent, response size limit). Redirects are followed only within the same host.
- **Parsing** — HTML is parsed with `golang.org/x/net/html`; the crawler extracts the page title and all `<a href>` links.
- **Link normalization** — relative links are resolved to absolute URLs, fragments are removed, an empty path becomes `/`, links to other hosts, non-HTTP schemes and downloadable files (archives, images, PDFs, etc.) are dropped. The start URL goes through the same normalization.
- **Crawling** — breadth-first traversal limited by maximum depth and number of pages per run. The queue (frontier) lives in PostgreSQL: tasks are claimed in small portions ordered by depth, duplicates are rejected by a unique constraint.
- **Resuming** — unfinished tasks are returned to the queue on start, so the next run continues where the previous one stopped. Each page is processed at least once.
- **Concurrency** — a coordinator goroutine distributes tasks to a pool of workers through channels and collects results using `select`.
- **Politeness** — a shared rate limiter between requests and support for `robots.txt` (`Disallow` rules for `User-agent: *`).
- **Graceful shutdown** — the crawl stops on Ctrl+C or after a time limit (via `context`), finishing in-flight requests and saving collected data.
- **Storage** — a dedicated writer goroutine saves results to PostgreSQL in batches: all commands of a batch are sent in one round trip with `pgx.Batch`, inside a single transaction.

## Web archive
Results are stored in three tables:

| table      | contents |
|------------|----------|
| `pages`    | URL, depth, HTTP status, title, error text, number of links, fetch time |
| `links`    | which page links to which (`from_url` → `to_url`) |
| `frontier` | crawl queue: URL, depth and status (`pending`, `in_progress`, `done`, `failed`) |

Example queries:

```sql
-- pages per depth
SELECT depth, count(*) FROM pages GROUP BY depth ORDER BY depth;

-- most linked pages
SELECT to_url, count(*) AS inbound FROM links
GROUP BY to_url ORDER BY inbound DESC LIMIT 10;

-- queue state
SELECT status, count(*) FROM frontier GROUP BY status;
```

## Getting started
Requirements: Go and Docker.

```bash
docker compose up -d                                  # start PostgreSQL
go run . https://go.dev/                              # start or resume crawling
go run . -fresh -depth 3 -pages 100 https://go.dev/   # start over with custom limits
```

Flags must come before the URL:

| flag        | default | description |
|-------------|---------|-------------|
| `-depth`    | `2`     | maximum crawl depth |
| `-pages`    | `30`    | maximum number of pages per run |
| `-workers`  | `5`     | number of concurrent workers |
| `-delay`    | `500ms` | pause between requests |
| `-duration` | `1m`    | overall time limit |
| `-fresh`    | off     | clear the site's queue and start over |

Run `go run . -h` to see all flags.
The database address can be overridden with the `DATABASE_URL` environment variable.

To explore the results:

```bash
docker compose exec db psql -U crawler -d crawler
```

## Tests
Unit tests cover link normalization, HTML parsing and `robots.txt` handling:

```bash
go test ./...
```

GitHub Actions runs formatting check (`gofmt`), `go vet` and tests with the race detector on every push to `main` and on pull requests.

## Project structure
| file | purpose |
|------|---------|
| `main.go` | entry point and constants |
| `config.go` | command-line flags |
| `crawler.go` | coordinator and workers |
| `fetch.go` | HTTP requests |
| `parse.go` | HTML parsing and link normalization |
| `robots.go` | `robots.txt` support |
| `queue.go` | crawl queue in PostgreSQL |
| `storage.go` | database connection, schema and writer |
