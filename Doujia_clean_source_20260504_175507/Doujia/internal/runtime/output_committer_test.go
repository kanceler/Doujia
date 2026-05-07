package runtime

import (
	"context"
	"strings"
	"testing"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
)

func TestDoujiaGitOutputCommitterCommitsArtifactRefs(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()
	const outputRef = "projects/run_commit/agents/ceo/artifacts/requirement/requirement_v1.md"
	store := mapArtifactStore{
		files: map[string][]byte{
			outputRef: []byte("# Requirement\n"),
		},
	}
	committer := NewDoujiaGitOutputCommitter(repository, store)

	receipt, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:   "run_commit",
		AgentID: "ceo",
		TaskID:  "task_01",
		Op:      core.TaskOpWritePlan,
		Outputs: []core.AgentOutput{
			{LogicalKey: agentcore.LKRequirement, ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "requirement",
				Members: []core.ProducedBagMember{
					{LogicalKey: agentcore.LKRequirement},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CommitOutputs() error = %v", err)
	}
	if receipt.Result != core.TaskResultCodeOK {
		t.Fatalf("receipt result = %s, want %s", receipt.Result, core.TaskResultCodeOK)
	}
	if got, want := len(receipt.ProducedBags), 1; got != want {
		t.Fatalf("produced bags = %d, want %d", got, want)
	}
	if receipt.ProducedBags[0].Name != "requirement" {
		t.Fatalf("produced bag name = %q, want requirement", receipt.ProducedBags[0].Name)
	}
	if got, want := len(receipt.ProducedBags[0].ArtifactVersionIDs), 1; got != want {
		t.Fatalf("produced bag version ids = %d, want %d", got, want)
	}
	versionID := receipt.ProducedBags[0].ArtifactVersionIDs[0]
	version, err := repository.GetArtifactVersion(ctx, versionID)
	if err != nil {
		t.Fatalf("GetArtifactVersion() error = %v", err)
	}
	wantLogicalID := doujiagit.StableLogicalArtifactID("run_commit", "ceo", "requirement")
	if version.LogicalArtifactID != wantLogicalID {
		t.Fatalf("logical id = %q, want %q", version.LogicalArtifactID, wantLogicalID)
	}
	object, err := repository.GetObject(ctx, version.ObjectIDs[0])
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	if object.StorageURI != outputRef {
		t.Fatalf("object storage uri = %q, want %q", object.StorageURI, outputRef)
	}

	reused, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:   "run_commit",
		AgentID: "ceo",
		TaskID:  "task_01_retry",
		Op:      core.TaskOpWritePlan,
		Outputs: []core.AgentOutput{
			{LogicalKey: agentcore.LKRequirement, ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "requirement",
				Members: []core.ProducedBagMember{
					{LogicalKey: agentcore.LKRequirement},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CommitOutputs(reuse) error = %v", err)
	}
	if reused.ProducedBags[0].ArtifactVersionIDs[0] != versionID {
		t.Fatalf("reused produced bag version id = %q, want %q", reused.ProducedBags[0].ArtifactVersionIDs[0], versionID)
	}
}

func TestDoujiaGitOutputCommitterCommitsExplicitProducedBags(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()
	const outputRef = "projects/run_commit/agents/architect01/artifacts/create_container/container_context.json"
	store := mapArtifactStore{
		files: map[string][]byte{
			outputRef: []byte(`{"container_id":"ctn_01"}`),
		},
	}
	committer := NewDoujiaGitOutputCommitter(repository, store)

	receipt, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:   "run_commit",
		AgentID: "architect01",
		TaskID:  "task_02",
		Op:      core.TaskOpCreateContainer,
		Outputs: []core.AgentOutput{
			{
				LogicalKey:  "container_context",
				ObjectType:  "json",
				Status:      "produced",
				ArtifactURI: outputRef,
			},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "container_context",
				Members: []core.ProducedBagMember{
					{LogicalKey: "architecture_plan", ArtifactVersionID: "version:architecture"},
					{LogicalKey: "environment_spec", ArtifactVersionID: "version:environment"},
					{LogicalKey: "container_context"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CommitOutputs() error = %v", err)
	}

	if got, want := len(receipt.ProducedBags), 1; got != want {
		t.Fatalf("produced bags = %d, want %d", got, want)
	}
	bag := receipt.ProducedBags[0]
	if bag.Name != "container_context" {
		t.Fatalf("bag name = %q, want container_context", bag.Name)
	}
	if got, want := len(bag.ArtifactVersionIDs), 3; got != want {
		t.Fatalf("artifact version ids = %d, want %d", got, want)
	}
	if !containsString(bag.ArtifactVersionIDs, "version:architecture") {
		t.Fatalf("artifact version ids = %v, want version:architecture", bag.ArtifactVersionIDs)
	}
	if !containsString(bag.ArtifactVersionIDs, "version:environment") {
		t.Fatalf("artifact version ids = %v, want version:environment", bag.ArtifactVersionIDs)
	}
	if !containsString(bag.MemberLogicalKeys, "container_context") {
		t.Fatalf("member logical keys = %v, want container_context", bag.MemberLogicalKeys)
	}
}

func TestDoujiaGitOutputCommitterRejectsOutputsWithoutProducedBags(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()
	const outputRef = "projects/run_commit/agents/ceo/artifacts/requirement/requirement_v1.md"
	store := mapArtifactStore{
		files: map[string][]byte{
			outputRef: []byte("# Requirement\n"),
		},
	}
	committer := NewDoujiaGitOutputCommitter(repository, store)

	_, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:   "run_commit",
		AgentID: "ceo",
		TaskID:  "task_01",
		Op:      core.TaskOpWritePlan,
		Outputs: []core.AgentOutput{
			{LogicalKey: agentcore.LKRequirement, ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
	})
	if err == nil {
		t.Fatal("CommitOutputs() error = nil, want missing produced_bags")
	}
	if !strings.Contains(err.Error(), "produced_bags") {
		t.Fatalf("CommitOutputs() error = %v, want produced_bags", err)
	}
}

func TestDoujiaGitOutputCommitterRejectsProducedOutputWithoutLogicalKey(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()
	const outputRef = "projects/run_commit/agents/ceo/artifacts/requirement/requirement_v1.md"
	store := mapArtifactStore{
		files: map[string][]byte{
			outputRef: []byte("# Requirement\n"),
		},
	}
	committer := NewDoujiaGitOutputCommitter(repository, store)

	_, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:   "run_commit",
		AgentID: "ceo",
		TaskID:  "task_01",
		Op:      core.TaskOpWritePlan,
		Outputs: []core.AgentOutput{
			{ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "requirement",
				Members: []core.ProducedBagMember{
					{LogicalKey: agentcore.LKRequirement},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("CommitOutputs() error = nil, want missing logical_key rejection")
	}
	if !strings.Contains(err.Error(), "logical_key") {
		t.Fatalf("CommitOutputs() error = %v, want logical_key detail", err)
	}
}

func TestDoujiaGitOutputCommitterPreservesProducedBagIndexes(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()
	const outputRef = "projects/run_commit/agents/architect01/artifacts/split_module/module01_spec.json"
	store := mapArtifactStore{
		files: map[string][]byte{
			outputRef: []byte(`{"module_id":"module01"}`),
		},
	}
	committer := NewDoujiaGitOutputCommitter(repository, store)

	receipt, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:   "run_commit",
		AgentID: "architect01",
		TaskID:  "task_split",
		Op:      core.TaskOpSplitModule,
		Outputs: []core.AgentOutput{
			{LogicalKey: "module_spec.module01", ObjectType: "json", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name:    "module_input",
				Indexes: map[string]string{"module_key": "module01"},
				Members: []core.ProducedBagMember{
					{LogicalKey: "module_spec.module01"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CommitOutputs() error = %v", err)
	}
	if got := receipt.ProducedBags[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("produced bag indexes = %#v, want module_key module01", receipt.ProducedBags[0].Indexes)
	}
}

func TestCommitAgentFeedbackOutputsCommitsProducedBagsFromFeedback(t *testing.T) {
	ctx := context.Background()
	repository := doujiagit.NewMemoryRepository()
	ownedPlan := "projects/run_commit/agents/architect01/artifacts/module/module_plan_v1.json"
	store := mapArtifactStore{
		files: map[string][]byte{
			ownedPlan: []byte(`{"modules":["module01"]}`),
		},
	}
	committer := NewDoujiaGitOutputCommitter(repository, store)
	feedback, err := CommitAgentFeedbackOutputs(ctx, committer, "run_commit", "architect01", core.TaskMetaData{
		TaskID: "task_03",
		Op:     core.TaskOpSplitModule,
	}, core.TaskMetaData{
		Result: core.TaskResultCodeOK,
		Outputs: []core.AgentOutput{
			{LogicalKey: agentcore.LKModuleSpecs, ObjectType: "json", Status: "produced", ArtifactURI: ownedPlan},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "global_test_input",
				Members: []core.ProducedBagMember{
					{LogicalKey: agentcore.LKModuleSpecs},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CommitAgentFeedbackOutputs() error = %v", err)
	}
	if feedback.Commit == nil {
		t.Fatalf("CommitAgentFeedbackOutputs() did not attach commit receipt")
	}
	got := strings.Join(feedback.Commit.MaterializedOutputRefs, "|")
	want := ownedPlan
	if got != want {
		t.Fatalf("materialized refs = %q, want %q", got, want)
	}
	if got, want := len(feedback.Commit.ProducedBags), 1; got != want {
		t.Fatalf("produced bags = %d, want %d", got, want)
	}
}

type mapArtifactStore struct {
	files map[string][]byte
}

func (s mapArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	return append([]byte(nil), s.files[uri]...), nil
}

func (s mapArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.files == nil {
		s.files = make(map[string][]byte)
	}
	s.files[uri] = append([]byte(nil), content...)
	return nil
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
