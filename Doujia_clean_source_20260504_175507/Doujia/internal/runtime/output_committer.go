package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
)

type AgentOutputCommitter interface {
	CommitOutputs(ctx context.Context, req OutputCommitRequest) (core.CommitReceipt, error)
}

type OutputCommitRequest struct {
	RunID           core.RunID
	AgentID         core.AgentID
	TaskID          core.TaskID
	Op              string
	Result          core.TaskResultCode
	Outputs         []core.AgentOutput
	ProducedBags    []core.ProducedBagManifest
	Control         []core.Control
	DiagnosticsJSON string
}

type DoujiaGitOutputCommitter struct {
	repository doujiagit.Repository
	store      artifact.Store
}

func NewDoujiaGitOutputCommitter(repository doujiagit.Repository, store artifact.Store) *DoujiaGitOutputCommitter {
	return &DoujiaGitOutputCommitter{repository: repository, store: store}
}

func (c *DoujiaGitOutputCommitter) CommitOutputs(ctx context.Context, req OutputCommitRequest) (core.CommitReceipt, error) {
	if c == nil || c.repository == nil {
		return core.CommitReceipt{}, fmt.Errorf("agent output committer is not configured")
	}
	if c.store == nil {
		return core.CommitReceipt{}, fmt.Errorf("agent output committer requires artifact store")
	}
	if strings.TrimSpace(string(req.RunID)) == "" {
		return core.CommitReceipt{}, fmt.Errorf("run_id is required")
	}
	if strings.TrimSpace(string(req.AgentID)) == "" {
		return core.CommitReceipt{}, fmt.Errorf("agent_id is required")
	}
	outputRefs := normalizeOutputRefs(producedOutputRefs(req.Outputs))
	if len(req.Outputs) > 0 && len(req.ProducedBags) == 0 {
		return core.CommitReceipt{}, fmt.Errorf("commit outputs requires produced_bags")
	}
	if len(outputRefs) == 0 && len(req.ProducedBags) == 0 {
		return core.CommitReceipt{}, fmt.Errorf("commit outputs requires produced_bags or produced outputs")
	}

	now := time.Now().UTC()
	versionIDsByRef := make(map[string]string, len(outputRefs))
	versionIDs := make([]string, 0, len(outputRefs))
	outputsByRef := outputsByArtifactURI(req.Outputs)
	for _, outputRef := range outputRefs {
		output := outputsByRef[outputRef]
		logicalKey := strings.TrimSpace(output.LogicalKey)
		if logicalKey == "" {
			return core.CommitReceipt{}, fmt.Errorf("produced output ref %q requires explicit logical_key", outputRef)
		}
		content, err := c.store.Read(ctx, outputRef)
		if err != nil {
			return core.CommitReceipt{}, fmt.Errorf("read output artifact %s: %w", outputRef, err)
		}
		objectID := doujiagit.StableObjectID(content)
		if _, err := c.repository.UpsertObject(ctx, doujiagit.ArtifactObject{
			ObjectID:   objectID,
			ObjectType: doujiagit.ObjectTypeBlob,
			StorageURI: outputRef,
			CreatedAt:  now,
		}); err != nil {
			return core.CommitReceipt{}, err
		}

		logicalID := doujiagit.StableLogicalArtifactID(req.RunID, string(req.AgentID), logicalKey)
		logical, err := c.repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
			LogicalArtifactID: logicalID,
			RunID:             req.RunID,
			Namespace:         string(req.AgentID),
			LogicalKey:        logicalKey,
			CreatedAt:         now,
		})
		if err != nil {
			return core.CommitReceipt{}, err
		}

		versionID := doujiagit.StableArtifactVersionID(logical.LogicalArtifactID, []string{objectID})
		version, err := c.repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
			ArtifactVersionID: versionID,
			LogicalArtifactID: logical.LogicalArtifactID,
			ObjectIDs:         []string{objectID},
			CreatedAt:         now,
		})
		if err != nil {
			return core.CommitReceipt{}, err
		}
		versionIDsByRef[outputRef] = version.ArtifactVersionID
		versionIDs = append(versionIDs, version.ArtifactVersionID)
	}
	for _, output := range req.Outputs {
		if output.Status != "reused" || strings.TrimSpace(output.ArtifactVersionID) == "" {
			continue
		}
		versionIDs = append(versionIDs, strings.TrimSpace(output.ArtifactVersionID))
	}

	committedBags, err := materializeProducedBags(req, outputRefs, versionIDsByRef, versionIDs)
	if err != nil {
		return core.CommitReceipt{}, err
	}
	result := req.Result
	if result == "" {
		result = core.TaskResultCodeOK
	}
	return core.CommitReceipt{
		Result:                 result,
		ProducedBags:           committedBags,
		MaterializedOutputRefs: outputRefs,
		Control:                append([]core.Control(nil), req.Control...),
		DiagnosticsJSON:        strings.TrimSpace(req.DiagnosticsJSON),
	}, nil
}

func producedOutputRefs(outputs []core.AgentOutput) []string {
	refs := make([]string, 0, len(outputs))
	for _, output := range outputs {
		if strings.TrimSpace(output.Status) != "produced" {
			continue
		}
		ref := filepath.ToSlash(strings.TrimSpace(output.ArtifactURI))
		if ref == "" {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func outputsByArtifactURI(outputs []core.AgentOutput) map[string]core.AgentOutput {
	out := make(map[string]core.AgentOutput, len(outputs))
	for _, output := range outputs {
		ref := filepath.ToSlash(strings.TrimSpace(output.ArtifactURI))
		if ref == "" {
			continue
		}
		out[ref] = output
	}
	return out
}

func materializeProducedBags(req OutputCommitRequest, outputRefs []string, versionIDsByRef map[string]string, fallbackVersionIDs []string) ([]core.CommittedBagDef, error) {
	if len(req.ProducedBags) == 0 {
		return nil, nil
	}
	outputByLogicalKey := make(map[string]core.AgentOutput, len(req.Outputs))
	for _, output := range req.Outputs {
		key := strings.TrimSpace(output.LogicalKey)
		if key == "" {
			continue
		}
		outputByLogicalKey[key] = output
	}

	out := make([]core.CommittedBagDef, 0, len(req.ProducedBags))
	for _, bag := range req.ProducedBags {
		name := strings.TrimSpace(bag.Name)
		if name == "" {
			return nil, fmt.Errorf("produced bag name is required")
		}
		if len(bag.Members) == 0 {
			return nil, fmt.Errorf("produced bag %q has no members", name)
		}
		next := core.CommittedBagDef{
			Name:              name,
			Indexes:           cloneStringMap(bag.Indexes),
			MemberLogicalKeys: make([]string, 0, len(bag.Members)),
		}
		seenMembers := make(map[string]bool, len(bag.Members))
		for _, member := range bag.Members {
			logicalKey := strings.TrimSpace(member.LogicalKey)
			outputRef := filepath.ToSlash(strings.TrimSpace(member.OutputRef))
			artifactVersionID := strings.TrimSpace(member.ArtifactVersionID)
			if logicalKey == "" && outputRef == "" && artifactVersionID == "" {
				return nil, fmt.Errorf("produced bag %q has an empty member", name)
			}

			memberIdentity := logicalKey + "|" + outputRef + "|" + artifactVersionID
			if seenMembers[memberIdentity] {
				return nil, fmt.Errorf("produced bag %q has duplicate member %q", name, memberIdentity)
			}
			seenMembers[memberIdentity] = true

			if artifactVersionID == "" && logicalKey != "" {
				if output, ok := outputByLogicalKey[logicalKey]; ok {
					switch strings.TrimSpace(output.Status) {
					case "produced":
						if outputRef == "" {
							outputRef = filepath.ToSlash(strings.TrimSpace(output.ArtifactURI))
						}
					case "reused":
						artifactVersionID = strings.TrimSpace(output.ArtifactVersionID)
					}
				}
			}
			if artifactVersionID != "" {
				next.ArtifactVersionIDs = append(next.ArtifactVersionIDs, artifactVersionID)
				if logicalKey != "" {
					next.MemberLogicalKeys = append(next.MemberLogicalKeys, logicalKey)
				}
				continue
			}
			if outputRef == "" {
				return nil, fmt.Errorf("produced bag %q member %q could not be resolved to output_ref or artifact_version_id", name, logicalKey)
			}
			versionID := strings.TrimSpace(versionIDsByRef[outputRef])
			if versionID == "" {
				return nil, fmt.Errorf("produced bag %q references output ref %q that was not materialized", name, outputRef)
			}
			next.ArtifactVersionIDs = append(next.ArtifactVersionIDs, versionID)
			if logicalKey != "" {
				next.MemberLogicalKeys = append(next.MemberLogicalKeys, logicalKey)
			}
		}
		next.ArtifactVersionIDs = doujiagit.NormalizeIDs(next.ArtifactVersionIDs)
		next.MemberLogicalKeys = normalizeStrings(next.MemberLogicalKeys)
		if len(next.ArtifactVersionIDs) == 0 {
			return nil, fmt.Errorf("produced bag %q has no artifact versions", name)
		}
		out = append(out, next)
	}
	return out, nil
}

func normalizeOutputRefs(refs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = filepath.ToSlash(strings.TrimSpace(ref))
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func normalizeStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
