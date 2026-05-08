package doujiagit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"devflow/internal/core"
)

func NewDebugHandler(repository Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 5 || parts[0] != "debug" || parts[1] != "doujiagit" || parts[2] != "runs" {
			writeDebugError(w, http.StatusNotFound, "not found")
			return
		}
		runID := core.RunID(strings.TrimSpace(parts[3]))
		switch {
		case len(parts) == 5 && parts[4] == "ui":
			if r.Method != http.MethodGet {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeDebugHTML(w, debugUIHTML)
		case len(parts) == 5 && parts[4] == "graph":
			if r.Method != http.MethodGet {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			refName := strings.TrimSpace(r.URL.Query().Get("ref"))
			if refName == "" {
				refName = DefaultRefName
			}
			graph, err := BuildRunGraph(r.Context(), repository, runID, refName)
			writeDebugJSON(w, graph, err)
		case len(parts) == 5 && parts[4] == "frontiers":
			if r.Method != http.MethodGet {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			items, err := repository.ListFrontierSnapshotsByRun(r.Context(), runID)
			if err != nil {
				writeDebugJSON(w, nil, err)
				return
			}
			views := make([]FrontierSnapshotView, 0, len(items))
			for _, item := range items {
				views = append(views, frontierSnapshotView(item))
			}
			writeDebugJSON(w, views, nil)
		case len(parts) == 5 && parts[4] == "ref-moves":
			if r.Method != http.MethodGet {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			refName := strings.TrimSpace(r.URL.Query().Get("ref"))
			if refName == "" {
				refName = DefaultRefName
			}
			items, err := repository.ListRefMoveEvents(r.Context(), runID, refName)
			if err != nil {
				writeDebugJSON(w, nil, err)
				return
			}
			views := make([]RefMoveEventView, 0, len(items))
			for _, item := range items {
				views = append(views, refMoveEventView(item))
			}
			writeDebugJSON(w, views, nil)
		case len(parts) == 6 && parts[4] == "tasks":
			if r.Method != http.MethodGet {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			detail, err := BuildTaskSnapshotDetail(r.Context(), repository, runID, core.TaskID(strings.TrimSpace(parts[5])))
			writeDebugJSON(w, detail, err)
		case len(parts) == 5 && parts[4] == "refs":
			if r.Method != http.MethodGet {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			refs, err := repository.ListRefsByRun(r.Context(), runID)
			if err != nil {
				writeDebugJSON(w, nil, err)
				return
			}
			views := make([]RefView, 0, len(refs))
			for _, ref := range refs {
				views = append(views, refView(ref))
			}
			writeDebugJSON(w, views, nil)
		case len(parts) == 6 && parts[4] == "refs":
			if r.Method != http.MethodGet && r.Method != http.MethodPost {
				writeDebugError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			if r.Method == http.MethodPost {
				ref, err := updateDebugRef(r, repository, runID, strings.TrimSpace(parts[5]))
				writeDebugJSON(w, refView(ref), err)
				return
			}
			ref, err := repository.GetRef(r.Context(), runID, strings.TrimSpace(parts[5]))
			writeDebugJSON(w, refView(ref), err)
		default:
			writeDebugError(w, http.StatusNotFound, "not found")
		}
	})
}

type updateDebugRefRequest struct {
	FrontierSnapshotID string `json:"frontier_snapshot_id"`
	Reason             string `json:"reason"`
}

func updateDebugRef(r *http.Request, repository Repository, runID core.RunID, refName string) (Ref, error) {
	var req updateDebugRefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return Ref{}, err
	}
	frontierID := strings.TrimSpace(req.FrontierSnapshotID)
	frontier, err := repository.GetFrontierSnapshot(r.Context(), frontierID)
	if err != nil {
		return Ref{}, err
	}
	if frontier.RunID != runID {
		return Ref{}, fmt.Errorf("frontier snapshot %q does not belong to run %q", frontierID, runID)
	}
	var from []string
	if current, err := repository.GetRef(r.Context(), runID, refName); err == nil {
		if current.FrontierSnapshotID != "" {
			from = []string{current.FrontierSnapshotID}
		}
	}
	now := time.Now().UTC()
	expected := ""
	if len(from) > 0 {
		expected = from[0]
	}
	ref := Ref{
		RefName:                   refName,
		RunID:                     runID,
		FrontierSnapshotID:        frontier.FrontierSnapshotID,
		FrontierMemberSnapshotIDs: append([]string(nil), frontier.TaskSnapshotIDs...),
		UpdatedAt:                 now,
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "debug ui checkout"
	}
	ref, _, err = repository.MoveRef(r.Context(), MoveRefRequest{
		Ref:                        ref,
		ExpectedFrontierSnapshotID: expected,
		Event: RefMoveEvent{
			RunID:                   runID,
			RefName:                 ref.RefName,
			FromFrontierSnapshotIDs: from,
			ToFrontierSnapshotIDs:   []string{frontier.FrontierSnapshotID},
			Mode:                    RefMoveModeCheckout,
			Reason:                  reason,
			CreatedAt:               now,
		},
	})
	if err != nil {
		return Ref{}, err
	}
	return ref, nil
}

func writeDebugJSON(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		writeDebugError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

func writeDebugHTML(w http.ResponseWriter, payload string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(payload))
}

func writeDebugError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
