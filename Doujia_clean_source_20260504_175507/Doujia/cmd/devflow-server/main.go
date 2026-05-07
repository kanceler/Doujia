package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"devflow/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "devflow-server: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var addr string
	var dbPath string
	var projectsRoot string
	var pipelineRegistryPath string
	var agentMode string
	var agentPluginRoots string
	flag.StringVar(&addr, "addr", "127.0.0.1:18080", "HTTP listen address")
	flag.StringVar(&dbPath, "db", filepath.Join("runtime", "devflow", "state.db"), "SQLite state database path")
	flag.StringVar(&projectsRoot, "projects", filepath.Join("runtime", "workspaces"), "projects/workspaces root")
	flag.StringVar(&pipelineRegistryPath, "pipeline-registry", "", "optional PipelineSpec JSON registry path")
	flag.StringVar(&agentMode, "agent-mode", "", "agent mode: empty/default/protocolmock or real")
	flag.StringVar(&agentPluginRoots, "agent-plugin-roots", "", "optional real-agent plugin root directories separated by the OS path-list separator")
	flag.Parse()

	bootstrap, db, err := app.NewSQLiteBootstrapWithOptions(ctx, filepath.Clean(projectsRoot), filepath.Clean(dbPath), app.BootstrapOptions{
		PipelineRegistryPath: pipelineRegistryPath,
		AgentMode:            agentMode,
		AgentPluginRoots:     filepath.SplitList(agentPluginRoots),
	})
	if err != nil {
		return err
	}
	defer db.Close()

	server := &http.Server{
		Addr:              addr,
		Handler:           bootstrap.NewHTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("DevFlow HTTP server listening on http://%s\n", addr)
		fmt.Printf("DoujiaGit debug UI: http://%s/debug/doujiagit/runs/{run_id}/ui\n", addr)
		fmt.Printf("DoujiaGit debug graph: http://%s/debug/doujiagit/runs/{run_id}/graph\n", addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
