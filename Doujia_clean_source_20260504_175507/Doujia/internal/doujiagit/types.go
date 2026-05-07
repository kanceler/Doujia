package doujiagit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"devflow/internal/core"
)

const (
	ObjectTypeBlob      = "blob"
	ObjectTypeGitCommit = "git_commit"
	ObjectTypeManifest  = "manifest"

	DefaultRefName = "main"

	RefMoveModeAdvance  = "advance"
	RefMoveModeRecover  = "recover"
	RefMoveModeFailRun  = "fail_run"
	RefMoveModeCheckout = "checkout"
)

var ErrRefCASConflict = errors.New("ref compare-and-swap conflict")

type ArtifactObject struct {
	ObjectID   string    `json:"object_id"`
	ObjectType string    `json:"object_type"`
	StorageURI string    `json:"storage_uri,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type LogicalArtifact struct {
	LogicalArtifactID string     `json:"logical_artifact_id"`
	RunID             core.RunID `json:"run_id"`
	Namespace         string     `json:"namespace"`
	LogicalKey        string     `json:"logical_key"`
	CreatedAt         time.Time  `json:"created_at"`
}

type ArtifactVersion struct {
	ArtifactVersionID string    `json:"artifact_version_id"`
	LogicalArtifactID string    `json:"logical_artifact_id"`
	ObjectIDs         []string  `json:"object_ids"`
	CreatedAt         time.Time `json:"created_at"`
}

type ArtifactBag struct {
	BagID              string     `json:"bag_id"`
	RunID              core.RunID `json:"run_id"`
	ArtifactVersionIDs []string   `json:"artifact_version_ids"`
	CreatedAt          time.Time  `json:"created_at"`
}

type TaskSnapshot struct {
	SnapshotID         string                  `json:"snapshot_id"`
	RunID              core.RunID              `json:"run_id"`
	TaskID             core.TaskID             `json:"task_id"`
	PipelineInstanceID core.PipelineInstanceID `json:"pipeline_instance_id,omitempty"`
	TransitionID       core.StageID            `json:"transition_id,omitempty"`
	AgentRole          core.AgentRole          `json:"agent_role,omitempty"`
	AgentID            core.AgentID            `json:"agent_id,omitempty"`
	Op                 string                  `json:"op,omitempty"`
	Result             core.TaskResultCode     `json:"result"`
	InputBagIDs        []string                `json:"input_bag_ids"`
	OutputBagIDs       []string                `json:"output_bag_ids"`
	DiagnosticsJSON    string                  `json:"diagnostics_json,omitempty"`
	RuntimeContextJSON string                  `json:"runtime_context_json,omitempty"`
	CreatedAt          time.Time               `json:"created_at"`
}

type FrontierSnapshot struct {
	FrontierSnapshotID        string     `json:"frontier_snapshot_id"`
	RunID                     core.RunID `json:"run_id"`
	ParentFrontierSnapshotIDs []string   `json:"parent_frontier_snapshot_ids,omitempty"`
	TaskSnapshotIDs           []string   `json:"task_snapshot_ids"`
	CreatedByMode             string     `json:"created_by_mode,omitempty"`
	CreatedByEventID          string     `json:"created_by_event_id,omitempty"`
	DetailsJSON               string     `json:"details_json,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
}

type Ref struct {
	RefName             string     `json:"ref_name"`
	RunID               core.RunID `json:"run_id"`
	FrontierSnapshotID  string     `json:"frontier_snapshot_id,omitempty"`
	FrontierSnapshotIDs []string   `json:"frontier_snapshot_ids"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type RefMoveEvent struct {
	EventID                 string     `json:"event_id"`
	RunID                   core.RunID `json:"run_id"`
	RefName                 string     `json:"ref_name"`
	FromFrontierSnapshotIDs []string   `json:"from_frontier_snapshot_ids,omitempty"`
	ToFrontierSnapshotIDs   []string   `json:"to_frontier_snapshot_ids"`
	Mode                    string     `json:"mode"`
	Reason                  string     `json:"reason,omitempty"`
	DetailsJSON             string     `json:"details_json,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
}

type MoveRefRequest struct {
	Ref                        Ref
	ExpectedFrontierSnapshotID string
	Event                      RefMoveEvent
}

type CommitReceipt = core.CommitReceipt

func StableObjectID(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func StableLogicalArtifactID(runID core.RunID, namespace string, logicalKey string) string {
	raw := strings.Join([]string{
		string(runID),
		strings.TrimSpace(namespace),
		strings.TrimSpace(logicalKey),
	}, "\x00")
	return "logical:" + shortHash(raw)
}

func StableArtifactVersionID(logicalArtifactID string, objectIDs []string) string {
	normalized := NormalizeIDs(objectIDs)
	raw := strings.TrimSpace(logicalArtifactID) + "\x00" + strings.Join(normalized, "\x00")
	return "version:" + shortHash(raw)
}

func StableBagID(runID core.RunID, snapshotID string, outputKey string) string {
	raw := strings.Join([]string{
		string(runID),
		strings.TrimSpace(snapshotID),
		strings.TrimSpace(outputKey),
	}, "\x00")
	return "bag:" + shortHash(raw)
}

func StableSnapshotID(runID core.RunID, taskID core.TaskID, createdAt time.Time) string {
	raw := strings.Join([]string{
		string(runID),
		string(taskID),
		createdAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	return "snapshot:" + shortHash(raw)
}

func StableFrontierSnapshotID(runID core.RunID, taskSnapshotIDs []string, parentFrontierSnapshotIDs []string, createdAt time.Time) string {
	raw := strings.Join([]string{
		string(runID),
		strings.Join(NormalizeIDs(taskSnapshotIDs), "\x00"),
		strings.Join(NormalizeIDs(parentFrontierSnapshotIDs), "\x00"),
		createdAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	return "frontier:" + shortHash(raw)
}

func StableRefMoveEventID(runID core.RunID, refName string, fromFrontierSnapshotIDs []string, toFrontierSnapshotIDs []string, mode string, createdAt time.Time) string {
	raw := strings.Join([]string{
		string(runID),
		strings.TrimSpace(refName),
		strings.Join(NormalizeIDs(fromFrontierSnapshotIDs), "\x00"),
		strings.Join(NormalizeIDs(toFrontierSnapshotIDs), "\x00"),
		strings.TrimSpace(mode),
		createdAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	return "refmove:" + shortHash(raw)
}

func NormalizeIDs(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
