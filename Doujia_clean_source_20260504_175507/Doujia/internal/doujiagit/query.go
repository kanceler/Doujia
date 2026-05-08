package doujiagit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"devflow/internal/core"
)

type RunGraph struct {
	RunID               core.RunID                       `json:"run_id"`
	Ref                 RefView                          `json:"ref"`
	Refs                []RefView                        `json:"refs,omitempty"`
	FrontierSnapshots   []FrontierSnapshotView           `json:"frontier_snapshots,omitempty"`
	RefMoveEvents       []RefMoveEventView               `json:"ref_move_events,omitempty"`
	ProcessingDecisions []SnapshotProcessingDecisionView `json:"processing_decisions,omitempty"`
	Snapshots           []SnapshotView                   `json:"snapshots"`
	Bags                []BagView                        `json:"bags,omitempty"`
	HistorySnapshots    []SnapshotView                   `json:"history_snapshots,omitempty"`
	HistoryBags         []BagView                        `json:"history_bags,omitempty"`
	HistoryFrontiers    []FrontierSnapshotView           `json:"history_frontier_snapshots,omitempty"`
}

type RefView struct {
	RefName                   string    `json:"ref_name"`
	FrontierSnapshotID        string    `json:"frontier_snapshot_id,omitempty"`
	FrontierMemberSnapshotIDs []string  `json:"frontier_member_snapshot_ids,omitempty"`
	FrontierSnapshotIDs       []string  `json:"frontier_snapshot_ids"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type SnapshotView struct {
	SnapshotID                 string                  `json:"snapshot_id"`
	RunID                      core.RunID              `json:"run_id"`
	TaskID                     core.TaskID             `json:"task_id"`
	LogicalSnapshotID          string                  `json:"logical_snapshot_id,omitempty"`
	SnapshotVersionID          string                  `json:"snapshot_version_id,omitempty"`
	SnapshotVersionNo          int                     `json:"snapshot_version_no,omitempty"`
	ArrivalKind                string                  `json:"arrival_kind,omitempty"`
	BranchKind                 string                  `json:"branch_kind,omitempty"`
	BranchFromSnapshotID       string                  `json:"branch_from_snapshot_id,omitempty"`
	RecoverFromSnapshotID      string                  `json:"recover_from_snapshot_id,omitempty"`
	RecoverTargetSnapshotIDs   []string                `json:"recover_target_snapshot_ids,omitempty"`
	ReusableSnapshotIDs        []string                `json:"reusable_snapshot_ids,omitempty"`
	RecoverAnchorSnapshotIDs   []string                `json:"recover_anchor_snapshot_ids,omitempty"`
	PreviousAttemptSnapshotIDs []string                `json:"previous_attempt_snapshot_ids,omitempty"`
	FailureReportBagIDs        []string                `json:"failure_report_bag_ids,omitempty"`
	PreviousOutputBagIDs       []string                `json:"previous_output_bag_ids,omitempty"`
	RepairTargetTransitionID   string                  `json:"repair_target_transition_id,omitempty"`
	RepairTargetTaskID         string                  `json:"repair_target_task_id,omitempty"`
	PipelineInstanceID         core.PipelineInstanceID `json:"pipeline_instance_id,omitempty"`
	TransitionID               core.StageID            `json:"transition_id,omitempty"`
	AgentRole                  core.AgentRole          `json:"agent_role,omitempty"`
	AgentID                    core.AgentID            `json:"agent_id,omitempty"`
	Op                         string                  `json:"op,omitempty"`
	Result                     core.TaskResultCode     `json:"result"`
	InputBagIDs                []string                `json:"input_bag_ids,omitempty"`
	OutputBagIDs               []string                `json:"output_bag_ids,omitempty"`
	InputBags                  []BagView               `json:"input_bags,omitempty"`
	OutputBags                 []BagView               `json:"output_bags,omitempty"`
	DiagnosticsJSON            string                  `json:"diagnostics_json,omitempty"`
	RuntimeContextJSON         string                  `json:"runtime_context_json,omitempty"`
	CreatedAt                  time.Time               `json:"created_at"`
}

type FrontierSnapshotView struct {
	FrontierSnapshotID        string     `json:"frontier_snapshot_id"`
	RunID                     core.RunID `json:"run_id"`
	ParentFrontierSnapshotIDs []string   `json:"parent_frontier_snapshot_ids,omitempty"`
	TaskSnapshotIDs           []string   `json:"task_snapshot_ids"`
	CreatedByMode             string     `json:"created_by_mode,omitempty"`
	CreatedByEventID          string     `json:"created_by_event_id,omitempty"`
	DetailsJSON               string     `json:"details_json,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
}

type RefMoveEventView struct {
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

type SnapshotProcessingDecisionView struct {
	DecisionID                  string     `json:"decision_id"`
	RunID                       core.RunID `json:"run_id"`
	RefName                     string     `json:"ref_name"`
	SnapshotID                  string     `json:"snapshot_id"`
	SnapshotVersionID           string     `json:"snapshot_version_id,omitempty"`
	Status                      string     `json:"status"`
	DecisionKind                string     `json:"decision_kind,omitempty"`
	ContinuationID              string     `json:"continuation_id,omitempty"`
	Reason                      string     `json:"reason,omitempty"`
	ProducedTaskIDs             []string   `json:"produced_task_ids,omitempty"`
	ProducedPipelineInstanceIDs []string   `json:"produced_pipeline_instance_ids,omitempty"`
	ConsumedSnapshotIDs         []string   `json:"consumed_snapshot_ids,omitempty"`
	ProducedSnapshotIDs         []string   `json:"produced_snapshot_ids,omitempty"`
	FromFrontierSnapshotID      string     `json:"from_frontier_snapshot_id,omitempty"`
	ToFrontierSnapshotID        string     `json:"to_frontier_snapshot_id,omitempty"`
	RecoverTargetSnapshotIDs    []string   `json:"recover_target_snapshot_ids,omitempty"`
	ReusableSnapshotIDs         []string   `json:"reusable_snapshot_ids,omitempty"`
	RecoverAnchorSnapshotIDs    []string   `json:"recover_anchor_snapshot_ids,omitempty"`
	FailedSnapshotID            string     `json:"failed_snapshot_id,omitempty"`
	PreviousAttemptSnapshotIDs  []string   `json:"previous_attempt_snapshot_ids,omitempty"`
	FailureReportBagIDs         []string   `json:"failure_report_bag_ids,omitempty"`
	PreviousOutputBagIDs        []string   `json:"previous_output_bag_ids,omitempty"`
	RepairTargetTransitionID    string     `json:"repair_target_transition_id,omitempty"`
	RepairTargetTaskID          string     `json:"repair_target_task_id,omitempty"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

type BagView struct {
	BagID               string        `json:"bag_id"`
	RunID               core.RunID    `json:"run_id"`
	ArtifactVersionIDs  []string      `json:"artifact_version_ids"`
	Versions            []VersionView `json:"versions,omitempty"`
	ProducerSnapshotID  string        `json:"producer_snapshot_id,omitempty"`
	ConsumerSnapshotIDs []string      `json:"consumer_snapshot_ids,omitempty"`
	CreatedAt           time.Time     `json:"created_at"`
}

type VersionView struct {
	ArtifactVersionID string              `json:"artifact_version_id"`
	LogicalArtifactID string              `json:"logical_artifact_id"`
	LogicalArtifact   LogicalArtifactView `json:"logical_artifact"`
	ObjectIDs         []string            `json:"object_ids"`
	Objects           []ObjectView        `json:"objects,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
}

type LogicalArtifactView struct {
	LogicalArtifactID string     `json:"logical_artifact_id"`
	RunID             core.RunID `json:"run_id"`
	Namespace         string     `json:"namespace"`
	LogicalKey        string     `json:"logical_key"`
	CreatedAt         time.Time  `json:"created_at"`
}

type ObjectView struct {
	ObjectID   string    `json:"object_id"`
	ObjectType string    `json:"object_type"`
	StorageURI string    `json:"storage_uri,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func BuildRunGraph(ctx context.Context, repository Repository, runID core.RunID, refName string) (RunGraph, error) {
	if repository == nil {
		return RunGraph{}, fmt.Errorf("doujia git repository is required")
	}
	if strings.TrimSpace(string(runID)) == "" {
		return RunGraph{}, fmt.Errorf("run_id is required")
	}
	ref, err := repository.GetRef(ctx, runID, refName)
	if err != nil {
		return RunGraph{}, err
	}
	snapshots, err := repository.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		return RunGraph{}, err
	}
	bags, err := repository.ListBagsByRun(ctx, runID)
	if err != nil {
		return RunGraph{}, err
	}
	frontiers, err := repository.ListFrontierSnapshotsByRun(ctx, runID)
	if err != nil {
		return RunGraph{}, err
	}
	reachableFrontiers, reachableSnapshots, err := reachableGraphIDs(ctx, repository, ref, frontiers)
	if err != nil {
		return RunGraph{}, err
	}
	refs, err := repository.ListRefsByRun(ctx, runID)
	if err != nil {
		return RunGraph{}, err
	}
	refMoves, err := repository.ListRefMoveEvents(ctx, runID, ref.RefName)
	if err != nil {
		return RunGraph{}, err
	}
	decisions, err := repository.ListSnapshotProcessingDecisions(ctx, runID, ref.RefName)
	if err != nil {
		return RunGraph{}, err
	}

	visibleSnapshots := make([]TaskSnapshot, 0, len(reachableSnapshots))
	for _, snapshot := range snapshots {
		if reachableSnapshots[snapshot.SnapshotID] {
			visibleSnapshots = append(visibleSnapshots, snapshot)
		}
	}
	builder := newRunGraphBuilder(repository, visibleSnapshots)
	historyBuilder := newRunGraphBuilder(repository, snapshots)
	historySnapshotViews := make([]SnapshotView, 0, len(snapshots))
	snapshotViews := make([]SnapshotView, 0, len(reachableSnapshots))
	reachableBagIDs := make(map[string]bool)
	for _, snapshot := range snapshots {
		historyView, err := historyBuilder.snapshotView(ctx, snapshot)
		if err != nil {
			return RunGraph{}, err
		}
		historySnapshotViews = append(historySnapshotViews, historyView)
		if reachableSnapshots[snapshot.SnapshotID] {
			view, err := builder.snapshotView(ctx, snapshot)
			if err != nil {
				return RunGraph{}, err
			}
			snapshotViews = append(snapshotViews, view)
			for _, bagID := range snapshot.InputBagIDs {
				reachableBagIDs[bagID] = true
			}
			for _, bagID := range snapshot.OutputBagIDs {
				reachableBagIDs[bagID] = true
			}
		}
	}
	historyBagViews := make([]BagView, 0, len(bags))
	bagViews := make([]BagView, 0, len(reachableBagIDs))
	for _, bag := range bags {
		historyView, err := historyBuilder.bagView(ctx, bag)
		if err != nil {
			return RunGraph{}, err
		}
		historyBagViews = append(historyBagViews, historyView)
		if reachableBagIDs[bag.BagID] {
			view, err := builder.bagView(ctx, bag)
			if err != nil {
				return RunGraph{}, err
			}
			bagViews = append(bagViews, view)
		}
	}
	historyFrontierViews := make([]FrontierSnapshotView, 0, len(frontiers))
	frontierViews := make([]FrontierSnapshotView, 0, len(reachableFrontiers))
	for _, frontier := range frontiers {
		view := frontierSnapshotView(frontier)
		historyFrontierViews = append(historyFrontierViews, view)
		if reachableFrontiers[frontier.FrontierSnapshotID] {
			frontierViews = append(frontierViews, view)
		}
	}
	refMoveViews := make([]RefMoveEventView, 0, len(refMoves))
	for _, event := range refMoves {
		refMoveViews = append(refMoveViews, refMoveEventView(event))
	}
	decisionViews := make([]SnapshotProcessingDecisionView, 0, len(decisions))
	for _, decision := range decisions {
		decisionViews = append(decisionViews, snapshotProcessingDecisionView(decision))
	}
	refViews := make([]RefView, 0, len(refs))
	for _, item := range refs {
		refViews = append(refViews, refView(item))
	}

	return RunGraph{
		RunID:               runID,
		Ref:                 refView(ref),
		Refs:                refViews,
		FrontierSnapshots:   frontierViews,
		RefMoveEvents:       refMoveViews,
		ProcessingDecisions: decisionViews,
		Snapshots:           snapshotViews,
		Bags:                bagViews,
		HistorySnapshots:    historySnapshotViews,
		HistoryBags:         historyBagViews,
		HistoryFrontiers:    historyFrontierViews,
	}, nil
}

func reachableGraphIDs(ctx context.Context, repository Repository, ref Ref, frontiers []FrontierSnapshot) (map[string]bool, map[string]bool, error) {
	reachableFrontiers := make(map[string]bool)
	reachableSnapshots := make(map[string]bool)
	if strings.TrimSpace(ref.FrontierSnapshotID) == "" {
		for _, snapshotID := range ref.FrontierSnapshotIDs {
			reachableSnapshots[snapshotID] = true
		}
		return reachableFrontiers, reachableSnapshots, nil
	}
	frontiersByID := make(map[string]FrontierSnapshot, len(frontiers))
	for _, frontier := range frontiers {
		frontiersByID[frontier.FrontierSnapshotID] = frontier
	}
	var visit func(string) error
	visit = func(frontierID string) error {
		frontierID = strings.TrimSpace(frontierID)
		if frontierID == "" || reachableFrontiers[frontierID] {
			return nil
		}
		frontier, ok := frontiersByID[frontierID]
		if !ok {
			var err error
			frontier, err = repository.GetFrontierSnapshot(ctx, frontierID)
			if err != nil {
				return err
			}
		}
		reachableFrontiers[frontierID] = true
		for _, snapshotID := range frontier.TaskSnapshotIDs {
			reachableSnapshots[snapshotID] = true
		}
		for _, parentID := range frontier.ParentFrontierSnapshotIDs {
			if err := visit(parentID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(ref.FrontierSnapshotID); err != nil {
		return nil, nil, err
	}
	return reachableFrontiers, reachableSnapshots, nil
}

func BuildTaskSnapshotDetail(ctx context.Context, repository Repository, runID core.RunID, taskID core.TaskID) (SnapshotView, error) {
	if repository == nil {
		return SnapshotView{}, fmt.Errorf("doujia git repository is required")
	}
	if strings.TrimSpace(string(runID)) == "" {
		return SnapshotView{}, fmt.Errorf("run_id is required")
	}
	if strings.TrimSpace(string(taskID)) == "" {
		return SnapshotView{}, fmt.Errorf("task_id is required")
	}
	snapshots, err := repository.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		return SnapshotView{}, err
	}
	var found TaskSnapshot
	for _, snapshot := range snapshots {
		if snapshot.TaskID == taskID {
			found = snapshot
		}
	}
	if strings.TrimSpace(found.SnapshotID) == "" {
		return SnapshotView{}, fmt.Errorf("task snapshot for task %q in run %q not found", taskID, runID)
	}
	builder := newRunGraphBuilder(repository, snapshots)
	return builder.snapshotView(ctx, found)
}

type runGraphBuilder struct {
	repository     Repository
	producerByBag  map[string]string
	consumersByBag map[string][]string
}

func newRunGraphBuilder(repository Repository, snapshots []TaskSnapshot) *runGraphBuilder {
	builder := &runGraphBuilder{
		repository:     repository,
		producerByBag:  make(map[string]string),
		consumersByBag: make(map[string][]string),
	}
	for _, snapshot := range snapshots {
		for _, bagID := range snapshot.OutputBagIDs {
			builder.producerByBag[bagID] = snapshot.SnapshotID
		}
		for _, bagID := range snapshot.InputBagIDs {
			builder.consumersByBag[bagID] = append(builder.consumersByBag[bagID], snapshot.SnapshotID)
		}
	}
	for bagID := range builder.consumersByBag {
		sort.Strings(builder.consumersByBag[bagID])
	}
	return builder
}

func (b *runGraphBuilder) snapshotView(ctx context.Context, snapshot TaskSnapshot) (SnapshotView, error) {
	inputBags, err := b.bagViews(ctx, snapshot.InputBagIDs)
	if err != nil {
		return SnapshotView{}, err
	}
	outputBags, err := b.bagViews(ctx, snapshot.OutputBagIDs)
	if err != nil {
		return SnapshotView{}, err
	}
	return SnapshotView{
		SnapshotID:                 snapshot.SnapshotID,
		RunID:                      snapshot.RunID,
		TaskID:                     snapshot.TaskID,
		LogicalSnapshotID:          snapshot.LogicalSnapshotID,
		SnapshotVersionID:          snapshot.SnapshotVersionID,
		SnapshotVersionNo:          snapshot.SnapshotVersionNo,
		ArrivalKind:                snapshot.ArrivalKind,
		BranchKind:                 snapshot.BranchKind,
		BranchFromSnapshotID:       snapshot.BranchFromSnapshotID,
		RecoverFromSnapshotID:      snapshot.RecoverFromSnapshotID,
		RecoverTargetSnapshotIDs:   append([]string(nil), snapshot.RecoverTargetSnapshotIDs...),
		ReusableSnapshotIDs:        append([]string(nil), snapshot.ReusableSnapshotIDs...),
		RecoverAnchorSnapshotIDs:   append([]string(nil), snapshot.RecoverAnchorSnapshotIDs...),
		PreviousAttemptSnapshotIDs: append([]string(nil), snapshot.PreviousAttemptSnapshotIDs...),
		FailureReportBagIDs:        append([]string(nil), snapshot.FailureReportBagIDs...),
		PreviousOutputBagIDs:       append([]string(nil), snapshot.PreviousOutputBagIDs...),
		RepairTargetTransitionID:   snapshot.RepairTargetTransitionID,
		RepairTargetTaskID:         snapshot.RepairTargetTaskID,
		PipelineInstanceID:         snapshot.PipelineInstanceID,
		TransitionID:               snapshot.TransitionID,
		AgentRole:                  snapshot.AgentRole,
		AgentID:                    snapshot.AgentID,
		Op:                         snapshot.Op,
		Result:                     snapshot.Result,
		InputBagIDs:                append([]string(nil), snapshot.InputBagIDs...),
		OutputBagIDs:               append([]string(nil), snapshot.OutputBagIDs...),
		InputBags:                  inputBags,
		OutputBags:                 outputBags,
		DiagnosticsJSON:            snapshot.DiagnosticsJSON,
		RuntimeContextJSON:         snapshot.RuntimeContextJSON,
		CreatedAt:                  snapshot.CreatedAt,
	}, nil
}

func (b *runGraphBuilder) bagViews(ctx context.Context, bagIDs []string) ([]BagView, error) {
	views := make([]BagView, 0, len(bagIDs))
	for _, bagID := range bagIDs {
		bag, err := b.repository.GetBag(ctx, bagID)
		if err != nil {
			return nil, err
		}
		view, err := b.bagView(ctx, bag)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (b *runGraphBuilder) bagView(ctx context.Context, bag ArtifactBag) (BagView, error) {
	versions := make([]VersionView, 0, len(bag.ArtifactVersionIDs))
	for _, versionID := range bag.ArtifactVersionIDs {
		version, err := b.repository.GetArtifactVersion(ctx, versionID)
		if err != nil {
			return BagView{}, err
		}
		view, err := b.versionView(ctx, version)
		if err != nil {
			return BagView{}, err
		}
		versions = append(versions, view)
	}
	return BagView{
		BagID:               bag.BagID,
		RunID:               bag.RunID,
		ArtifactVersionIDs:  append([]string(nil), bag.ArtifactVersionIDs...),
		Versions:            versions,
		ProducerSnapshotID:  b.producerByBag[bag.BagID],
		ConsumerSnapshotIDs: append([]string(nil), b.consumersByBag[bag.BagID]...),
		CreatedAt:           bag.CreatedAt,
	}, nil
}

func (b *runGraphBuilder) versionView(ctx context.Context, version ArtifactVersion) (VersionView, error) {
	logical, err := b.repository.GetLogicalArtifact(ctx, version.LogicalArtifactID)
	if err != nil {
		return VersionView{}, err
	}
	objects := make([]ObjectView, 0, len(version.ObjectIDs))
	for _, objectID := range version.ObjectIDs {
		object, err := b.repository.GetObject(ctx, objectID)
		if err != nil {
			return VersionView{}, err
		}
		objects = append(objects, objectView(object))
	}
	return VersionView{
		ArtifactVersionID: version.ArtifactVersionID,
		LogicalArtifactID: version.LogicalArtifactID,
		LogicalArtifact:   logicalArtifactView(logical),
		ObjectIDs:         append([]string(nil), version.ObjectIDs...),
		Objects:           objects,
		CreatedAt:         version.CreatedAt,
	}, nil
}

func refView(ref Ref) RefView {
	return RefView{
		RefName:                   ref.RefName,
		FrontierSnapshotID:        ref.FrontierSnapshotID,
		FrontierMemberSnapshotIDs: append([]string(nil), ref.FrontierMemberSnapshotIDs...),
		FrontierSnapshotIDs:       append([]string(nil), ref.FrontierSnapshotIDs...),
		UpdatedAt:                 ref.UpdatedAt,
	}
}

func frontierSnapshotView(snapshot FrontierSnapshot) FrontierSnapshotView {
	return FrontierSnapshotView{
		FrontierSnapshotID:        snapshot.FrontierSnapshotID,
		RunID:                     snapshot.RunID,
		ParentFrontierSnapshotIDs: append([]string(nil), snapshot.ParentFrontierSnapshotIDs...),
		TaskSnapshotIDs:           append([]string(nil), snapshot.TaskSnapshotIDs...),
		CreatedByMode:             snapshot.CreatedByMode,
		CreatedByEventID:          snapshot.CreatedByEventID,
		DetailsJSON:               snapshot.DetailsJSON,
		CreatedAt:                 snapshot.CreatedAt,
	}
}

func refMoveEventView(event RefMoveEvent) RefMoveEventView {
	return RefMoveEventView{
		EventID:                 event.EventID,
		RunID:                   event.RunID,
		RefName:                 event.RefName,
		FromFrontierSnapshotIDs: append([]string(nil), event.FromFrontierSnapshotIDs...),
		ToFrontierSnapshotIDs:   append([]string(nil), event.ToFrontierSnapshotIDs...),
		Mode:                    event.Mode,
		Reason:                  event.Reason,
		DetailsJSON:             event.DetailsJSON,
		CreatedAt:               event.CreatedAt,
	}
}

func snapshotProcessingDecisionView(decision SnapshotProcessingDecision) SnapshotProcessingDecisionView {
	return SnapshotProcessingDecisionView{
		DecisionID:                  decision.DecisionID,
		RunID:                       decision.RunID,
		RefName:                     decision.RefName,
		SnapshotID:                  decision.SnapshotID,
		SnapshotVersionID:           decision.SnapshotVersionID,
		Status:                      decision.Status,
		DecisionKind:                decision.DecisionKind,
		ContinuationID:              decision.ContinuationID,
		Reason:                      decision.Reason,
		ProducedTaskIDs:             append([]string(nil), decision.ProducedTaskIDs...),
		ProducedPipelineInstanceIDs: append([]string(nil), decision.ProducedPipelineInstanceIDs...),
		ConsumedSnapshotIDs:         append([]string(nil), decision.ConsumedSnapshotIDs...),
		ProducedSnapshotIDs:         append([]string(nil), decision.ProducedSnapshotIDs...),
		FromFrontierSnapshotID:      decision.FromFrontierSnapshotID,
		ToFrontierSnapshotID:        decision.ToFrontierSnapshotID,
		RecoverTargetSnapshotIDs:    append([]string(nil), decision.RecoverTargetSnapshotIDs...),
		ReusableSnapshotIDs:         append([]string(nil), decision.ReusableSnapshotIDs...),
		RecoverAnchorSnapshotIDs:    append([]string(nil), decision.RecoverAnchorSnapshotIDs...),
		FailedSnapshotID:            decision.FailedSnapshotID,
		PreviousAttemptSnapshotIDs:  append([]string(nil), decision.PreviousAttemptSnapshotIDs...),
		FailureReportBagIDs:         append([]string(nil), decision.FailureReportBagIDs...),
		PreviousOutputBagIDs:        append([]string(nil), decision.PreviousOutputBagIDs...),
		RepairTargetTransitionID:    decision.RepairTargetTransitionID,
		RepairTargetTaskID:          decision.RepairTargetTaskID,
		CreatedAt:                   decision.CreatedAt,
		UpdatedAt:                   decision.UpdatedAt,
	}
}

func logicalArtifactView(logical LogicalArtifact) LogicalArtifactView {
	return LogicalArtifactView{
		LogicalArtifactID: logical.LogicalArtifactID,
		RunID:             logical.RunID,
		Namespace:         logical.Namespace,
		LogicalKey:        logical.LogicalKey,
		CreatedAt:         logical.CreatedAt,
	}
}

func objectView(object ArtifactObject) ObjectView {
	return ObjectView{
		ObjectID:   object.ObjectID,
		ObjectType: object.ObjectType,
		StorageURI: object.StorageURI,
		CreatedAt:  object.CreatedAt,
	}
}
