package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"devflow/internal/core"
	"devflow/internal/doujiagit"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "doujiagit-dump: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var dbPath string
	var runID string
	var refName string
	var taskID string
	var compact bool
	flag.StringVar(&dbPath, "db", "", "SQLite state database path")
	flag.StringVar(&runID, "run", "", "run id")
	flag.StringVar(&refName, "ref", doujiagit.DefaultRefName, "ref name")
	flag.StringVar(&taskID, "task", "", "optional task id; when set, dumps one task snapshot detail")
	flag.BoolVar(&compact, "compact", false, "emit compact JSON")
	flag.Parse()

	if dbPath == "" {
		return fmt.Errorf("-db is required")
	}
	if runID == "" {
		return fmt.Errorf("-run is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return err
	}

	repository := doujiagit.NewSQLiteRepository(db)
	if err := repository.Migrate(ctx); err != nil {
		return err
	}

	var payload any
	if taskID != "" {
		payload, err = doujiagit.BuildTaskSnapshotDetail(ctx, repository, core.RunID(runID), core.TaskID(taskID))
	} else {
		payload, err = doujiagit.BuildRunGraph(ctx, repository, core.RunID(runID), refName)
	}
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	if !compact {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(payload)
}
