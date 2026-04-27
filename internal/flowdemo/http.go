package flowdemo

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

//go:embed static/index.html
var staticFiles embed.FS

func NewHandler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		content, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(content)
	})
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, service.ListRuns())
		case http.MethodPost:
			handleCreateRun(w, r, service)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		}
	})
	mux.HandleFunc("/api/runs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/runs/")
		run, ok := service.GetRun(id)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("run %q not found", id))
			return
		}
		writeJSON(w, http.StatusOK, run)
	})
	mux.HandleFunc("/api/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		runID := r.URL.Query().Get("run_id")
		uri := r.URL.Query().Get("uri")
		if runID == "" || uri == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("run_id and uri are required"))
			return
		}
		artifact, err := service.ReadArtifact(runID, uri)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, artifact)
	})
	mux.HandleFunc("/api/git", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		runID := r.URL.Query().Get("run_id")
		run, ok := service.GetRun(runID)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("run %q not found", runID))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"run_id":       run.ID,
			"run_root":     run.Root,
			"project_repo": run.ProjectRepo,
			"git_branch":   run.GitBranch,
			"git_commit":   run.GitCommit,
		})
	})
	return mux
}

func handleCreateRun(w http.ResponseWriter, r *http.Request, service *Service) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("file field is required: %w", err))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, err := service.Execute(r.Context(), header.Filename, content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
