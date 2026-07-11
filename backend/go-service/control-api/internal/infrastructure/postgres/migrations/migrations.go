package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var files embed.FS

type migration struct {
	version int64
	name    string
	query   string
}

func Run(ctx context.Context, pool *pgxpool.Pool) error {
	items, err := load()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version BIGINT PRIMARY KEY,
name TEXT NOT NULL,
applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(0x41464c4f57)); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(0x41464c4f57)) }()

	for _, item := range items {
		if err := apply(ctx, conn, item); err != nil {
			return err
		}
	}
	return nil
}

func apply(ctx context.Context, conn *pgxpool.Conn, item migration) error {
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", item.version).Scan(&exists); err != nil {
		return fmt.Errorf("check migration %d: %w", item.version, err)
	}
	if exists {
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", item.version, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, item.query); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", item.version, item.name, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations(version, name) VALUES ($1, $2)", item.version, item.name); err != nil {
		return fmt.Errorf("record migration %d: %w", item.version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %d: %w", item.version, err)
	}
	return nil
}

func load() ([]migration, error) {
	entries, err := fs.ReadDir(files, "sql")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	items := make([]migration, 0, len(entries))
	seen := make(map[int64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %d in %q and %q", version, previous, entry.Name())
		}
		query, err := files.ReadFile("sql/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		seen[version] = entry.Name()
		items = append(items, migration{version: version, name: entry.Name(), query: string(query)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	return items, nil
}
