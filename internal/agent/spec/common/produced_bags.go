package common

import (
	"strings"

	appcore "devflow/internal/core"

	agentcore "devflow/internal/agent/core"
)

func OutputMembers(result agentcore.AgentResult, keys ...string) []appcore.ProducedBagMember {
	outputsByKey := make(map[string]agentcore.AgentOutput, len(result.Outputs))
	for _, output := range result.Outputs {
		outputsByKey[output.LogicalKey] = output
	}
	members := make([]appcore.ProducedBagMember, 0, len(keys))
	for _, key := range keys {
		output, ok := outputsByKey[key]
		if !ok {
			continue
		}
		switch output.Status {
		case "produced":
			if output.ArtifactURI == "" {
				continue
			}
			members = append(members, appcore.ProducedBagMember{
				LogicalKey: key,
				OutputRef:  output.ArtifactURI,
			})
		case "reused":
			if output.ArtifactVersionID == "" {
				continue
			}
			members = append(members, appcore.ProducedBagMember{
				LogicalKey:        key,
				ArtifactVersionID: output.ArtifactVersionID,
			})
		}
	}
	return members
}

func InputMembers(bundle agentcore.AgentInputBundle, keys ...string) []appcore.ProducedBagMember {
	versionIDs := InputVersionIDsByLogicalKey(bundle)
	members := make([]appcore.ProducedBagMember, 0, len(keys))
	for _, key := range keys {
		for _, id := range versionIDs[key] {
			members = append(members, appcore.ProducedBagMember{
				LogicalKey:        key,
				ArtifactVersionID: id,
			})
		}
	}
	return members
}

func InputVersionIDsByLogicalKey(bundle agentcore.AgentInputBundle) map[string][]string {
	out := make(map[string][]string)
	for _, version := range bundle.Versions {
		if version.LogicalKey == "" || version.ArtifactVersionID == "" {
			continue
		}
		out[version.LogicalKey] = append(out[version.LogicalKey], version.ArtifactVersionID)
	}
	for _, input := range bundle.Inputs {
		if input.LogicalKey == "" || input.ArtifactVersionID == "" {
			continue
		}
		out[input.LogicalKey] = append(out[input.LogicalKey], input.ArtifactVersionID)
	}
	for key, ids := range out {
		out[key] = uniqueStrings(ids)
	}
	return out
}

func NonEmptyProducedBags(defs []appcore.ProducedBagManifest) []appcore.ProducedBagManifest {
	out := make([]appcore.ProducedBagManifest, 0, len(defs))
	for _, def := range defs {
		if len(def.Members) == 0 {
			continue
		}
		out = append(out, def)
	}
	return out
}

func InputBagIndexes(bundle agentcore.AgentInputBundle, name string) map[string]string {
	name = strings.TrimSpace(name)
	for _, bag := range bundle.Bags {
		if strings.TrimSpace(bag.Name) != name || len(bag.Indexes) == 0 {
			continue
		}
		return cloneStringMap(bag.Indexes)
	}
	return nil
}

func cloneStringMap(items map[string]string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for key, value := range items {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
