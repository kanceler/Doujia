package doujiagit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devflow/internal/core"
)

func TestDebugHandlerServesRunGraphAndTaskDetail(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	runID := core.RunID("run_debug_http")
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	versionID := seedDebugHTTPGraph(t, ctx, repository, runID, now)

	handler := NewDebugHandler(repository)
	uiReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/ui", nil)
	uiResp := httptest.NewRecorder()
	handler.ServeHTTP(uiResp, uiReq)
	if uiResp.Code != http.StatusOK {
		t.Fatalf("ui status = %d body=%s", uiResp.Code, uiResp.Body.String())
	}
	if contentType := uiResp.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("ui content-type = %q, want text/html", contentType)
	}
	if body := uiResp.Body.String(); !strings.Contains(body, "DoujiaGit Run Viewer") || !strings.Contains(body, "/graph?ref=") {
		t.Fatalf("ui body missing expected content:\n%s", body)
	}
	if body := uiResp.Body.String(); !strings.Contains(body, "architect_create_container") || !strings.Contains(body, "create_container") {
		t.Fatalf("ui body missing create_container recognition:\n%s", body)
	}
	if body := uiResp.Body.String(); !strings.Contains(body, "交付流程总览") ||
		!strings.Contains(body, "模块并行视图") ||
		!strings.Contains(body, "Recover 事件") {
		t.Fatalf("ui body missing human-readable panels:\n%s", body)
	}

	graphReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/graph", nil)
	graphResp := httptest.NewRecorder()
	handler.ServeHTTP(graphResp, graphReq)
	if graphResp.Code != http.StatusOK {
		t.Fatalf("graph status = %d body=%s", graphResp.Code, graphResp.Body.String())
	}
	if body := graphResp.Body.String(); !strings.Contains(body, `"frontier_snapshots"`) ||
		!strings.Contains(body, `"refs"`) ||
		!strings.Contains(body, `"ref_move_events"`) ||
		!strings.Contains(body, `"processing_decisions"`) ||
		!strings.Contains(body, `"status": "advanced"`) ||
		!strings.Contains(body, `"produced_task_ids":`) ||
		!strings.Contains(body, `"frontier_snapshot_id": "frontier_debug_http"`) ||
		!strings.Contains(body, `"frontier_member_snapshot_ids":`) ||
		!strings.Contains(body, `"snapshot_version_id": "snapshot_debug_http:v1"`) ||
		!strings.Contains(body, versionID) {
		t.Fatalf("graph body missing expected content:\n%s", body)
	}

	refReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/refs/main", nil)
	refResp := httptest.NewRecorder()
	handler.ServeHTTP(refResp, refReq)
	if refResp.Code != http.StatusOK {
		t.Fatalf("ref status = %d body=%s", refResp.Code, refResp.Body.String())
	}
	if body := refResp.Body.String(); !strings.Contains(body, `"frontier_snapshot_id": "frontier_debug_http"`) ||
		!strings.Contains(body, `"frontier_member_snapshot_ids":`) {
		t.Fatalf("ref body missing current frontier:\n%s", body)
	}

	refsReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/refs", nil)
	refsResp := httptest.NewRecorder()
	handler.ServeHTTP(refsResp, refsReq)
	if refsResp.Code != http.StatusOK {
		t.Fatalf("refs status = %d body=%s", refsResp.Code, refsResp.Body.String())
	}
	if body := refsResp.Body.String(); !strings.Contains(body, `"ref_name": "main"`) {
		t.Fatalf("refs body missing main ref:\n%s", body)
	}

	frontiersReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/frontiers", nil)
	frontiersResp := httptest.NewRecorder()
	handler.ServeHTTP(frontiersResp, frontiersReq)
	if frontiersResp.Code != http.StatusOK {
		t.Fatalf("frontiers status = %d body=%s", frontiersResp.Code, frontiersResp.Body.String())
	}
	if body := frontiersResp.Body.String(); !strings.Contains(body, `"frontier_snapshot_id": "frontier_debug_http"`) ||
		!strings.Contains(body, `"details_json": "{\"source\":\"debug-test\"}"`) {
		t.Fatalf("frontiers body missing expected content:\n%s", body)
	}

	movesReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/ref-moves", nil)
	movesResp := httptest.NewRecorder()
	handler.ServeHTTP(movesResp, movesReq)
	if movesResp.Code != http.StatusOK {
		t.Fatalf("ref moves status = %d body=%s", movesResp.Code, movesResp.Body.String())
	}
	if body := movesResp.Body.String(); !strings.Contains(body, `"event_id": "refmove_debug_http"`) ||
		!strings.Contains(body, `"mode": "advance"`) {
		t.Fatalf("ref moves body missing expected content:\n%s", body)
	}

	checkoutReq := httptest.NewRequest(http.MethodPost, "/debug/doujiagit/runs/run_debug_http/refs/main", strings.NewReader(`{"frontier_snapshot_id":"frontier_debug_http","reason":"test checkout"}`))
	checkoutReq.Header.Set("Content-Type", "application/json")
	checkoutResp := httptest.NewRecorder()
	handler.ServeHTTP(checkoutResp, checkoutReq)
	if checkoutResp.Code != http.StatusOK {
		t.Fatalf("checkout status = %d body=%s", checkoutResp.Code, checkoutResp.Body.String())
	}
	if body := checkoutResp.Body.String(); !strings.Contains(body, `"frontier_snapshot_id": "frontier_debug_http"`) {
		t.Fatalf("checkout body missing current frontier:\n%s", body)
	}

	movesAfterCheckoutReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/ref-moves", nil)
	movesAfterCheckoutResp := httptest.NewRecorder()
	handler.ServeHTTP(movesAfterCheckoutResp, movesAfterCheckoutReq)
	if movesAfterCheckoutResp.Code != http.StatusOK {
		t.Fatalf("ref moves after checkout status = %d body=%s", movesAfterCheckoutResp.Code, movesAfterCheckoutResp.Body.String())
	}
	if body := movesAfterCheckoutResp.Body.String(); !strings.Contains(body, `"mode": "checkout"`) ||
		!strings.Contains(body, `"reason": "test checkout"`) {
		t.Fatalf("ref moves after checkout missing checkout event:\n%s", body)
	}

	taskReq := httptest.NewRequest(http.MethodGet, "/debug/doujiagit/runs/run_debug_http/tasks/task_01", nil)
	taskResp := httptest.NewRecorder()
	handler.ServeHTTP(taskResp, taskReq)
	if taskResp.Code != http.StatusOK {
		t.Fatalf("task status = %d body=%s", taskResp.Code, taskResp.Body.String())
	}
	if body := taskResp.Body.String(); !strings.Contains(body, `"task_id": "task_01"`) || !strings.Contains(body, `"output_bags"`) {
		t.Fatalf("task body missing expected content:\n%s", body)
	}
}

func seedDebugHTTPGraph(t *testing.T, ctx context.Context, repository Repository, runID core.RunID, now time.Time) string {
	t.Helper()
	object, err := repository.UpsertObject(ctx, ArtifactObject{
		ObjectID:   StableObjectID([]byte("requirement")),
		ObjectType: ObjectTypeBlob,
		StorageURI: "projects/run_debug_http/agents/ceo/artifacts/requirement/requirement_v1.md",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, LogicalArtifact{
		RunID:      runID,
		Namespace:  "ceo",
		LogicalKey: "requirement",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	bagID := "bag_debug_http"
	if err := repository.CreateBag(ctx, ArtifactBag{
		BagID:              bagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}
	snapshotID := "snapshot_debug_http"
	if err := repository.CreateSnapshot(ctx, TaskSnapshot{
		SnapshotID:        snapshotID,
		RunID:             runID,
		TaskID:            "task_01",
		LogicalSnapshotID: "logical_task_01",
		SnapshotVersionID: "snapshot_debug_http:v1",
		SnapshotVersionNo: 1,
		ArrivalKind:       RefMoveModeAdvance,
		Result:            core.TaskResultCodeOK,
		OutputBagIDs:      []string{bagID},
		CreatedAt:         now,
	}); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	frontierID := "frontier_debug_http"
	if err := repository.CreateFrontierSnapshot(ctx, FrontierSnapshot{
		FrontierSnapshotID: frontierID,
		RunID:              runID,
		TaskSnapshotIDs:    []string{snapshotID},
		CreatedByMode:      RefMoveModeAdvance,
		CreatedByEventID:   "refmove_debug_http",
		DetailsJSON:        `{"source":"debug-test"}`,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := repository.UpdateRef(ctx, Ref{
		RefName:                   DefaultRefName,
		RunID:                     runID,
		FrontierSnapshotID:        frontierID,
		FrontierMemberSnapshotIDs: []string{snapshotID},
		UpdatedAt:                 now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	if err := repository.CreateRefMoveEvent(ctx, RefMoveEvent{
		EventID:               "refmove_debug_http",
		RunID:                 runID,
		RefName:               DefaultRefName,
		ToFrontierSnapshotIDs: []string{frontierID},
		Mode:                  RefMoveModeAdvance,
		Reason:                "debug test seed",
		DetailsJSON:           `{"source":"debug-test"}`,
		CreatedAt:             now,
	}); err != nil {
		t.Fatalf("CreateRefMoveEvent() error = %v", err)
	}
	if err := repository.CreateSnapshotProcessingDecision(ctx, SnapshotProcessingDecision{
		RunID:           runID,
		RefName:         DefaultRefName,
		SnapshotID:      snapshotID,
		Status:          SnapshotProcessingStatusAdvanced,
		DecisionKind:    RefMoveModeAdvance,
		ContinuationID:  "task_02",
		ProducedTaskIDs: []string{"task_02"},
		CreatedAt:       now.Add(time.Second),
		UpdatedAt:       now.Add(time.Second),
	}); err != nil {
		t.Fatalf("CreateSnapshotProcessingDecision() error = %v", err)
	}
	return version.ArtifactVersionID
}
