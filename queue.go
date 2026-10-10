package main

import (
	"context"
	"log"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
)

// returns the beginning of the site addresses, for example, "https://go.dev/"
// limiting the requests, so we won't grab tasks from the other site
func sitePrefix(startURL string) string {
	u, err := url.Parse(startURL)
	if err != nil {
		return startURL
	}
	return u.Scheme + "://" + u.Host + "/"
}

// prepares the site queue for launch: clears it if `fresh` is specified,
// returns undone tasks from the previous launch and adds up start page
func prepareQueue(ctx context.Context, pool *pgxpool.Pool, site, startURL string, fresh bool) error {
	if fresh {
		tag, err := pool.Exec(ctx,
			`DELETE FROM frontier WHERE starts_with(url, $1)`, site)
		if err != nil {
			return err
		}
		log.Printf("queue is clean, tasks deleted: %d", tag.RowsAffected())
	}

	// it's correct till crawler works in a single copy
	// for future: To support the operation of multiple crawlers it needs to be revised
	tag, err := pool.Exec(ctx,
		`UPDATE frontier SET status = 'pending', updated_at = now()
		WHERE status = 'in_progress' AND starts_with(url, $1)`, site)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Printf("returned undone tasks to queue: %d", n)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO frontier (url, depth) VALUES ($1, 0) ON CONFLICT (url) DO NOTHING`,
		startURL)
	return err
}

// claims a single portion of 'waiting tasks' and then tags them as 'claimed'
const claimTasksSQL = `
	UPDATE frontier
	SET status = 'in_progress', updated_at = now()
	WHERE id IN (
		SELECT id FROM frontier
		WHERE status = 'pending' AND starts_with(url, $1)
		ORDER BY depth, id
		LIMIT $2
	)
	RETURNING url, depth`

// retrieves up to `limit` site tasks from the queue
func claimTasks(ctx context.Context, pool *pgxpool.Pool, site string, limit int) ([]task, error) {
	rows, err := pool.Query(ctx, claimTasksSQL, site, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claimed []task
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.url, &t.depth); err != nil {
			return nil, err
		}
		claimed = append(claimed, t)
	}
	return claimed, rows.Err()
}

// adds all links, already existings - skips
const enqueueLinksSQL = `
	INSERT INTO frontier (url, depth)
	SELECT unnest($1::text[]), $2::int
	ON CONFLICT (url) DO NOTHING`

// places the links in queue and returns how many of them are new
// uses it's own context: on crawler's stop links won't loose
func enqueueLinks(pool *pgxpool.Pool, links []string, depth int) (int64, error) {
	if len(links) == 0 {
		return 0, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	tag, err := pool.Exec(ctx, enqueueLinksSQL, links, depth)
	if err != nil {
		return 0, nil
	}
	return tag.RowsAffected(), nil
}

// returns amount of tasks on "waiting" from the site
func countPending(pool *pgxpool.Pool, site string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var n int64
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM frontier WHERE status = 'pending' AND starts_with(url, $1)`,
		site).Scan(&n)
	return n, err
}
