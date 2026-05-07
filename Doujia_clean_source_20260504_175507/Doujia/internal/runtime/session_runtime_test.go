package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
)

type sessionTestFactory struct {
	inits []AgentInit
}

func (f *sessionTestFactory) Create(init AgentInit, _ AgentDeps) Agent {
	f.inits = append(f.inits, init)
	return sessionTestAgent{}
}

func (f *sessionTestFactory) Execute(context.Context, core.TaskMetaData) (core.TaskMetaData, error) {
	return core.TaskMetaData{}, nil
}

type sessionTestAgent struct{}

type sessionEchoAgent struct{}

type recordingFeedbackSink struct {
	feedback core.TaskMetaData
}

func (s *recordingFeedbackSink) OnFeedback(_ context.Context, feedback core.TaskMetaData) error {
	s.feedback = feedback
	return nil
}

func (sessionTestAgent) Create(AgentInit, AgentDeps) Agent {
	return sessionTestAgent{}
}

func (sessionTestAgent) Execute(context.Context, core.TaskMetaData) (core.TaskMetaData, error) {
	return core.TaskMetaData{}, nil
}

func (sessionEchoAgent) Create(AgentInit, AgentDeps) Agent {
	return sessionEchoAgent{}
}

func (sessionEchoAgent) Execute(_ context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	task.Direction = core.TaskDirectionFeedback
	task.Result = core.TaskResultCodeOK
	return task, nil
}

func TestInMemorySessionRuntimeCreatesConfiguredSessionRole(t *testing.T) {
	t.Parallel()

	factory := &sessionTestFactory{}
	sessionRuntime := NewInMemorySessionRuntime(SessionTemplate{
		Role:    "lead",
		AgentID: "lead01",
		Factory: factory,
	}, nil, AgentRuntimeConfig{})

	run := core.PipelineRun{
		ID:         "run_session_template",
		ProjectDir: t.TempDir(),
	}

	session, err := sessionRuntime.CreateSession(context.Background(), run)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if session.Role != "lead" {
		t.Fatalf("session role = %q, want lead", session.Role)
	}
	if session.AgentID != "lead01" {
		t.Fatalf("session agent_id = %q, want lead01", session.AgentID)
	}
	if got, want := session.WorkspacePath, filepath.Join(run.ProjectDir, "agents", "lead01"); got != want {
		t.Fatalf("workspace = %q, want %q", got, want)
	}
	if got, want := len(factory.inits), 1; got != want {
		t.Fatalf("factory init count = %d, want %d", got, want)
	}
	if factory.inits[0].AgentID != "lead01" {
		t.Fatalf("factory init agent_id = %q, want lead01", factory.inits[0].AgentID)
	}
	if factory.inits[0].RuntimeID != "run_session_template_lead01_runtime" {
		t.Fatalf("factory init runtime_id = %q, want run_session_template_lead01_runtime", factory.inits[0].RuntimeID)
	}
}

func TestInMemorySessionRuntimeResolvesInputBundleBeforeDispatch(t *testing.T) {
	t.Parallel()

	repository := doujiagit.NewMemoryRepository()
	ctx := context.Background()

	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte("requirement")),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "projects/run_session/system/requirement.md",
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      "run_session",
		Namespace:  "lead01",
		LogicalKey: "requirement",
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	const bagID = "bag_requirement"
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              "run_session",
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}

	sink := &recordingFeedbackSink{}
	sessionRuntime := NewInMemorySessionRuntime(SessionTemplate{
		Role:    "lead",
		AgentID: "lead01",
		Factory: sessionEchoAgent{},
	}, nil, AgentRuntimeConfig{})
	sessionRuntime.SetFeedbackSink(sink)
	sessionRuntime.SetInputBundleResolver(NewDoujiaGitInputResolver(repository))

	run := core.PipelineRun{
		ID:         "run_session",
		ProjectDir: t.TempDir(),
	}
	if _, err := sessionRuntime.CreateSession(ctx, run); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := sessionRuntime.DispatchToSession(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionDispatch,
		RunID:       run.ID,
		TaskID:      "task_session",
		AgentID:     "lead01",
		Op:          core.TaskOpWritePlan,
		InputBagIDs: []string{bagID},
	}); err != nil {
		t.Fatalf("DispatchToSession() error = %v", err)
	}

	if sink.feedback.RunID != run.ID {
		t.Fatalf("feedback run_id = %q, want %q", sink.feedback.RunID, run.ID)
	}
	if sink.feedback.InputBundle == nil {
		t.Fatal("feedback should carry resolved input bundle")
	}
	if len(sink.feedback.InputBundle.Bags) != 1 || sink.feedback.InputBundle.Bags[0].BagID != bagID {
		t.Fatalf("resolved input bags = %+v, want %s", sink.feedback.InputBundle.Bags, bagID)
	}
	if len(sink.feedback.ArtifactURIs) != 1 || sink.feedback.ArtifactURIs[0] != object.StorageURI {
		t.Fatalf("artifact uris = %v, want [%s]", sink.feedback.ArtifactURIs, object.StorageURI)
	}
}
