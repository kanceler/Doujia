package runtime

import (
	"context"
	"testing"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
)

func TestMessageGatewayResolvesInputBundle(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()

	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte("prd")),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "objects/sha256/prd",
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      "run_gateway",
		Namespace:  "pm01",
		LogicalKey: "plan",
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
	const bagID = "bag_pm_plan"
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              "run_gateway",
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}

	submitter := &recordingSubmitter{}
	gateway := NewMessageGateway(submitter, nil)
	gateway.SetInputBundleResolver(NewDoujiaGitInputResolver(repository))
	if err := gateway.Bind(ctx, "run_gateway", "pm01", "runtime_01"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if err := gateway.Dispatch(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionDispatch,
		RunID:       "run_gateway",
		TaskID:      "task_02",
		AgentID:     "pm01",
		Op:          core.TaskOpWritePlan,
		InputBagIDs: []string{bagID},
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if submitter.runtimeID != "runtime_01" {
		t.Fatalf("runtime id = %q, want runtime_01", submitter.runtimeID)
	}
	if submitter.task.InputBundle == nil {
		t.Fatalf("input bundle should be resolved")
	}
	if len(submitter.task.InputBundle.Bags) != 1 || submitter.task.InputBundle.Bags[0].BagID != bagID {
		t.Fatalf("input bags = %+v, want %s", submitter.task.InputBundle.Bags, bagID)
	}
	if len(submitter.task.InputBundle.Versions) != 1 {
		t.Fatalf("input versions count = %d, want 1", len(submitter.task.InputBundle.Versions))
	}
	gotVersion := submitter.task.InputBundle.Versions[0]
	if gotVersion.ArtifactVersionID != version.ArtifactVersionID {
		t.Fatalf("version id = %q, want %q", gotVersion.ArtifactVersionID, version.ArtifactVersionID)
	}
	if len(gotVersion.Objects) != 1 || gotVersion.Objects[0].StorageURI != object.StorageURI {
		t.Fatalf("version objects = %+v, want storage uri %s", gotVersion.Objects, object.StorageURI)
	}
	if len(submitter.task.ArtifactURIs) != 1 || submitter.task.ArtifactURIs[0] != object.StorageURI {
		t.Fatalf("artifact uris = %v, want [%s]", submitter.task.ArtifactURIs, object.StorageURI)
	}
}

func TestMessageGatewayResolvesNamedInputBagBindings(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()

	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte("module spec")),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "objects/sha256/module_spec",
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      "run_gateway",
		Namespace:  "architect01",
		LogicalKey: "module01_spec",
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
	const bagID = "bag_module01"
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              "run_gateway",
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}

	submitter := &recordingSubmitter{}
	gateway := NewMessageGateway(submitter, nil)
	gateway.SetInputBundleResolver(NewDoujiaGitInputResolver(repository))
	if err := gateway.Bind(ctx, "run_gateway", "coder01", "runtime_01"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if err := gateway.Dispatch(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     "run_gateway",
		TaskID:    "task_write_code",
		AgentID:   "coder01",
		Op:        core.TaskOpWriteCode,
		InputBags: []core.BagBindingRef{
			{Name: "module_input", BagID: bagID, Indexes: map[string]string{"module_key": "module01"}},
		},
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if submitter.task.InputBundle == nil {
		t.Fatalf("input bundle should be resolved")
	}
	if got := submitter.task.InputBundle.Bags[0].Name; got != "module_input" {
		t.Fatalf("input bag name = %q, want module_input", got)
	}
	if got := submitter.task.InputBundle.Bags[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("input bag module_key = %q, want module01", got)
	}
	if got := submitter.task.InputBags[0].Name; got != "module_input" {
		t.Fatalf("dispatched task input bag name = %q, want module_input", got)
	}
}

type recordingSubmitter struct {
	runtimeID core.RuntimeID
	task      core.TaskMetaData
}

func (s *recordingSubmitter) Submit(_ context.Context, runtimeID core.RuntimeID, task core.TaskMetaData) error {
	s.runtimeID = runtimeID
	s.task = task
	return nil
}
