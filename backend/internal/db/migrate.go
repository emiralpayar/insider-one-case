package db

import (
	"context"
	"database/sql"
	"embed"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, url string) error {
	return withGoose(url, func(sqlDB *sql.DB) error { return goose.UpContext(ctx, sqlDB, "migrations") })
}

// MigrateDownTo rolls back every migration newer than version (used by tests
// that check a migration's backfill).
func MigrateDownTo(ctx context.Context, url string, version int64) error {
	return withGoose(url, func(sqlDB *sql.DB) error { return goose.DownToContext(ctx, sqlDB, "migrations", version) })
}

func withGoose(url string, fn func(*sql.DB) error) error {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return fn(sqlDB)
}
