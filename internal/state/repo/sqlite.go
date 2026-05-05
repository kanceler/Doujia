package repo

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

func OpenSQLiteRepositories(ctx context.Context, dataSourceName string) (SQLRepositories, *sql.DB, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return SQLRepositories{}, nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return SQLRepositories{}, nil, err
	}
	repos := NewSQLRepositories(db)
	if err := repos.Store.Migrate(ctx); err != nil {
		_ = db.Close()
		return SQLRepositories{}, nil, err
	}
	return repos, db, nil
}
