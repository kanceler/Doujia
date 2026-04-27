package flowdemo

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRunAndArtifactEndpoints(t *testing.T) {
	service := NewService(t.TempDir())
	server := httptest.NewServer(NewHandler(service))
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "requirement.md")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("# Requirement\n\nHTTP uploaded requirement.")); err != nil {
		t.Fatalf("write multipart: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/runs", &body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/runs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", resp.StatusCode)
	}
	var run Run
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if len(run.Steps) == 0 || len(run.Steps[0].Outputs) == 0 {
		t.Fatalf("run missing first step output: %+v", run)
	}

	artifactURL := server.URL + "/api/artifacts?run_id=" + run.ID + "&uri=" + run.Steps[0].Outputs[0]
	artifactResp, err := http.Get(artifactURL)
	if err != nil {
		t.Fatalf("GET /api/artifacts: %v", err)
	}
	defer artifactResp.Body.Close()
	if artifactResp.StatusCode != http.StatusOK {
		t.Fatalf("artifact status = %d, want 200", artifactResp.StatusCode)
	}
	var artifact ArtifactContent
	if err := json.NewDecoder(artifactResp.Body).Decode(&artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	if artifact.Content == "" {
		t.Fatal("artifact content is empty")
	}
}
