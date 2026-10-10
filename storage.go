package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// default addr from docker-compose.yml
const defaultDatabaseURL = "postgres://crawler:crawler@localhost:5433/crawler"

func connectDB(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultDatabaseURL
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	return pool, nil
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS pages (
		url         TEXT PRIMARY KEY,
		depth       INT NOT NULL,
		status      INT,
		title       TEXT,
		error_text  TEXT,
		links_count INT NOT NULL DEFAULT 0,
		fetched_at  TIMESTAMPTZ NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS links (
		from_url TEXT NOT NULL,
		to_url   TEXT NOT NULL,
		PRIMARY KEY (from_url, to_url)
	)`,
}

// ensures that the table is created, if it's not
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	for _, stmt := range schema {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

type pageRecord struct {
	url       string
	depth     int
	status    int    // 0 - no response
	title     string // "" - no title
	errText   string // "" - no error
	links     []string
	fetchedAt time.Time
}

// update if exists and insert if not
// nullif transforms '0' and empty string into NULL
const upsertPageSQL = `
	INSERT INTO pages (url, depth, status, title, error_text, links_count, fetched_at)
	VALUES ($1, $2, NULLIF($3, 0), NULLIF($4, ''), NULLIF($5, ''), $6, $7)
	ON CONFLICT (url) DO UPDATE SET
		depth       = EXCLUDED.depth,
		status      = EXCLUDED.status,
		title       = EXCLUDED.title,
		error_text  = EXCLUDED.error_text,
		links_count = EXCLUDED.links_count,
		fetched_at  = EXCLUDED.fetched_at`

const deleteLinksSQL = `DELETE FROM links WHERE from_url = $1`
const insertLinksSQL = `INSERT INTO links (from_url , to_url) VALUES ($1, $2) ON CONFLICT DO NOTHING` 

// writer reads from saves and saves them into DB in groups
// closes done, when the channel saves is closed and everything is saved
func writer(pool *pgxpool.Pool, saves <-chan pageRecord, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(dbFlushInterval)
	defer ticker.Stop()

	var batch []pageRecord
	saved := 0

	flush := func() {
		if len(batch) == 0 {
			return
		}
		start := time.Now()
		if err := saveBatch(pool, batch); err != nil {
			log.Printf("database write error; pages lost: %d: %v", len(batch), err)
		} else {
			saved += len(batch)
			log.Printf("saved to db: %d for %v (total: %d)",
				len(batch), time.Since(start).Round(time.Millisecond), saved)
		}
		batch = batch[:0]
	}

	for {
		select {
		case rec, ok := <-saves:
			if !ok {
				// channel is closed: saving the remains and exit
				flush()
				log.Printf("writer ended work, total saved: %d", saved)
				return
			}
			batch = append(batch, rec)
			if len(batch) >= dbBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// saves a bunch of pages and their links in a single transaction
func saveBatch(pool *pgxpool.Pool, records []pageRecord) error {
	// giving it it's own context not related to the ctrl + c: crawler stop
	// so we are able to save all the remaining data
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	cmds := &pgx.Batch{}
	for _, p := range records {
		cmds.Queue(upsertPageSQL,
			p.url, p.depth, p.status, p.title, p.errText, len(p.links), p.fetchedAt)
		cmds.Queue(deleteLinksSQL, p.url)
		for _, link := range p.links {
			cmds.Queue(insertLinksSQL, p.url, link)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// after successful commit rollback doesn't make any returns
		// error "transaction is closed" we are consciously ignoring
		_ = tx.Rollback(ctx)
	}()

	if err := tx.SendBatch(ctx, cmds).Close(); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
