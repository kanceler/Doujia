package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
)

func TestBootstrapHTTPHandlerServesDoujiaGitDebugRoutes(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_app_http")
	seedAppHTTPGraph(t, ctx, bootstrap.Internals.DoujiaGitRepository, runID)

	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_app_http/graph", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"run_id": "run_app_http"`) || !strings.Contains(body, `"task_id": "task_01"`) {
		t.Fatalf("body missing debug graph content:\n%s", body)
	}
}

func TestBootstrapHTTPHandlerServesHealthz(t *testing.T) {
	handler := NewBootstrap(t.TempDir()).NewHTTPHandler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || strings.TrimSpace(resp.Body.String()) != "ok" {
		t.Fatalf("healthz status=%d body=%q", resp.Code, resp.Body.String())
	}
}

func seedAppHTTPGraph(t *testing.T, ctx context.Context, repository doujiagit.Repository, runID core.RunID) {
	t.Helper()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte("requirement")),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "projects/run_app_http/agents/ceo/artifacts/requirement/requirement_v1.md",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  "ceo",
		LogicalKey: "requirement",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	bagID := "bag_app_http"
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}
	snapshotID := "snapshot_app_http"
	if err := repository.CreateSnapshot(ctx, doujiagit.TaskSnapshot{
		SnapshotID:   snapshotID,
		RunID:        runID,
		TaskID:       "task_01",
		Result:       core.TaskResultCodeOK,
		OutputBagIDs: []string{bagID},
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if err := repository.UpdateRef(ctx, doujiagit.Ref{
		RefName:             doujiagit.DefaultRefName,
		RunID:               runID,
		FrontierSnapshotIDs: []string{snapshotID},
		UpdatedAt:           now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
}
