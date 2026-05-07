package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentbootstrap "devflow/internal/agent/bootstrap"
	"devflow/internal/agent/llm"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/state/repo"
)

type fakeDoujiaLLM struct {
	response     string
	streamChunks []string
	err          error
	streamErr    error
	lastReq      *llm.ChatRequest
}

func (f fakeDoujiaLLM) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if f.lastReq != nil {
		copied := req
		copied.Messages = append([]llm.Message(nil), req.Messages...)
		copied.Tools = append([]llm.Tool(nil), req.Tools...)
		*f.lastReq = copied
	}
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: f.response,
		},
	}, nil
}

func (f fakeDoujiaLLM) StreamChat(_ context.Context, req llm.ChatRequest, onDelta func(string) error) (llm.ChatResponse, error) {
	if f.lastReq != nil {
		copied := req
		copied.Messages = append([]llm.Message(nil), req.Messages...)
		copied.Tools = append([]llm.Tool(nil), req.Tools...)
		*f.lastReq = copied
	}
	if f.streamErr != nil {
		return llm.ChatResponse{}, f.streamErr
	}
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	chunks := f.streamChunks
	if len(chunks) == 0 {
		chunks = []string{f.response}
	}
	var full strings.Builder
	for _, chunk := range chunks {
		full.WriteString(chunk)
		if err := onDelta(chunk); err != nil {
			return llm.ChatResponse{}, err
		}
	}
	return llm.ChatResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: full.String(),
		},
	}, nil
}

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

func TestBootstrapHTTPHandlerServesAPIHealthAndCORSPreflight(t *testing.T) {
	handler := NewBootstrap(t.TempDir()).NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/health status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /api/health response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health body = %#v, want status ok", body)
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/runs", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/runs status=%d body=%s", resp.Code, resp.Body.String())
	}
	if methods := resp.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") {
		t.Fatalf("Access-Control-Allow-Methods = %q, want GET and POST", methods)
	}
}

func TestBootstrapHTTPHandlerServesRunAPIViews(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_api_http")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPGraph(t, ctx, bootstrap.Internals.DoujiaGitRepository, runID)
	seedAppHTTPWorkspace(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/api/runs", want: `"run_id":"run_api_http"`},
		{path: "/api/demo-runs", want: `"run_id":"run_api_http"`},
		{path: "/api/runs/run_api_http", want: `"pipeline_id":"phase_two_delivery_flow"`},
		{path: "/api/runs/run_api_http/tasks", want: `"task_id":"task_01"`},
		{path: "/api/runs/run_api_http/events", want: `"event_id":"event_api_http"`},
		{path: "/api/runs/run_api_http/pipeline-graph?ref=main", want: `"snapshot_id":"snapshot_app_http"`},
		{path: "/api/runs/run_api_http/nodes/task_api_http", want: `"task_id":"task_api_http"`},
		{path: "/api/runs/run_api_http/nodes/task_api_http", want: `"events":[{"event_id":"event_api_http"`},
		{path: "/api/runs/run_api_http/nodes/task_api_http", want: `"uri":"projects/run_api_http/agents/ceo/artifacts/requirement/requirement_v1.md"`},
		{path: "/api/runs/run_api_http/pipeline-workspace", want: `"main_pipeline_nodes"`},
		{path: "/api/runs/run_api_http/pipeline-workspace", want: `"node_id":"task:root__pm_write_plan"`},
		{path: "/api/runs/run_api_http/pipeline-workspace", want: `"node_id":"fork:implementation"`},
		{path: "/api/runs/run_api_http/pipeline-workspace", want: `"node_kind":"child_pipeline"`},
		{path: "/api/runs/run_api_http/pipeline-workspace", want: `"node_id":"task:root__acceptance"`},
		{path: "/api/runs/run_api_http/pipeline-workspace", want: `"kind":"fork"`},
		{path: "/api/runs/run_api_http/pipeline-workspace?focus_instance_id=root_test_all_modules_module01", want: `"focus_instance_id":"root_test_all_modules_module01"`},
		{path: "/api/runs/run_api_http/pipeline-workspace?focus_instance_id=root_test_all_modules_module01", want: `"pipeline_id":"pipeline_module"`},
		{path: "/api/runs/run_api_http/pipeline-workspace?focus_instance_id=root_test_all_modules_module01", want: `"node_kind":"task"`},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", tc.path, resp.Code, resp.Body.String())
		}
		if body := resp.Body.String(); !strings.Contains(body, tc.want) {
			t.Fatalf("GET %s body missing %s:\n%s", tc.path, tc.want, body)
		}
	}
}

func TestBootstrapHTTPHandlerServesEmptyPipelineGraphForRunWithoutRef(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_api_empty_graph")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_api_empty_graph/pipeline-graph?ref=main", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET empty graph status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{`"run_id":"run_api_empty_graph"`, `"ref_name":"main"`, `"snapshots":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET empty graph body missing %s:\n%s", want, body)
		}
	}
}

func TestBootstrapHTTPHandlerServesEmptyGitBranchesForRunWithoutArtifacts(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_git_branches_empty")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_git_branches_empty/git-branches", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET git branches status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{`"run_id":"run_git_branches_empty"`, `"module_branches":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET empty git branches body missing %s:\n%s", want, body)
		}
	}
}

func TestBootstrapHTTPHandlerServesRealGitBranchesFromDoujiaGitBags(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_git_branches_real")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:        "task_write_code",
		RunID:     runID,
		StageID:   "write_code",
		AgentRole: core.AgentRoleCoder,
		AgentID:   "coder01",
		Op:        core.TaskOpWriteCode,
		Status:    core.TaskStatusDone,
		Result:    core.TaskResultCodeOK,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create write task: %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:        "task_merge_code",
		RunID:     runID,
		StageID:   "merge_code",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpMergeCode,
		Status:    core.TaskStatusDone,
		Result:    core.TaskResultCodeOK,
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create merge task: %v", err)
	}
	seedAppHTTPGitBranches(t, ctx, bootstrap, runID)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_git_branches_real/git-branches", nil)
	resp := httptest.NewRecorder()
	bootstrap.NewHTTPHandler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET git branches status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"branch":"feature/module01-frontend"`,
		`"commit":"commitmodule01"`,
		`"module_id":"module01"`,
		`"module_name":"Snake Game Frontend"`,
		`"container_id":"container-real-01"`,
		`"merged_commit":"mergedcommit123"`,
		`"applied_commits":["commitmodule01"]`,
		`"bag_id":"bag:`,
		`"logical_key":"coder_branch"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET git branches body missing %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "meilee") || strings.Contains(body, "coder-bot") || strings.Contains(body, "fix: issue") {
		t.Fatalf("GET git branches body contains screenshot/mock data:\n%s", body)
	}
}

func TestBootstrapHTTPHandlerOpensFinalResultPreviewURL(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_final_result_preview")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_final_result_preview/preview/handlers/final_result_open", strings.NewReader(`{"args":{"bag_id":"bag:final","container_id":"container-real-01","preview_url":"http://127.0.0.1:5173/"}}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST final_result_open status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"status":"ready"`,
		`"preview_url":"http://127.0.0.1:5173/"`,
		`"container_id":"container-real-01"`,
		`"bag_id":"bag:final"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("POST final_result_open body missing %s:\n%s", want, body)
		}
	}
}

func TestBootstrapHTTPHandlerResolvesFinalResultContainerFromBag(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_final_result_bag")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	store := &artifact.ScopedLocalStore{RunRoot: run.ProjectDir, WorkspaceRoot: run.ProjectDir}
	uri := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "architect01", "artifacts", "container", "container_context.json"))
	content := []byte(`{"container_id":"container-from-bag","repo_dir":"/workspace/repo"}`)
	if err := store.Write(ctx, uri, content); err != nil {
		t.Fatalf("write container artifact: %v", err)
	}
	repository := bootstrap.Internals.DoujiaGitRepository
	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID(content),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: uri,
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  "architect01",
		LogicalKey: "container_context",
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
	bagID := doujiagit.StableBagID(runID, "snapshot_final_result", "container_context")
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}

	containerID, err := bootstrap.findContainerIDForBag(ctx, runID, bagID)
	if err != nil {
		t.Fatalf("findContainerIDForBag() error = %v", err)
	}
	if containerID != "container-from-bag" {
		t.Fatalf("containerID = %q, want container-from-bag", containerID)
	}
}

func TestFinalResultFallbackURLAndHTMLRewrite(t *testing.T) {
	runID := core.RunID("run_final_result_static")
	url := finalResultAssetURL(runID, "container-01", "index.html")
	if url != "/api/runs/run_final_result_static/preview/final-result/container-01/index.html" {
		t.Fatalf("finalResultAssetURL() = %q", url)
	}
	url = finalResultAssetURLWithVersion(runID, "container-01", "index.html", "2026-05-07T12:30:00Z")
	if url != "/api/runs/run_final_result_static/preview/final-result/container-01/index.html?v=2026-05-07T12%3A30%3A00Z" {
		t.Fatalf("finalResultAssetURLWithVersion() = %q", url)
	}
	rewritten := rewriteFinalResultHTML(`<!doctype html><html><head><script src="/src/main.js"></script></head><body></body></html>`, runID, "container-01")
	if !strings.Contains(rewritten, `<base href="/api/runs/run_final_result_static/preview/final-result/container-01/">`) {
		t.Fatalf("rewriteFinalResultHTML() missing base href:\n%s", rewritten)
	}
	asset, err := finalResultAssetFromPath([]string{"container-01", "src", "main.js"})
	if err != nil {
		t.Fatalf("finalResultAssetFromPath() error = %v", err)
	}
	if asset.ContainerID != "container-01" || asset.Path != "src/main.js" {
		t.Fatalf("asset = %+v", asset)
	}
}

func TestBootstrapHTTPHandlerCreatesRunAndRejectsBadJSON(t *testing.T) {
	handler := NewBootstrap(t.TempDir()).NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"run_id":"run_created_api","demand_summary":"build a real API","model_provider":"openai","model_name":"gpt-5.5","api_key":"test-key","base_url":"https://api.example.test/v1","api_style":"chat_completions","start_immediately":false}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/runs status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"run_id":"run_created_api"`) || !strings.Contains(body, `"status":"created"`) || !strings.Contains(body, `"model":"gpt-5.5"`) || !strings.Contains(body, `"base_url":"https://api.example.test/v1"`) {
		t.Fatalf("create run body missing persisted LLM config: %s", body)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"run_id":`))
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"error"`) {
		t.Fatalf("bad JSON body = %s, want JSON error", body)
	}
}

func TestBootstrapHTTPHandlerStartsRunWithoutSeedingRequirementWhenDemandIsProvided(t *testing.T) {
	bootstrap := NewBootstrap(t.TempDir())
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"run_id":"run_seeded_requirement_api","demand_summary":"甯垜鍋氫竴涓椽鍚冭泧娓告垙","start_immediately":true}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/runs status=%d body=%s", resp.Code, resp.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/runs/run_seeded_requirement_api/nodes/ceo_write_requirement", nil)
	detailResp := httptest.NewRecorder()
	handler.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("GET seeded requirement node status=%d body=%s", detailResp.Code, detailResp.Body.String())
	}
	body := detailResp.Body.String()
	for _, want := range []string{
		`"task_id":"ceo_write_requirement"`,
		`"status":"waiting_external"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("seeded requirement detail missing %s:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{
		`"result":"kok"`,
		`"uri":"projects/run_seeded_requirement_api/agents/ceo/artifacts/requirement/requirement_v1.md"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("seeded requirement detail unexpectedly contains %s:\n%s", unwanted, body)
		}
	}

	if _, err := bootstrap.Internals.TaskRepository.Get(context.Background(), "run_seeded_requirement_api", "pm_write_plan"); err == nil {
		t.Fatal("pm_write_plan should not exist before requirement confirmation")
	}
	run, err := bootstrap.Internals.RunRepository.Get(context.Background(), "run_seeded_requirement_api")
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusRunning {
		t.Fatalf("run status = %s, want running while collecting requirements", run.Status)
	}
}

func waitForHTTPRunStatus(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID, want core.RunStatus) core.PipelineRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	t.Fatalf("run status = %s, want %s", run.Status, want)
	return core.PipelineRun{}
}

func TestBootstrapHTTPHandlerServesCheckpointAPI(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_checkpoint_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPCheckpointTask(t, ctx, bootstrap, runID, "task_06", core.TaskStatusWaitingExternal)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_checkpoint_api/checkpoints", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET checkpoints status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"task_id":"task_06"`) || !strings.Contains(body, `"can_approve":true`) {
		t.Fatalf("GET checkpoints body missing actionable checkpoint:\n%s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_checkpoint_api/checkpoints/task_06", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET checkpoint detail status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"checkpoint_id":"task_06"`) {
		t.Fatalf("GET checkpoint detail body = %s, want task_06 checkpoint", body)
	}
}

func TestBootstrapHTTPHandlerApprovesCheckpointThroughOrchestratorFeedback(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_approve_checkpoint_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPCheckpointTask(t, ctx, bootstrap, runID, "task_06", core.TaskStatusWaitingExternal)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_approve_checkpoint_api/checkpoints/task_06/approve", strings.NewReader(`{"comment":"looks good"}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("approve checkpoint status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"done"`) || !strings.Contains(body, `"result":"kok"`) {
		t.Fatalf("approve checkpoint body = %s, want done/kok", body)
	}

	task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, "task_06")
	if err != nil {
		t.Fatalf("Get(task) error = %v", err)
	}
	if task.Status != core.TaskStatusDone || task.Result != core.TaskResultCodeOK {
		t.Fatalf("task after approve status=%s result=%s, want done/kok", task.Status, task.Result)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status after approving final task = %s, want awaiting_acceptance", run.Status)
	}
	if run.LatestAcceptanceCheckpointTaskID == "" {
		t.Fatalf("run latest acceptance checkpoint task id = empty, want generated acceptance task")
	}
}

func TestBootstrapHTTPHandlerRejectsCheckpointWithReason(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_reject_checkpoint_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPCheckpointTask(t, ctx, bootstrap, runID, "task_03", core.TaskStatusWaitingExternal)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_reject_checkpoint_api/checkpoints/task_03/reject", strings.NewReader(`{"reason":""}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("reject checkpoint without reason status=%d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_reject_checkpoint_api/checkpoints/task_03/reject", strings.NewReader(`{"reason":"plan is too vague","mode":"replan"}`))
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("reject checkpoint status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"blocked"`) || !strings.Contains(body, `"result":"kreplan"`) {
		t.Fatalf("reject checkpoint body = %s, want blocked/kreplan", body)
	}
	task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, "task_03")
	if err != nil {
		t.Fatalf("Get(task) error = %v", err)
	}
	if task.Status != core.TaskStatusBlocked || task.Result != core.TaskResultCodeReplan {
		t.Fatalf("task after reject status=%s result=%s, want blocked/kreplan", task.Status, task.Result)
	}
}

func TestBootstrapHTTPHandlerAcceptsGenericTaskFeedback(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_generic_feedback_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPCheckpointTask(t, ctx, bootstrap, runID, "task_06", core.TaskStatusWaitingExternal)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_generic_feedback_api/tasks/task_06/feedback", strings.NewReader(`{"result":"kok","message":"manual feedback"}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("generic feedback status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"done"`) || !strings.Contains(body, `"result":"kok"`) {
		t.Fatalf("generic feedback body = %s, want done/kok", body)
	}
}

func TestBootstrapHTTPHandlerServesArtifactContentAndRejectsEscapes(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_artifact_content_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	artifactPath := filepath.Join(run.ProjectDir, "agents", "pm01", "artifacts", "plan", "plan_v1.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("# Plan\n\nShip the flow."), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := bootstrap.Internals.ArtifactRepository.Create(ctx, repo.ArtifactRecord{
		ID:        "artifact_plan_content",
		RunID:     runID,
		TaskID:    "task_01",
		AgentID:   "pm01",
		Kind:      "plan",
		URI:       "agents/pm01/artifacts/plan/plan_v1.md",
		CreatedAt: time.Date(2026, 5, 2, 10, 8, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Create(artifact) error = %v", err)
	}
	if err := bootstrap.Internals.ArtifactRepository.Create(ctx, repo.ArtifactRecord{
		ID:        "artifact_escape_content",
		RunID:     runID,
		TaskID:    "task_01",
		AgentID:   "pm01",
		Kind:      "plan",
		URI:       "../secret.md",
		CreatedAt: time.Date(2026, 5, 2, 10, 9, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Create(escaping artifact) error = %v", err)
	}
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_artifact_content_api/artifacts/artifact_plan_content/content", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET artifact content status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"text":"# Plan\n\nShip the flow."`) || !strings.Contains(body, `"truncated":false`) {
		t.Fatalf("artifact content body = %s, want markdown text", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_artifact_content_api/artifacts/artifact_escape_content/content", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("escaping artifact status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestBootstrapHTTPHandlerServesOpenAPIJSON(t *testing.T) {
	handler := NewBootstrap(t.TempDir()).NewHTTPHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET openapi status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"/api/runs/{run_id}/checkpoints"`) || !strings.Contains(body, `"/api/runs/{run_id}/artifacts/{artifact_id}/content"`) {
		t.Fatalf("openapi body missing phase two paths:\n%s", body)
	}
}

func TestBootstrapHTTPHandlerServesSessionMessagesAndImprovementItems(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_session_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPSessionData(t, ctx, bootstrap, runID, 1)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_session_api/session/messages", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET session messages status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"message_id":"message_01"`) || !strings.Contains(body, `"content":"need a better preview flow"`) {
		t.Fatalf("GET session messages body = %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_session_api/improvement-items", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET improvement items status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"item_id":"item_01"`) || !strings.Contains(body, `"status":"open"`) {
		t.Fatalf("GET improvement items body = %s", body)
	}
}

func TestBootstrapHTTPHandlerApprovesAndContinuesAcceptanceCheckpoint(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_acceptance_api")
	seedAppHTTPAwaitingAcceptanceRun(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_acceptance_api/acceptance-checkpoint", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET acceptance-checkpoint status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"checkpoint_task_id":"acceptance_iter_01"`) || !strings.Contains(body, `"current_iteration_no":1`) {
		t.Fatalf("GET acceptance-checkpoint body = %s", body)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_acceptance_api/acceptance-checkpoint/approve", strings.NewReader(`{"comment":"accepted"}`))
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST acceptance approve status=%d body=%s", resp.Code, resp.Body.String())
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("run status after approval = %s, want completed", run.Status)
	}

	runID = core.RunID("run_continue_api")
	bootstrap = NewBootstrap(t.TempDir())
	seedAppHTTPAwaitingAcceptanceRun(t, ctx, bootstrap, runID)
	handler = bootstrap.NewHTTPHandler()

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_continue_api/acceptance-checkpoint/continue", strings.NewReader(`{"selected_item_ids":["item_01"],"freeform_text":"add a browser-like multi-tab preview"}`))
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST acceptance continue status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"running"`) || !strings.Contains(body, `"current_iteration_no":2`) {
		t.Fatalf("POST acceptance continue body = %s", body)
	}
	run, err = bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run after continue) error = %v", err)
	}
	if run.Status != core.RunStatusRunning || run.CurrentIterationNo != 2 {
		t.Fatalf("run after continue = %#v, want running iteration 2", run)
	}
	if run.LatestAcceptanceCheckpointTaskID != "" {
		t.Fatalf("run latest acceptance checkpoint after continue = %q, want cleared checkpoint reference", run.LatestAcceptanceCheckpointTaskID)
	}
	iterations, err := bootstrap.Internals.RunIterationRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(iterations) error = %v", err)
	}
	if len(iterations) != 2 || iterations[1].IterationNo != 2 {
		t.Fatalf("iterations after continue = %#v, want second iteration", iterations)
	}
	tasks, err := bootstrap.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(tasks) error = %v", err)
	}
	found := false
	for _, task := range tasks {
		if task.ID == "iter_02__ceo_write_requirement" && task.StageID == "ceo_write_requirement" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tasks after continue = %#v, want iter_02__ceo_write_requirement", tasks)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_continue_api/acceptance-checkpoint", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET acceptance-checkpoint after continue status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"running"`) || strings.Contains(body, `"checkpoint_task_id":"acceptance_iter_01"`) {
		t.Fatalf("GET acceptance-checkpoint after continue body = %s, want running status without stale checkpoint id", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_continue_api/workspace-overview", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET workspace-overview after continue status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"open_improvement_items":[]`) {
		t.Fatalf("GET workspace-overview after continue body = %s, want selected items excluded from open_improvement_items", body)
	}
}

func TestBootstrapHTTPHandlerMutatesSessionMessagesImprovementItemsAndIterations(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"支持多标签工作区，并保留插件校验结果。","should_emit_requirement_summary":false}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	runID := core.RunID("run_session_mutation_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Now().UTC()
	project := repo.ProjectRecord{
		ProjectID: "project_session_mutation_api",
		Name:      "Session Mutation Project",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := bootstrap.Internals.ProjectRepository.Create(ctx, project); err != nil {
		t.Fatalf("Create(project) error = %v", err)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	run.ProjectID = project.ProjectID
	run.Config.LLM = project.LLM
	run.UpdatedAt = now
	if err := bootstrap.Internals.RunRepository.Update(ctx, run); err != nil {
		t.Fatalf("Update(run) error = %v", err)
	}
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_session_mutation_api/session/messages", strings.NewReader(`{"content":"please add browser-like tabs and keep a plugin validation report","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST session message status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"accepted":true`) || !strings.Contains(body, `"message_type":"chat"`) {
		t.Fatalf("POST session message body = %s", body)
	}

	waitForHTTPSessionMessages(t, ctx, bootstrap, runID, 2)
	messages, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	if len(messages) < 2 {
		t.Fatalf("session messages = %#v, want persisted user + ceo reply", messages)
	}
	if messages[0].Role != "user" || !strings.Contains(messages[0].Content, "browser-like tabs") {
		t.Fatalf("first session message = %#v, want persisted user message", messages[0])
	}
	if messages[len(messages)-1].Role != "ceo" {
		t.Fatalf("last session message = %#v, want ceo reply", messages[len(messages)-1])
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_session_mutation_api/improvement-items", strings.NewReader(`{"title":"Support tabs","detail":"Open multiple node workspaces side by side","source":"user"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST improvement item status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"title":"Support tabs"`) || !strings.Contains(body, `"status":"open"`) {
		t.Fatalf("POST improvement item body = %s", body)
	}

	items, err := bootstrap.Internals.ImprovementItemRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(improvement items) error = %v", err)
	}
	if len(items) == 0 {
		t.Fatal("improvement items empty, want created item")
	}
	createdItemID := items[len(items)-1].ItemID

	req = httptest.NewRequest(http.MethodPatch, "/api/runs/run_session_mutation_api/improvement-items/"+createdItemID, strings.NewReader(`{"status":"selected","title":"Support workspace tabs","detail":"Focus existing tabs instead of opening duplicates"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("PATCH improvement item status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"selected"`) || !strings.Contains(body, `"title":"Support workspace tabs"`) {
		t.Fatalf("PATCH improvement item body = %s", body)
	}

	items, err = bootstrap.Internals.ImprovementItemRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(improvement items after patch) error = %v", err)
	}
	foundUpdated := false
	for _, item := range items {
		if item.ItemID == createdItemID && item.Status == "selected" && item.Title == "Support workspace tabs" {
			foundUpdated = true
			break
		}
	}
	if !foundUpdated {
		t.Fatalf("improvement items after patch = %#v, want updated item %s", items, createdItemID)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_session_mutation_api/iterations", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET iterations status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"iteration_no":1`) || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("GET iterations body = %s", body)
	}
}

func TestBootstrapHTTPHandlerRequiresProjectLLMConfigForSessionMessages(t *testing.T) {
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_session_requires_project_api")
	seedAppHTTPRun(t, context.Background(), bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_session_requires_project_api/session/messages", strings.NewReader(`{"content":"help me refine this requirement","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("POST session message without project llm status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"error":"please select an llm model first"`) {
		t.Fatalf("POST session message without project llm body = %s", body)
	}
}

func TestBootstrapHTTPHandlerServesProjectsAndPersistsRunProjectBinding(t *testing.T) {
	bootstrap := NewBootstrap(t.TempDir())
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{
		"name":"Doujia Product A",
		"llm":{
			"provider":"openai",
			"model":"gpt-5.5",
			"api_key":"test-key",
			"base_url":"https://api.example.test/v1",
			"api_style":"responses"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST project status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"name":"Doujia Product A"`) || !strings.Contains(body, `"model":"gpt-5.5"`) {
		t.Fatalf("POST project body = %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET projects status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"items"`) || !strings.Contains(body, `"Doujia Product A"`) {
		t.Fatalf("GET projects body = %s", body)
	}

	var list struct {
		Items []struct {
			ProjectID string `json:"project_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ProjectID == "" {
		t.Fatalf("projects list = %#v, want one created project", list.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{
		"run_id":"run_with_project_binding_api",
		"demand_summary":"build a browser-like workspace",
		"project_id":"`+list.Items[0].ProjectID+`",
		"start_immediately":false
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST run with project status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"project_id":"`+list.Items[0].ProjectID+`"`) || !strings.Contains(body, `"model":"gpt-5.5"`) {
		t.Fatalf("POST run with project body = %s", body)
	}
}

func TestBootstrapHTTPHandlerCreatesRequirementSummaryAndConfirmsIntoRequirementPool(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"我理解了，先收敛成一条需求给你确认。","should_emit_requirement_summary":true,"summary_title":"Doujia 聊天确认卡片","summary_detail":"把 CEO 改名为 Doujia，并在确认后再加入需求池。"}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{
		"name":"Doujia Product B",
		"llm":{
			"provider":"openai",
			"model":"gpt-5.5",
			"api_key":"test-key",
			"base_url":"https://api.example.test/v1",
			"api_style":"responses"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	projectResp := httptest.NewRecorder()
	handler.ServeHTTP(projectResp, req)
	if projectResp.Code != http.StatusCreated {
		t.Fatalf("POST project for requirement summary status=%d body=%s", projectResp.Code, projectResp.Body.String())
	}
	var createdProject struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(projectResp.Body.Bytes(), &createdProject); err != nil {
		t.Fatalf("decode created project: %v", err)
	}

	runID := core.RunID("run_requirement_summary_api")
	req = httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{
		"run_id":"run_requirement_summary_api",
		"demand_summary":"initial summary",
		"project_id":"`+createdProject.ProjectID+`",
		"start_immediately":false
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST run for requirement summary status=%d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_requirement_summary_api/session/messages", strings.NewReader(`{"content":"we need a calmer Doujia chat flow with requirement confirmation cards","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST session message for requirement summary status=%d body=%s", resp.Code, resp.Body.String())
	}
	waitForHTTPSessionMessages(t, ctx, bootstrap, runID, 2)

	messages, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session messages with requirement summary) error = %v", err)
	}
	foundSummary := false
	summaryMessageID := ""
	for _, message := range messages {
		if message.MessageType == "requirement_summary" {
			foundSummary = true
			summaryMessageID = message.ID
			break
		}
	}
	if !foundSummary || summaryMessageID == "" {
		t.Fatalf("session messages = %#v, want requirement_summary message", messages)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_requirement_summary_api/session/messages", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET requirement summary session messages status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"confirm_action_label":"确认加入需求池"`) || !strings.Contains(body, `"revise_action_label":"继续补充"`) {
		t.Fatalf("GET requirement summary session messages body = %s, want readable confirm labels", body)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_requirement_summary_api/session/messages/"+summaryMessageID+"/confirm", strings.NewReader(`{"action":"accept"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST requirement summary confirm status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"confirmed":true`) {
		t.Fatalf("POST requirement summary confirm body = %s", body)
	}

	items, err := bootstrap.Internals.ImprovementItemRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(improvement items after requirement summary confirm) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("improvement items len = %d, want 1 confirmed requirement item", len(items))
	}
	if got := items[0].Source; got != "requirement_pool_confirmed" {
		t.Fatalf("confirmed requirement item source = %q, want requirement_pool_confirmed", got)
	}
}

func TestBootstrapHTTPHandlerStreamsDoujiaReplyDeltasWithoutRawJSON(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{streamChunks: []string{
				`{"reply":"hello `,
				`world","should_emit_requirement_summary":true,`,
				`"summary_title":"Snake","summary_detail":"Browser game"}`,
			}}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	runID := core.RunID("run_session_stream_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Now().UTC()
	project := repo.ProjectRecord{
		ProjectID: "project_session_stream_api",
		Name:      "Session Stream Project",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "chat_completions",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := bootstrap.Internals.ProjectRepository.Create(ctx, project); err != nil {
		t.Fatalf("Create(project) error = %v", err)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	run.ProjectID = project.ProjectID
	run.Config.LLM = project.LLM
	run.UpdatedAt = now
	if err := bootstrap.Internals.RunRepository.Update(ctx, run); err != nil {
		t.Fatalf("Update(run) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_session_stream_api/session/messages/stream", strings.NewReader(`{"content":"make snake","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	bootstrap.NewHTTPHandler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST session stream status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := resp.Body.String()
	for _, want := range []string{
		"event: delta",
		`"delta":"hello `,
		`"delta":"world"`,
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `{"reply"`) || strings.Contains(body, `should_emit_requirement_summary`) {
		t.Fatalf("stream body leaked raw model JSON:\n%s", body)
	}

	messages, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("session messages len = %d, want user + chat + requirement summary: %#v", len(messages), messages)
	}
	if messages[1].MessageType != "chat" || messages[1].Content != "hello world" {
		t.Fatalf("streamed chat message = %#v, want clean chat content", messages[1])
	}
	if messages[2].MessageType != "requirement_summary" || !strings.Contains(messages[2].Content, "Snake") {
		t.Fatalf("streamed summary message = %#v, want requirement summary", messages[2])
	}
}

func TestBootstrapHTTPHandlerStreamsDoujiaReplyDeltasForResponsesMode(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{streamChunks: []string{
				`{"reply":"hello `,
				`world","should_emit_requirement_summary":false}`,
			}}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	runID := core.RunID("run_session_stream_responses_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Now().UTC()
	project := repo.ProjectRecord{
		ProjectID: "project_session_stream_responses_api",
		Name:      "Session Stream Responses Project",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := bootstrap.Internals.ProjectRepository.Create(ctx, project); err != nil {
		t.Fatalf("Create(project) error = %v", err)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	run.ProjectID = project.ProjectID
	run.Config.LLM = project.LLM
	run.UpdatedAt = now
	if err := bootstrap.Internals.RunRepository.Update(ctx, run); err != nil {
		t.Fatalf("Update(run) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_session_stream_responses_api/session/messages/stream", strings.NewReader(`{"content":"make snake","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	bootstrap.NewHTTPHandler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST session stream status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := resp.Body.String()
	for _, want := range []string{
		"event: delta",
		`"delta":"hello `,
		`"delta":"world"`,
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `{"reply"`) || strings.Contains(body, `should_emit_requirement_summary`) {
		t.Fatalf("stream body leaked raw model JSON:\n%s", body)
	}

	messages, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("session messages len = %d, want user + chat: %#v", len(messages), messages)
	}
	if messages[1].MessageType != "chat" || messages[1].Content != "hello world" {
		t.Fatalf("streamed chat message = %#v, want clean chat content", messages[1])
	}
}

func TestBootstrapGenerateDoujiaSessionResponseKeepsClarificationShortWithoutSummary(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"先告诉我你更在意玩法、界面还是部署方式。","should_emit_requirement_summary":false}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}

	messages, artifacts, err := bootstrap.generateDoujiaSessionResponse(ctx, repo.RunRecord{
		ID:                 "run_short_reply_api",
		CurrentIterationNo: 1,
	}, repo.ProjectRecord{
		ProjectID: "project_short_reply_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}, "鎴戞兂鍋氫竴涓椽鍚冭泧娓告垙", time.Now().UTC(), nil, nil, "", "")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want only one chat reply", len(messages))
	}
	if messages[0].MessageType != "chat" {
		t.Fatalf("message type = %s, want chat", messages[0].MessageType)
	}
	if len([]rune(messages[0].Content)) > 40 {
		t.Fatalf("chat reply too long: %q", messages[0].Content)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want none", artifacts)
	}
}

func TestParseDoujiaSessionReplyExtractsReplyFromTruncatedJSON(t *testing.T) {
	reply, summary, err := parseDoujiaSessionReply(`{"reply":"Need platform first.","should_emit_requirement_summary":false,"summary_title":"`)
	if err != nil {
		t.Fatalf("parseDoujiaSessionReply error = %v", err)
	}
	if reply != "Need platform first." {
		t.Fatalf("reply = %q, want extracted reply without raw JSON", reply)
	}
	if summary != "" {
		t.Fatalf("summary = %q, want empty summary for truncated JSON", summary)
	}
}

func TestBootstrapGenerateDoujiaSessionResponseCreatesCompactRequirementSummaryWhenRequested(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"我理解了，先给你一条确认卡片。","should_emit_requirement_summary":true,"summary_title":"Doujia 聊天确认卡片","summary_detail":"把 CEO 改名为 Doujia，并在确认后再加入需求池。"}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}

	messages, artifacts, err := bootstrap.generateDoujiaSessionResponse(ctx, repo.RunRecord{
		ID:                 "run_summary_reply_api",
		CurrentIterationNo: 1,
	}, repo.ProjectRecord{
		ProjectID: "project_summary_reply_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}, "璇锋妸 CEO 鏀规垚 Doujia", time.Now().UTC(), nil, nil, "", "")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want chat + summary", len(messages))
	}
	if messages[1].MessageType != "requirement_summary" {
		t.Fatalf("summary message type = %s, want requirement_summary", messages[1].MessageType)
	}
	if len([]rune(messages[1].Content)) > 80 {
		t.Fatalf("summary content too long: %q", messages[1].Content)
	}
	if strings.Contains(messages[1].Content, "璇锋妸 CEO 鏀规垚 Doujia") {
		t.Fatalf("summary content = %q, want compact summary instead of raw user text", messages[1].Content)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts len = %d, want one summary artifact", len(artifacts))
	}
	if artifacts[0].Title == "" || len([]rune(artifacts[0].Content)) > 80 {
		t.Fatalf("artifact = %#v, want compact titled summary artifact", artifacts[0])
	}
}

func TestBootstrapHTTPHandlerConfirmsRequirementsAndDispatchesCEO(t *testing.T) {
	ctx := context.Background()
	lastReq := &llm.ChatRequest{}
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{
				response: `{"reply":"我理解了，先给你一条确认卡片。","should_emit_requirement_summary":true,"summary_title":"多标签工作区","summary_detail":"创建 run 后在真实 workspace 中继续聊天，并提供总确认按钮。"}`,
				lastReq:  lastReq,
			}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	handler := bootstrap.NewHTTPHandler()

	projectReq := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{
		"name":"Doujia Product C",
		"llm":{
			"provider":"openai",
			"model":"gpt-5.5",
			"api_key":"test-key",
			"base_url":"https://api.example.test/v1",
			"api_style":"responses"
		}
	}`))
	projectReq.Header.Set("Content-Type", "application/json")
	projectResp := httptest.NewRecorder()
	handler.ServeHTTP(projectResp, projectReq)
	if projectResp.Code != http.StatusCreated {
		t.Fatalf("POST project status=%d body=%s", projectResp.Code, projectResp.Body.String())
	}
	var createdProject struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(projectResp.Body.Bytes(), &createdProject); err != nil {
		t.Fatalf("decode created project: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{
		"run_id":"run_confirm_requirements_api",
		"project_id":"`+createdProject.ProjectID+`",
		"start_immediately":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST run status=%d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_confirm_requirements_api/session/messages", strings.NewReader(`{"content":"创建 run 后我要在真实 workspace 里继续和 Doujia 聊天，并且最后统一确认需求。","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST session message status=%d body=%s", resp.Code, resp.Body.String())
	}
	waitForHTTPSessionMessages(t, ctx, bootstrap, "run_confirm_requirements_api", 2)

	messages, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, "run_confirm_requirements_api")
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	summaryMessageID := ""
	for _, message := range messages {
		if message.MessageType == "requirement_summary" {
			summaryMessageID = message.ID
			break
		}
	}
	if summaryMessageID == "" {
		t.Fatal("expected requirement summary message before confirm")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_confirm_requirements_api/session/messages/"+summaryMessageID+"/confirm", strings.NewReader(`{"action":"accept"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST requirement summary confirm status=%d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_confirm_requirements_api/requirements/confirm", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST requirements confirm status=%d body=%s", resp.Code, resp.Body.String())
	}

	run := waitForHTTPRunStatus(t, ctx, bootstrap, "run_confirm_requirements_api", core.RunStatusAwaitingAcceptance)
	if run.LatestAcceptanceCheckpointTaskID == "" {
		t.Fatalf("run latest acceptance checkpoint task id = empty, want generated acceptance task")
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/runs/run_confirm_requirements_api/nodes/ceo_write_requirement", nil)
	detailResp := httptest.NewRecorder()
	handler.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("GET CEO node detail status=%d body=%s", detailResp.Code, detailResp.Body.String())
	}
	body := detailResp.Body.String()
	for _, want := range []string{
		`"task_id":"ceo_write_requirement"`,
		`"result":"kok"`,
		`"uri":"projects/run_confirm_requirements_api/agents/ceo/artifacts/requirement/requirement_v1.md"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("CEO node detail missing %s:\n%s", want, body)
		}
	}
}

func TestBootstrapGenerateDoujiaSessionResponseBuildsPromptWithHistoryRequirementPoolAndProgress(t *testing.T) {
	ctx := context.Background()
	lastReq := &llm.ChatRequest{}
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{
				response: `{"reply":"我先继续帮你补充。","should_emit_requirement_summary":false}`,
				lastReq:  lastReq,
			}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}

	now := time.Now().UTC()
	run := repo.RunRecord{
		ID:                 "run_prompt_context_api",
		Status:             core.RunStatusRunning,
		CurrentIterationNo: 1,
	}
	project := repo.ProjectRecord{
		ProjectID: "project_prompt_context_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}
	history := []repo.SessionMessageRecord{
		{ID: "m1", RunID: run.ID, IterationNo: 1, Role: "user", MessageType: "chat", Content: "先做真实 workspace 聊天", CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "m2", RunID: run.ID, IterationNo: 1, Role: "ceo", MessageType: "chat", Content: "我会继续帮你澄清", CreatedAt: now.Add(-2 * time.Minute)},
	}
	pool := []repo.ImprovementItemRecord{
		{ItemID: "item_01", RunID: run.ID, IterationNo: 1, Title: "真实 workspace 聊天", Detail: "创建 run 后继续在 workspace 对话", Source: "requirement_pool_confirmed", Status: "confirmed", CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)},
	}

	messages, artifacts, err := bootstrap.generateDoujiaSessionResponse(ctx, run, project, "再加一个需求总确认按钮", now, history, pool, "ceo_write_requirement waiting_external", "")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want one chat reply", len(messages))
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want no summary artifacts", artifacts)
	}
	if len(lastReq.Messages) != 2 {
		t.Fatalf("chat request messages len = %d, want system + user", len(lastReq.Messages))
	}
	prompt := lastReq.Messages[1].Content
	for _, want := range []string{
		"Current run context:",
		"Conversation history:",
		"先做真实 workspace 聊天",
		"Confirmed requirement pool:",
		"真实 workspace 聊天",
		"Current progress:",
		"ceo_write_requirement waiting_external",
		"User message:",
		"再加一个需求总确认按钮",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBootstrapGenerateDoujiaSessionResponseBuildsPromptWithCompressedMemorySummary(t *testing.T) {
	ctx := context.Background()
	lastReq := &llm.ChatRequest{}
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{
				response: `{"reply":"我先继续帮你补充。","should_emit_requirement_summary":false}`,
				lastReq:  lastReq,
			}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}

	_, _, err = bootstrap.generateDoujiaSessionResponse(ctx, repo.RunRecord{
		ID:                 "run_prompt_memory_api",
		CurrentIterationNo: 1,
	}, repo.ProjectRecord{
		ProjectID: "project_prompt_memory_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}, "继续聊一下", time.Now().UTC(), nil, nil, "", "之前记住的偏好：简短回复优先")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(lastReq.Messages) != 2 {
		t.Fatalf("chat request messages len = %d, want system + user", len(lastReq.Messages))
	}
	prompt := lastReq.Messages[1].Content
	for _, want := range []string{
		"Compressed memory summary:",
		"之前记住的偏好：简短回复优先",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBootstrapHTTPHandlerServesFocusedPipelineWorkspaceGraph(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_workspace_focus_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPWorkspace(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/runs/run_workspace_focus_api/pipeline-workspace?focus_instance_id=root_test_all_modules_module01",
		nil,
	)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET focused pipeline-workspace status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"focus_instance_id":"root_test_all_modules_module01"`,
		`"focus_pipeline_id":"pipeline_module"`,
		`"node_id":"task:root_test_all_modules_module01_write_code_single_write_code"`,
		`"node_kind":"task"`,
		`"node_id":"fork:root_test_all_modules_module01:module_work"`,
		`"node_id":"join:root_test_all_modules_module01:module_test_input"`,
		`"from_node_id":"fork:root_test_all_modules_module01:module_work","to_node_id":"task:root_test_all_modules_module01_write_code_single_write_code","kind":"fork"`,
		`"from_node_id":"fork:root_test_all_modules_module01:module_work","to_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","kind":"fork"`,
		`"from_node_id":"task:root_test_all_modules_module01_write_code_single_write_code","to_node_id":"join:root_test_all_modules_module01:module_test_input","kind":"join"`,
		`"from_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","to_node_id":"join:root_test_all_modules_module01:module_test_input","kind":"join"`,
		`"from_node_id":"join:root_test_all_modules_module01:module_test_input","to_node_id":"child:test_code:root_test_all_modules_module01_test_code_single","kind":"sequence"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET focused pipeline-workspace body missing %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"from_node_id":"task:root_test_all_modules_module01_write_code_single_write_code","to_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","kind":"sequence"`) {
		t.Fatalf("focused pipeline-workspace incorrectly serialized write_code -> write_test_data as a sequence edge:\n%s", body)
	}
	if strings.Contains(body, `"from_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","to_node_id":"child:test_code:root_test_all_modules_module01_test_code_single","kind":"sequence"`) {
		t.Fatalf("focused pipeline-workspace incorrectly serialized write_test_data -> test_code as a sequence edge:\n%s", body)
	}
}

func TestBootstrapHTTPHandlerServesFocusedPipelineWorkspaceGraphFromChildPipelines(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_workspace_focus_child_pipeline_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPWorkspaceWithModuleChildPipelinesOnly(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/runs/run_workspace_focus_child_pipeline_api/pipeline-workspace?focus_instance_id=root_test_all_modules_module01",
		nil,
	)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET focused pipeline-workspace status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"from_node_id":"fork:root_test_all_modules_module01:module_work","to_node_id":"child:write_code:root_test_all_modules_module01_write_code_single","kind":"fork"`,
		`"from_node_id":"fork:root_test_all_modules_module01:module_work","to_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","kind":"fork"`,
		`"from_node_id":"child:write_code:root_test_all_modules_module01_write_code_single","to_node_id":"join:root_test_all_modules_module01:module_test_input","kind":"join"`,
		`"from_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","to_node_id":"join:root_test_all_modules_module01:module_test_input","kind":"join"`,
		`"from_node_id":"join:root_test_all_modules_module01:module_test_input","to_node_id":"child:test_code:root_test_all_modules_module01_test_code_single","kind":"sequence"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET focused child-pipeline workspace body missing %s:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		`"from_node_id":"child:write_code:root_test_all_modules_module01_write_code_single","to_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","kind":"sequence"`,
		`"from_node_id":"child:write_test_data:root_test_all_modules_module01_write_test_data_single","to_node_id":"child:test_code:root_test_all_modules_module01_test_code_single","kind":"sequence"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("focused child-pipeline workspace contains forbidden sequence edge %s:\n%s", forbidden, body)
		}
	}
}

func TestBootstrapGenerateDoujiaSessionResponseSuppressesQuestionLikeRequirementSummary(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"我先继续帮你澄清一下。","should_emit_requirement_summary":true,"summary_title":"历史记录功能","summary_detail":"你更偏向操作历史、搜索历史，还是内容历史？"}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}

	messages, artifacts, err := bootstrap.generateDoujiaSessionResponse(ctx, repo.RunRecord{
		ID:                 "run_question_like_summary_api",
		CurrentIterationNo: 1,
	}, repo.ProjectRecord{
		ProjectID: "project_question_like_summary_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}, "我觉得可以增加一个历史记录功能", time.Now().UTC(), nil, nil, "", "")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want only one clarification chat", len(messages))
	}
	if messages[0].MessageType != "chat" {
		t.Fatalf("message type = %s, want chat", messages[0].MessageType)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want none", artifacts)
	}
}

func TestBootstrapGenerateDoujiaSessionResponseSuppressesGreetingSummary(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"你好，我在。","should_emit_requirement_summary":true,"summary_title":"你好","summary_detail":"可以，方向合理。"} `}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}

	messages, artifacts, err := bootstrap.generateDoujiaSessionResponse(ctx, repo.RunRecord{
		ID:                 "run_greeting_summary_api",
		CurrentIterationNo: 1,
	}, repo.ProjectRecord{
		ProjectID: "project_greeting_summary_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}, "你好", time.Now().UTC(), nil, nil, "", "")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want only one chat reply", len(messages))
	}
	if messages[0].MessageType != "chat" {
		t.Fatalf("message type = %s, want chat", messages[0].MessageType)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want none", artifacts)
	}
}

func TestBootstrapHTTPHandlerPersistsAndReusesCompressedDoujiaMemory(t *testing.T) {
	ctx := context.Background()
	lastReq := &llm.ChatRequest{}
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{
				response: `{"reply":"收到，我记住了。","should_emit_requirement_summary":false}`,
				lastReq:  lastReq,
			}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	handler := bootstrap.NewHTTPHandler()
	runID := core.RunID("run_doujia_memory_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Now().UTC()
	if err := bootstrap.Internals.ProjectRepository.Create(ctx, repo.ProjectRecord{
		ProjectID: "project_doujia_memory_api",
		Name:      "Doujia Memory Project",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(project) error = %v", err)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	run.ProjectID = "project_doujia_memory_api"
	run.UpdatedAt = now
	if err := bootstrap.Internals.RunRepository.Update(ctx, run); err != nil {
		t.Fatalf("Update(run project binding) error = %v", err)
	}

	messages := []string{
		"我偏好先聊天收敛需求",
		"我要保留这个偏好",
		"请把这个偏好记住",
		"后面如果够短就直接确认",
		"记住我更在意简短回复而不是长解释",
		"这个偏好很重要",
		"别丢掉这个偏好",
		"继续记住这个偏好",
		"保留最早那个偏好",
		"现在再提醒我一次更在意简短回复",
	}
	for i, content := range messages {
		req := httptest.NewRequest(http.MethodPost, "/api/runs/run_doujia_memory_api/session/messages", strings.NewReader(`{"content":"`+content+`","message_type":"chat"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusAccepted {
			t.Fatalf("POST session message %d status=%d body=%s", i+1, resp.Code, resp.Body.String())
		}
	}

	artifacts, err := bootstrap.Internals.SessionArtifactRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session artifacts) error = %v", err)
	}
	var memoryCount int
	for _, artifact := range artifacts {
		if artifact.Kind == "conversation_memory" {
			memoryCount++
			if artifact.Content == "" {
				t.Fatalf("conversation memory artifact content is empty: %#v", artifact)
			}
		}
	}
	if memoryCount == 0 {
		t.Fatalf("session artifacts = %#v, want conversation_memory artifact", artifacts)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_doujia_memory_api/session/messages", strings.NewReader(`{"content":"我还是更在意简短回复吗","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST session message with memory reuse status=%d body=%s", resp.Code, resp.Body.String())
	}

	if len(lastReq.Messages) != 2 {
		t.Fatalf("chat request messages len = %d, want system + user", len(lastReq.Messages))
	}
	prompt := lastReq.Messages[1].Content
	for _, want := range []string{
		"Compressed memory summary:",
		"更在意简短回复",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBootstrapHTTPHandlerConfirmsRequirementsIdempotentlyAndSupportsRemoval(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"我理解了，先给你一条确认卡片。","should_emit_requirement_summary":true,"summary_title":"需求确认项","summary_detail":"把需求收敛成单独的确认项。"}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	handler := bootstrap.NewHTTPHandler()
	runID := core.RunID("run_requirement_pool_idempotent_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Now().UTC()
	if err := bootstrap.Internals.ProjectRepository.Create(ctx, repo.ProjectRecord{
		ProjectID: "project_requirement_pool_idempotent_api",
		Name:      "Requirement Pool Project",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(project) error = %v", err)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	run.ProjectID = "project_requirement_pool_idempotent_api"
	run.UpdatedAt = now
	if err := bootstrap.Internals.RunRepository.Update(ctx, run); err != nil {
		t.Fatalf("Update(run project binding) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/run_requirement_pool_idempotent_api/session/messages", strings.NewReader(`{"content":"把需求收敛成单独的确认项","message_type":"chat"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST session message status=%d body=%s", resp.Code, resp.Body.String())
	}

	messages, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	var summaryMessageID string
	for _, message := range messages {
		if message.MessageType == "requirement_summary" {
			summaryMessageID = message.ID
		}
	}
	if summaryMessageID == "" {
		t.Fatalf("session messages = %#v, want requirement_summary message", messages)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_requirement_pool_idempotent_api/session/messages/"+summaryMessageID+"/confirm", strings.NewReader(`{"action":"accept"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST requirement summary confirm status=%d body=%s", resp.Code, resp.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/runs/run_requirement_pool_idempotent_api/session/messages/"+summaryMessageID+"/confirm", strings.NewReader(`{"action":"accept"}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST requirement summary confirm second time status=%d body=%s", resp.Code, resp.Body.String())
	}

	items, err := bootstrap.Internals.ImprovementItemRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(improvement items) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("improvement items len = %d, want 1 after idempotent confirm", len(items))
	}

	itemID := items[0].ItemID
	req = httptest.NewRequest(http.MethodDelete, "/api/runs/run_requirement_pool_idempotent_api/improvement-items/"+itemID, nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("DELETE improvement item status=%d body=%s", resp.Code, resp.Body.String())
	}
	items, err = bootstrap.Internals.ImprovementItemRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(improvement items after delete) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("improvement items len = %d, want 1 historical item after delete", len(items))
	}
	if got := items[0].Status; got != "removed" {
		t.Fatalf("improvement item status = %q, want removed", got)
	}
}

func TestBootstrapGenerateDoujiaSessionResponseSuppressesWaitExternalApprovalMessage(t *testing.T) {
	ctx := context.Background()
	bootstrap, err := NewBootstrapWithOptions(t.TempDir(), BootstrapOptions{
		DoujiaLLMFactory: func(config core.LLMRunConfig) llm.Adapter {
			return fakeDoujiaLLM{response: `{"reply":"我先继续帮你澄清一下。","should_emit_requirement_summary":false}`}
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions error = %v", err)
	}
	msgs, _, err := bootstrap.generateDoujiaSessionResponse(ctx, repo.RunRecord{
		ID:                 "run_hide_waiting_external_api",
		CurrentIterationNo: 1,
	}, repo.ProjectRecord{
		ProjectID: "project_hide_waiting_external_api",
		LLM: core.LLMRunConfig{
			Provider: "openai",
			Model:    "gpt-5.5",
			APIKey:   "test-key",
			BaseURL:  "https://api.example.test/v1",
			APIStyle: "responses",
		},
	}, "继续补需求", time.Now().UTC(), nil, nil, "task waiting_external", "")
	if err != nil {
		t.Fatalf("generateDoujiaSessionResponse error = %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageType != "chat" {
		t.Fatalf("messages = %#v, want only chat reply", msgs)
	}
}

func TestBootstrapHTTPHandlerServesWorkspaceViews(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_workspace_api")
	seedAppHTTPAwaitingAcceptanceRun(t, ctx, bootstrap, runID)
	seedAppHTTPWorkspace(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_workspace_api/workspace-overview", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET workspace-overview status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"run_id":"run_workspace_api"`,
		`"current_iteration_no":1`,
		`"checkpoint_task_id":"acceptance_iter_01"`,
		`"kind":"requirement_doc"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET workspace-overview body missing %s:\n%s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/runs/run_workspace_api/pipeline-workspace", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET pipeline-workspace status=%d body=%s", resp.Code, resp.Body.String())
	}
	body = resp.Body.String()
	for _, want := range []string{
		`"run_id":"run_workspace_api"`,
		`"instance_id":"root"`,
		`"instance_id":"root_test_all_modules_module01"`,
		`"default_collapsed":true`,
		`"status":"waiting_human"`,
		`"status":"completed"`,
		`"main_pipeline_nodes"`,
		`"main_pipeline_edges"`,
		`"collapsed_child_pipeline_nodes"`,
		`"node_id":"task:root__split_module"`,
		`"node_id":"fork:prepare_delivery"`,
		`"node_id":"child:test_all_modules:root_test_all_modules_module01"`,
		`"node_id":"child:write_global_test_data:root_write_global_test_data_global"`,
		`"label":"模块并行开发"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET pipeline-workspace body missing %s:\n%s", want, body)
		}
	}
}

func TestBootstrapHTTPHandlerServesFullRootMainPipelineGraphWhenRootTasksLackInstanceID(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	runID := core.RunID("run_workspace_root_main_graph_api")
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	seedAppHTTPWorkspaceWithDetachedRootTasks(t, ctx, bootstrap, runID)
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run_workspace_root_main_graph_api/pipeline-workspace", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET pipeline-workspace status=%d body=%s", resp.Code, resp.Body.String())
	}

	body := resp.Body.String()
	for _, want := range []string{
		`"node_id":"task:ceo_write_requirement"`,
		`"node_id":"task:pm_write_plan"`,
		`"node_id":"task:architect_write_plan"`,
		`"node_id":"task:architect_create_container"`,
		`"node_id":"task:split_module"`,
		`"node_id":"fork:implementation"`,
		`"node_id":"join:implementation"`,
		`"node_id":"child:merge_code:root_merge_code_single"`,
		`"node_id":"child:global_test_code:root_global_test_code_single"`,
		`"node_id":"task:acceptance_iter_01"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET pipeline-workspace body missing %s:\n%s", want, body)
		}
	}

	joinIndex := strings.Index(body, `"node_id":"join:implementation"`)
	mergeIndex := strings.Index(body, `"node_id":"child:merge_code:root_merge_code_single"`)
	globalTestIndex := strings.Index(body, `"node_id":"child:global_test_code:root_global_test_code_single"`)
	acceptanceIndex := strings.Index(body, `"node_id":"task:acceptance_iter_01"`)
	if !(joinIndex >= 0 && mergeIndex > joinIndex && globalTestIndex > mergeIndex && acceptanceIndex > globalTestIndex) {
		t.Fatalf("main pipeline ordering wrong:\n%s", body)
	}
}

func TestBootstrapHTTPHandlerServesPluginCenterLifecycle(t *testing.T) {
	ctx := context.Background()
	bootstrap := NewBootstrap(t.TempDir())
	handler := bootstrap.NewHTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/registry-state", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET plugin registry-state status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"handlers"`) || !strings.Contains(body, `"roles"`) || !strings.Contains(body, `"pipelines"`) {
		t.Fatalf("GET registry-state body = %s", body)
	}

	validPipeline := "\ufeff" + `{
  "schema_version": "devflow.pipeline/v0.4",
  "pipeline_id": "workspace_preview_pipeline",
  "name": "Workspace Preview Pipeline",
  "namespace": {
    "agents": [
      {"name": "pm", "role": "pm"}
    ]
  },
  "signature": {
    "agents": [
      {"name": "pm", "role": "pm"}
    ]
  },
  "start_state": "draft_ready",
  "delivery_state": "preview_ready",
  "states": [
    {"id": "draft_ready", "next": {"type": "all", "transitions": ["pm_write_plan"]}},
    {"id": "preview_ready"}
  ],
  "transitions": [
    {
      "id": "pm_write_plan",
      "kind": "task",
      "from_state": "draft_ready",
      "to_state": "preview_ready",
      "agent": {"role": "pm", "alias": "pm"},
      "op": "write_plan"
    }
  ]
}`

	req = newMultipartRequest(t, http.MethodPost, "/api/plugins/upload-pipeline", "file", "workspace_preview_pipeline.json", validPipeline)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST upload-pipeline status=%d body=%s", resp.Code, resp.Body.String())
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode upload-pipeline response: %v", err)
	}
	if accepted.JobID == "" {
		t.Fatalf("upload-pipeline response missing job_id: %s", resp.Body.String())
	}

	waitForHTTPPluginValidationStatus(t, ctx, bootstrap, accepted.JobID, "succeeded")

	req = httptest.NewRequest(http.MethodGet, "/api/plugins/validations/"+accepted.JobID, nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET plugin validation status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"succeeded"`) || !strings.Contains(body, `"activated_pipelines":["workspace_preview_pipeline"]`) {
		t.Fatalf("GET plugin validation body = %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugins/registry-state", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET plugin registry-state after activation status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"workspace_preview_pipeline"`) {
		t.Fatalf("registry-state after activation missing uploaded pipeline:\n%s", body)
	}

	invalidPackRoot := filepath.Join(t.TempDir(), "invalid_pack")
	if err := os.MkdirAll(filepath.Join(invalidPackRoot, "roles", "broken_role"), 0o755); err != nil {
		t.Fatalf("MkdirAll(invalid pack) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidPackRoot, "plugin.json"), []byte(`{"name":"broken_pack"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidPackRoot, "roles", "broken_role", "role.json"), []byte(`{"role_id":"broken_role","execution_driver":"builtin_role","driver_ref":"builtin:pm","interaction_mode":"task","supported_ops":[{"name":"write_plan","op_id":"missing.write_plan"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile(role.json) error = %v", err)
	}

	zipPath := filepath.Join(t.TempDir(), "broken_pack.zip")
	writeZipFromDir(t, zipPath, invalidPackRoot)
	req = newMultipartRequestFromFile(t, http.MethodPost, "/api/plugins/upload-pack", "file", zipPath)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST upload-pack status=%d body=%s", resp.Code, resp.Body.String())
	}
	var invalidAccepted struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &invalidAccepted); err != nil {
		t.Fatalf("decode upload-pack response: %v", err)
	}
	waitForHTTPPluginValidationStatus(t, ctx, bootstrap, invalidAccepted.JobID, "failed")

	req = httptest.NewRequest(http.MethodGet, "/api/plugins/validations/"+invalidAccepted.JobID, nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET invalid plugin validation status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, `"status":"failed"`) || !strings.Contains(body, `"errors"`) {
		t.Fatalf("GET invalid plugin validation body = %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/plugins/registry-state", nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET plugin registry-state after failed upload status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); strings.Contains(body, `"broken_role"`) {
		t.Fatalf("registry-state polluted by invalid upload:\n%s", body)
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

func seedAppHTTPGitBranches(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: run.ProjectDir,
	}
	writeJSON := func(uri string, value any) []byte {
		t.Helper()
		content, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal artifact: %v", err)
		}
		if err := store.Write(ctx, uri, content); err != nil {
			t.Fatalf("write artifact %s: %v", uri, err)
		}
		return content
	}

	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	testPassed := true
	modulePayload := coderBranchArtifact{
		Kind:         "coder_branch",
		ModuleID:     "module01",
		ModuleName:   "Snake Game Frontend",
		ContainerID:  "container-real-01",
		BaseBranch:   "main",
		BaseCommit:   "basecommit123",
		Branch:       "feature/module01-frontend",
		Commit:       "commitmodule01",
		ChangedFiles: []string{"index.html"},
		Result:       "kok",
		TestPassed:   &testPassed,
	}
	moduleURI := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "coder01", "artifacts", "task_write_code", "coder_branch.json"))
	moduleContent := writeJSON(moduleURI, modulePayload)
	moduleObjectID := doujiagit.StableObjectID(moduleContent)
	moduleSnapshotID := doujiagit.StableSnapshotID(runID, "task_write_code", now)
	moduleBagID := doujiagit.StableBagID(runID, moduleSnapshotID, "coder_branch")

	repository := bootstrap.Internals.DoujiaGitRepository
	moduleObject, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   moduleObjectID,
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: moduleURI,
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertObject(module) error = %v", err)
	}
	moduleLogical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  "coder01",
		LogicalKey: "coder_branch",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact(module) error = %v", err)
	}
	moduleVersion, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: moduleLogical.LogicalArtifactID,
		ObjectIDs:         []string{moduleObject.ObjectID},
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion(module) error = %v", err)
	}
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              moduleBagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{moduleVersion.ArtifactVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module) error = %v", err)
	}
	if err := repository.CreateSnapshot(ctx, doujiagit.TaskSnapshot{
		SnapshotID:   moduleSnapshotID,
		RunID:        runID,
		TaskID:       "task_write_code",
		AgentRole:    core.AgentRoleCoder,
		AgentID:      "coder01",
		Op:           core.TaskOpWriteCode,
		Result:       core.TaskResultCodeOK,
		OutputBagIDs: []string{moduleBagID},
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateSnapshot(module) error = %v", err)
	}

	mergePayload := mergedMainBranchArtifact{
		Kind:           "merged_main_branch",
		BaseBranch:     "main",
		BaseCommit:     "basecommit123",
		ContainerID:    "container-real-01",
		MergedCommit:   "mergedcommit123",
		AppliedCommits: []string{"commitmodule01"},
		Modules:        []coderBranchArtifact{modulePayload},
		Result:         "kok",
		SchemaVersion:  2,
	}
	mergeTime := now.Add(time.Minute)
	mergeURI := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "architect01", "artifacts", "task_merge_code", "merged_main_branch.json"))
	mergeContent := writeJSON(mergeURI, mergePayload)
	mergeObjectID := doujiagit.StableObjectID(mergeContent)
	mergeSnapshotID := doujiagit.StableSnapshotID(runID, "task_merge_code", mergeTime)
	mergeBagID := doujiagit.StableBagID(runID, mergeSnapshotID, "merged_main_branch")
	mergeObject, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   mergeObjectID,
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: mergeURI,
		CreatedAt:  mergeTime,
	})
	if err != nil {
		t.Fatalf("UpsertObject(merge) error = %v", err)
	}
	mergeLogical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  "architect01",
		LogicalKey: "merged_main_branch",
		CreatedAt:  mergeTime,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact(merge) error = %v", err)
	}
	mergeVersion, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: mergeLogical.LogicalArtifactID,
		ObjectIDs:         []string{mergeObject.ObjectID},
		CreatedAt:         mergeTime,
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion(merge) error = %v", err)
	}
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              mergeBagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{mergeVersion.ArtifactVersionID},
		CreatedAt:          mergeTime,
	}); err != nil {
		t.Fatalf("CreateBag(merge) error = %v", err)
	}
	if err := repository.CreateSnapshot(ctx, doujiagit.TaskSnapshot{
		SnapshotID:   mergeSnapshotID,
		RunID:        runID,
		TaskID:       "task_merge_code",
		AgentRole:    core.AgentRoleArchitect,
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		Result:       core.TaskResultCodeOK,
		OutputBagIDs: []string{mergeBagID},
		CreatedAt:    mergeTime,
	}); err != nil {
		t.Fatalf("CreateSnapshot(merge) error = %v", err)
	}
}

func seedAppHTTPSessionData(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID, iterationNo int) {
	t.Helper()
	now := time.Date(2026, 5, 2, 10, 10, 0, 0, time.UTC)
	if bootstrap.Internals.RunIterationRepository != nil {
		iterations, err := bootstrap.Internals.RunIterationRepository.ListByRun(ctx, runID)
		if err != nil {
			t.Fatalf("ListByRun(run iterations) error = %v", err)
		}
		found := false
		for _, iteration := range iterations {
			if iteration.IterationNo == iterationNo {
				found = true
				break
			}
		}
		if !found {
			if err := bootstrap.Internals.RunIterationRepository.Create(ctx, repo.RunIterationRecord{
				RunID:       runID,
				IterationNo: iterationNo,
				Status:      core.RunStatusRunning,
				CreatedAt:   now,
				UpdatedAt:   now,
			}); err != nil {
				t.Fatalf("Create(run iteration) error = %v", err)
			}
		}
	}
	if err := bootstrap.Internals.SessionMessageRepository.Create(ctx, repo.SessionMessageRecord{
		ID:          "message_01",
		RunID:       runID,
		IterationNo: iterationNo,
		Role:        "ceo",
		MessageType: "chat",
		Content:     "need a better preview flow",
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(session message) error = %v", err)
	}
	if err := bootstrap.Internals.ImprovementItemRepository.Create(ctx, repo.ImprovementItemRecord{
		ItemID:      "item_01",
		RunID:       runID,
		IterationNo: iterationNo,
		Title:       "Improve preview area",
		Detail:      "support browser-like tabs",
		Source:      "ceo_summary",
		Status:      "open",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(improvement item) error = %v", err)
	}
}

func seedAppHTTPWorkspace(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	rootID := core.PipelineInstanceID("root")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:          rootID,
		RunID:       runID,
		PipelineID:  "phase_two_delivery_flow",
		InstanceKey: "root",
		Status:      core.PipelineInstanceStatusRunning,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}
	childID := core.PipelineInstanceID("root_test_all_modules_module01")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:                 childID,
		RunID:              runID,
		PipelineID:         "pipeline_module",
		ParentID:           &rootID,
		ParentTransitionID: "test_all_modules",
		InstanceKey:        "module01",
		Status:             core.PipelineInstanceStatusCompleted,
		CreatedAt:          now.Add(time.Minute),
		UpdatedAt:          now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(child instance) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "task_01",
		RunID:              runID,
		PipelineInstanceID: rootID,
		StageID:            "task_01",
		AgentRole:          core.AgentRolePM,
		AgentID:            "pm01",
		Op:                 "write_plan",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(root task) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root__pm_write_plan",
		RunID:              runID,
		PipelineInstanceID: rootID,
		StageID:            "pm_write_plan",
		AgentRole:          core.AgentRolePM,
		AgentID:            "pm01",
		Op:                 "write_plan",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now.Add(1 * time.Minute),
		UpdatedAt:          now.Add(1 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(pm_write_plan task) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root__architect_write_plan",
		RunID:              runID,
		PipelineInstanceID: rootID,
		StageID:            "architect_write_plan",
		AgentRole:          core.AgentRoleArchitect,
		AgentID:            "architect01",
		Op:                 "write_plan",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now.Add(90 * time.Second),
		UpdatedAt:          now.Add(90 * time.Second),
	}); err != nil {
		t.Fatalf("Create(architect_write_plan task) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root__architect_create_container",
		RunID:              runID,
		PipelineInstanceID: rootID,
		StageID:            "architect_create_container",
		AgentRole:          core.AgentRoleArchitect,
		AgentID:            "architect01",
		Op:                 "create_container",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now.Add(2 * time.Minute),
		UpdatedAt:          now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(architect_create_container task) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root__split_module",
		RunID:              runID,
		PipelineInstanceID: rootID,
		StageID:            "split_module",
		AgentRole:          core.AgentRoleArchitect,
		AgentID:            "architect01",
		Op:                 "split_module",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now.Add(150 * time.Second),
		UpdatedAt:          now.Add(150 * time.Second),
	}); err != nil {
		t.Fatalf("Create(split_module task) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root__acceptance",
		RunID:              runID,
		PipelineInstanceID: rootID,
		StageID:            "acceptance",
		AgentRole:          core.AgentRoleCEO,
		AgentID:            "ceo",
		Op:                 "acceptance_checkpoint",
		Status:             core.TaskStatusWaitingExternal,
		CreatedAt:          now.Add(3 * time.Minute),
		UpdatedAt:          now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(root acceptance task) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root_test_all_modules_module01_write_code_single_write_code",
		RunID:              runID,
		PipelineInstanceID: childID,
		StageID:            "write_code",
		AgentRole:          core.AgentRoleCoder,
		AgentID:            "coder01",
		Op:                 "write_code",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now.Add(4 * time.Minute),
		UpdatedAt:          now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(child task) error = %v", err)
	}
	writeCodeChildID := core.PipelineInstanceID("root_test_all_modules_module01_write_code_single")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:                 writeCodeChildID,
		RunID:              runID,
		PipelineID:         "pipeline_write_code",
		ParentID:           &childID,
		ParentTransitionID: "write_code",
		InstanceKey:        "single",
		Status:             core.PipelineInstanceStatusCompleted,
		CreatedAt:          now.Add(4 * time.Minute),
		UpdatedAt:          now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(write_code child instance) error = %v", err)
	}
	writeTestDataChildID := core.PipelineInstanceID("root_test_all_modules_module01_write_test_data_single")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:                 writeTestDataChildID,
		RunID:              runID,
		PipelineID:         "pipeline_write_test_data",
		ParentID:           &childID,
		ParentTransitionID: "write_test_data",
		InstanceKey:        "single",
		Status:             core.PipelineInstanceStatusCompleted,
		CreatedAt:          now.Add(4*time.Minute + 30*time.Second),
		UpdatedAt:          now.Add(5*time.Minute + 30*time.Second),
	}); err != nil {
		t.Fatalf("Create(write_test_data child instance) error = %v", err)
	}
	testCodeChildID := core.PipelineInstanceID("root_test_all_modules_module01_test_code_single")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:                 testCodeChildID,
		RunID:              runID,
		PipelineID:         "pipeline_test_code",
		ParentID:           &childID,
		ParentTransitionID: "test_code",
		InstanceKey:        "single",
		Status:             core.PipelineInstanceStatusRunning,
		CreatedAt:          now.Add(6 * time.Minute),
		UpdatedAt:          now.Add(6 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(test_code child instance) error = %v", err)
	}
	globalChildID := core.PipelineInstanceID("root_write_global_test_data_global")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:                 globalChildID,
		RunID:              runID,
		PipelineID:         "pipeline_global_test_data",
		ParentID:           &rootID,
		ParentTransitionID: "write_global_test_data",
		InstanceKey:        "global",
		Status:             core.PipelineInstanceStatusCompleted,
		CreatedAt:          now.Add(2*time.Minute + 30*time.Second),
		UpdatedAt:          now.Add(6 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(global child instance) error = %v", err)
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:                 "root_write_global_test_data_global_write_global_test_data",
		RunID:              runID,
		PipelineInstanceID: globalChildID,
		StageID:            "write_global_test_data",
		AgentRole:          core.AgentRoleArchitect,
		AgentID:            "architect01",
		Op:                 "test_data",
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		CreatedAt:          now.Add(2*time.Minute + 30*time.Second),
		UpdatedAt:          now.Add(6 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(global child task) error = %v", err)
	}
	if err := bootstrap.Internals.SessionArtifactRepository.Create(ctx, repo.SessionArtifactRecord{
		ID:          "artifact_requirement_doc_01",
		RunID:       runID,
		IterationNo: 1,
		Kind:        "requirement_doc",
		Title:       "Requirement v1",
		Content:     "# Requirement\n\nKeep the acceptance loop within one run.\n",
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(session artifact requirement_doc) error = %v", err)
	}
	if err := bootstrap.Internals.SessionArtifactRepository.Create(ctx, repo.SessionArtifactRecord{
		ID:          "artifact_pipeline_draft_01",
		RunID:       runID,
		IterationNo: 1,
		Kind:        "pipeline_draft",
		Title:       "Pipeline Draft v1",
		Content:     "{\n  \"pipeline_id\": \"workspace_preview_pipeline\"\n}\n",
		CreatedAt:   now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("Create(session artifact pipeline_draft) error = %v", err)
	}
}

func seedAppHTTPWorkspaceWithModuleChildPipelinesOnly(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	rootID := core.PipelineInstanceID("root")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:          rootID,
		RunID:       runID,
		PipelineID:  "phase_two_delivery_flow",
		InstanceKey: "root",
		Status:      core.PipelineInstanceStatusRunning,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}
	moduleID := core.PipelineInstanceID("root_test_all_modules_module01")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:                 moduleID,
		RunID:              runID,
		PipelineID:         "pipeline_module",
		ParentID:           &rootID,
		ParentTransitionID: "test_all_modules",
		InstanceKey:        "module01",
		Status:             core.PipelineInstanceStatusCompleted,
		CreatedAt:          now.Add(time.Minute),
		UpdatedAt:          now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(module instance) error = %v", err)
	}

	children := []struct {
		id         core.PipelineInstanceID
		pipelineID core.PipelineID
		transition string
		createdAt  time.Time
	}{
		{
			id:         "root_test_all_modules_module01_write_code_single",
			pipelineID: "pipeline_write_code",
			transition: "write_code",
			createdAt:  now.Add(4 * time.Minute),
		},
		{
			id:         "root_test_all_modules_module01_write_test_data_single",
			pipelineID: "pipeline_write_test_data",
			transition: "write_test_data",
			createdAt:  now.Add(4*time.Minute + 30*time.Second),
		},
		{
			id:         "root_test_all_modules_module01_test_code_single",
			pipelineID: "pipeline_test_code",
			transition: "test_code",
			createdAt:  now.Add(6 * time.Minute),
		},
	}
	for _, child := range children {
		if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
			ID:                 child.id,
			RunID:              runID,
			PipelineID:         child.pipelineID,
			ParentID:           &moduleID,
			ParentTransitionID: child.transition,
			InstanceKey:        "single",
			Status:             core.PipelineInstanceStatusCompleted,
			CreatedAt:          child.createdAt,
			UpdatedAt:          child.createdAt.Add(time.Minute),
		}); err != nil {
			t.Fatalf("Create(%s child instance) error = %v", child.transition, err)
		}
	}
}

func seedAppHTTPWorkspaceWithDetachedRootTasks(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	now := time.Date(2026, 5, 2, 13, 0, 0, 0, time.UTC)
	rootID := core.PipelineInstanceID("root")
	if err := bootstrap.Internals.InstanceRepository.Create(ctx, repo.PipelineInstanceRecord{
		ID:          rootID,
		RunID:       runID,
		PipelineID:  "phase_two_delivery_flow",
		InstanceKey: "root",
		Status:      core.PipelineInstanceStatusCompleted,
		CreatedAt:   now,
		UpdatedAt:   now.Add(20 * time.Minute),
	}); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}

	rootTasks := []repo.TaskRecord{
		{
			ID:        "ceo_write_requirement",
			RunID:     runID,
			StageID:   "ceo_write_requirement",
			AgentRole: core.AgentRoleCEO,
			AgentID:   "ceo",
			Op:        "write_plan",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "pm_write_plan",
			RunID:     runID,
			StageID:   "pm_write_plan",
			AgentRole: core.AgentRolePM,
			AgentID:   "pm01",
			Op:        "write_plan",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now.Add(1 * time.Minute),
			UpdatedAt: now.Add(1 * time.Minute),
		},
		{
			ID:        "ceo_review_product_plan",
			RunID:     runID,
			StageID:   "ceo_review_product_plan",
			AgentRole: core.AgentRoleCEO,
			AgentID:   "ceo",
			Op:        "review_plan",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now.Add(2 * time.Minute),
			UpdatedAt: now.Add(2 * time.Minute),
		},
		{
			ID:        "architect_write_plan",
			RunID:     runID,
			StageID:   "architect_write_plan",
			AgentRole: core.AgentRoleArchitect,
			AgentID:   "architect01",
			Op:        "write_plan",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now.Add(3 * time.Minute),
			UpdatedAt: now.Add(3 * time.Minute),
		},
		{
			ID:        "pm_review_architecture",
			RunID:     runID,
			StageID:   "pm_review_architecture",
			AgentRole: core.AgentRolePM,
			AgentID:   "pm01",
			Op:        "review_plan",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now.Add(4 * time.Minute),
			UpdatedAt: now.Add(4 * time.Minute),
		},
		{
			ID:        "architect_create_container",
			RunID:     runID,
			StageID:   "architect_create_container",
			AgentRole: core.AgentRoleArchitect,
			AgentID:   "architect01",
			Op:        "create_container",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now.Add(5 * time.Minute),
			UpdatedAt: now.Add(5 * time.Minute),
		},
		{
			ID:        "split_module",
			RunID:     runID,
			StageID:   "split_module",
			AgentRole: core.AgentRoleArchitect,
			AgentID:   "architect01",
			Op:        "split_module",
			Status:    core.TaskStatusDone,
			Result:    core.TaskResultCodeOK,
			CreatedAt: now.Add(6 * time.Minute),
			UpdatedAt: now.Add(6 * time.Minute),
		},
		{
			ID:        "acceptance_iter_01",
			RunID:     runID,
			StageID:   "acceptance",
			AgentRole: core.AgentRoleCEO,
			AgentID:   "ceo",
			Op:        "acceptance_checkpoint",
			Status:    core.TaskStatusWaitingExternal,
			CreatedAt: now.Add(15 * time.Minute),
			UpdatedAt: now.Add(15 * time.Minute),
		},
	}
	for _, task := range rootTasks {
		if err := bootstrap.Internals.TaskRepository.Create(ctx, task); err != nil {
			t.Fatalf("Create(root detached task %s) error = %v", task.ID, err)
		}
	}

	module01ID := core.PipelineInstanceID("root_test_all_modules_module01")
	module02ID := core.PipelineInstanceID("root_test_all_modules_module02")
	globalChildID := core.PipelineInstanceID("root_write_global_test_data_global")
	mergeChildID := core.PipelineInstanceID("root_merge_code_single")
	globalTestChildID := core.PipelineInstanceID("root_global_test_code_single")

	childInstances := []repo.PipelineInstanceRecord{
		{
			ID:                 module01ID,
			RunID:              runID,
			PipelineID:         "pipeline_module",
			ParentID:           &rootID,
			ParentTransitionID: "test_all_modules",
			InstanceKey:        "module01",
			Status:             core.PipelineInstanceStatusCompleted,
			CreatedAt:          now.Add(7 * time.Minute),
			UpdatedAt:          now.Add(12 * time.Minute),
		},
		{
			ID:                 module02ID,
			RunID:              runID,
			PipelineID:         "pipeline_module",
			ParentID:           &rootID,
			ParentTransitionID: "test_all_modules",
			InstanceKey:        "module02",
			Status:             core.PipelineInstanceStatusCompleted,
			CreatedAt:          now.Add(7*time.Minute + 10*time.Second),
			UpdatedAt:          now.Add(12 * time.Minute),
		},
		{
			ID:                 globalChildID,
			RunID:              runID,
			PipelineID:         "pipeline_global_test_data",
			ParentID:           &rootID,
			ParentTransitionID: "write_global_test_data",
			InstanceKey:        "global",
			Status:             core.PipelineInstanceStatusCompleted,
			CreatedAt:          now.Add(7*time.Minute + 20*time.Second),
			UpdatedAt:          now.Add(12 * time.Minute),
		},
		{
			ID:                 mergeChildID,
			RunID:              runID,
			PipelineID:         "pipeline_merge_code",
			ParentID:           &rootID,
			ParentTransitionID: "merge_code",
			InstanceKey:        "single",
			Status:             core.PipelineInstanceStatusCompleted,
			CreatedAt:          now.Add(13 * time.Minute),
			UpdatedAt:          now.Add(13 * time.Minute),
		},
		{
			ID:                 globalTestChildID,
			RunID:              runID,
			PipelineID:         "pipeline_global_test_code",
			ParentID:           &rootID,
			ParentTransitionID: "global_test_code",
			InstanceKey:        "single",
			Status:             core.PipelineInstanceStatusCompleted,
			CreatedAt:          now.Add(14 * time.Minute),
			UpdatedAt:          now.Add(14 * time.Minute),
		},
	}
	for _, instance := range childInstances {
		if err := bootstrap.Internals.InstanceRepository.Create(ctx, instance); err != nil {
			t.Fatalf("Create(child instance %s) error = %v", instance.ID, err)
		}
	}

	childTasks := []repo.TaskRecord{
		{
			ID:                 "root_test_all_modules_module01_test_code_single_test_code",
			RunID:              runID,
			PipelineInstanceID: module01ID,
			StageID:            "test_code",
			AgentRole:          core.AgentRoleTester,
			AgentID:            "tester01",
			Op:                 "test_code",
			Status:             core.TaskStatusDone,
			Result:             core.TaskResultCodeOK,
			CreatedAt:          now.Add(11 * time.Minute),
			UpdatedAt:          now.Add(11 * time.Minute),
		},
		{
			ID:                 "root_test_all_modules_module02_test_code_single_test_code",
			RunID:              runID,
			PipelineInstanceID: module02ID,
			StageID:            "test_code",
			AgentRole:          core.AgentRoleTester,
			AgentID:            "tester02",
			Op:                 "test_code",
			Status:             core.TaskStatusDone,
			Result:             core.TaskResultCodeOK,
			CreatedAt:          now.Add(11*time.Minute + 10*time.Second),
			UpdatedAt:          now.Add(11*time.Minute + 10*time.Second),
		},
		{
			ID:                 "root_write_global_test_data_global_write_global_test_data",
			RunID:              runID,
			PipelineInstanceID: globalChildID,
			StageID:            "write_global_test_data",
			AgentRole:          core.AgentRoleArchitect,
			AgentID:            "architect01",
			Op:                 "test_data",
			Status:             core.TaskStatusDone,
			Result:             core.TaskResultCodeOK,
			CreatedAt:          now.Add(10 * time.Minute),
			UpdatedAt:          now.Add(10 * time.Minute),
		},
		{
			ID:                 "root_merge_code_single_merge_code",
			RunID:              runID,
			PipelineInstanceID: mergeChildID,
			StageID:            "merge_code",
			AgentRole:          core.AgentRoleArchitect,
			AgentID:            "architect01",
			Op:                 "merge_code",
			Status:             core.TaskStatusDone,
			Result:             core.TaskResultCodeOK,
			CreatedAt:          now.Add(13 * time.Minute),
			UpdatedAt:          now.Add(13 * time.Minute),
		},
		{
			ID:                 "root_global_test_code_single_global_test_code",
			RunID:              runID,
			PipelineInstanceID: globalTestChildID,
			StageID:            "global_test_code",
			AgentRole:          core.AgentRoleArchitect,
			AgentID:            "architect01",
			Op:                 "test_code",
			Status:             core.TaskStatusDone,
			Result:             core.TaskResultCodeOK,
			CreatedAt:          now.Add(14 * time.Minute),
			UpdatedAt:          now.Add(14 * time.Minute),
		},
	}
	for _, task := range childTasks {
		if err := bootstrap.Internals.TaskRepository.Create(ctx, task); err != nil {
			t.Fatalf("Create(child task %s) error = %v", task.ID, err)
		}
	}
}

func waitForHTTPSessionMessages(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID, wantAtLeast int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
		if err == nil && len(items) >= wantAtLeast {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	items, err := bootstrap.Internals.SessionMessageRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	t.Fatalf("session messages len = %d, want at least %d", len(items), wantAtLeast)
}

func waitForHTTPPluginValidationStatus(t *testing.T, ctx context.Context, bootstrap *Bootstrap, jobID string, want agentbootstrap.ValidationStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result, err := bootstrap.PluginValidationResult(ctx, jobID)
		if err == nil && result.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	result, err := bootstrap.PluginValidationResult(ctx, jobID)
	if err != nil {
		t.Fatalf("PluginValidationResult(%s) error = %v", jobID, err)
	}
	t.Fatalf("plugin validation status = %s, want %s, errors=%v", result.Status, want, result.Errors)
}

func newMultipartRequest(t *testing.T, method string, path string, fieldName string, fileName string, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("multipart write error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func newMultipartRequestFromFile(t *testing.T, method string, path string, fieldName string, fullPath string) *http.Request {
	t.Helper()
	body, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", fullPath, err)
	}
	return newMultipartRequest(t, method, path, fieldName, filepath.Base(fullPath), string(body))
}

func writeZipFromDir(t *testing.T, zipPath string, dir string) {
	t.Helper()
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Create(%s) error = %v", zipPath, err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		entry, err := writer.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = entry.Write(body)
		return err
	})
	if err != nil {
		t.Fatalf("write zip from dir error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip writer close error = %v", err)
	}
}

func seedAppHTTPAwaitingAcceptanceRun(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	seedAppHTTPRun(t, ctx, bootstrap, runID)
	now := time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC)
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	run.Status = core.RunStatusAwaitingAcceptance
	run.CurrentIterationNo = 1
	run.LatestDeliveryFrontierID = "frontier_01"
	run.LatestAcceptanceCheckpointTaskID = "acceptance_iter_01"
	run.UpdatedAt = now
	if err := bootstrap.Internals.RunRepository.Update(ctx, run); err != nil {
		t.Fatalf("Update(run) error = %v", err)
	}
	iterations, err := bootstrap.Internals.RunIterationRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(run iterations) error = %v", err)
	}
	if len(iterations) == 0 {
		t.Fatalf("run iterations empty for %s", runID)
	}
	iterations[0].StartFrontierID = "frontier_00"
	iterations[0].DeliveryFrontierID = "frontier_01"
	iterations[0].AcceptanceCheckpointTaskID = "acceptance_iter_01"
	iterations[0].Status = core.RunStatusAwaitingAcceptance
	iterations[0].UpdatedAt = now
	if err := bootstrap.Internals.TaskRepository.Create(ctx, repo.TaskRecord{
		ID:        "acceptance_iter_01",
		RunID:     runID,
		StageID:   "acceptance",
		AgentRole: core.AgentRoleCEO,
		AgentID:   "ceo",
		Op:        "acceptance_checkpoint",
		Status:    core.TaskStatusWaitingExternal,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(acceptance task) error = %v", err)
	}
	seedAppHTTPSessionData(t, ctx, bootstrap, runID, 1)
}

func seedAppHTTPCheckpointTask(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID, taskID core.TaskID, status core.TaskStatus) {
	t.Helper()
	now := time.Date(2026, 5, 2, 10, 5, 0, 0, time.UTC)
	task := repo.TaskRecord{
		ID:        taskID,
		RunID:     runID,
		StageID:   core.StageID(taskID),
		AgentRole: core.AgentRoleCEO,
		AgentID:   "ceo",
		Op:        core.TaskOpReviewPlan,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if taskID == "task_06" {
		task.Op = "ceo_user_confirm"
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, task); err != nil {
		t.Fatalf("Create(checkpoint task) error = %v", err)
	}
}

func seedAppHTTPRun(t *testing.T, ctx context.Context, bootstrap *Bootstrap, runID core.RunID) {
	t.Helper()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	run := repo.RunRecord{
		ID:                 runID,
		PipelineID:         "phase_two_delivery_flow",
		Status:             core.RunStatusRunning,
		ProjectDir:         t.TempDir(),
		SessionID:          "session_api_http",
		CurrentIterationNo: 1,
		Config: core.RunConfig{Delivery: core.DeliveryConfig{
			MaxCoderAgents: 2,
			Git: core.GitRunConfig{
				RepoURL:    "https://example.com/repo.git",
				MainBranch: "main",
				BaseRef:    "main",
			},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := bootstrap.Internals.RunRepository.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if bootstrap.Internals.RunIterationRepository != nil {
		if err := bootstrap.Internals.RunIterationRepository.Create(ctx, repo.RunIterationRecord{
			RunID:       runID,
			IterationNo: 1,
			Status:      core.RunStatusRunning,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("Create(run iteration) error = %v", err)
		}
	}
	task := repo.TaskRecord{
		ID:                 "task_api_http",
		RunID:              runID,
		PipelineInstanceID: "root",
		StageID:            "task_01",
		AgentRole:          core.AgentRoleCEO,
		AgentID:            "ceo",
		Op:                 core.TaskOpWritePlan,
		Status:             core.TaskStatusDone,
		Result:             core.TaskResultCodeOK,
		OutputBagIDs:       []string{"bag_app_http"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := bootstrap.Internals.TaskRepository.Create(ctx, task); err != nil {
		t.Fatalf("Create(task) error = %v", err)
	}
	if err := bootstrap.Internals.ArtifactRepository.Create(ctx, repo.ArtifactRecord{
		ID:        "artifact_api_http",
		RunID:     runID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Kind:      "requirement",
		URI:       "projects/run_api_http/agents/ceo/artifacts/requirement/requirement_v1.md",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("Create(artifact) error = %v", err)
	}
	if err := bootstrap.Internals.EventRepository.Create(ctx, repo.EventRecord{
		ID:          "event_api_http",
		RunID:       runID,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		Type:        "task_done",
		Message:     "task completed",
		PayloadJSON: `{"result":"kok"}`,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(event) error = %v", err)
	}
}
