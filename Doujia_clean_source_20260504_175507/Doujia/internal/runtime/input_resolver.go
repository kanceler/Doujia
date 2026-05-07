package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
)

type InputBundleResolver interface {
	ResolveInputBundle(ctx context.Context, inputBags []core.BagBindingRef) (core.AgentInputBundle, error)
}

type DoujiaGitInputResolver struct {
	repository doujiagit.Repository
}

func NewDoujiaGitInputResolver(repository doujiagit.Repository) *DoujiaGitInputResolver {
	return &DoujiaGitInputResolver{repository: repository}
}

func (r *DoujiaGitInputResolver) ResolveInputBundle(ctx context.Context, inputBags []core.BagBindingRef) (core.AgentInputBundle, error) {
	if r == nil || r.repository == nil {
		return core.AgentInputBundle{}, fmt.Errorf("doujia git input resolver is not configured")
	}

	bundle := core.AgentInputBundle{
		Bags:     make([]core.AgentInputBag, 0, len(inputBags)),
		Versions: make([]core.AgentInputVersion, 0),
	}
	seenBags := make(map[string]bool)
	seenVersions := make(map[string]bool)

	for _, inputBag := range inputBags {
		inputBagID := strings.TrimSpace(inputBag.BagID)
		if inputBagID == "" || seenBags[inputBagID] {
			continue
		}
		seenBags[inputBagID] = true

		bag, err := r.repository.GetBag(ctx, inputBagID)
		if err != nil {
			return core.AgentInputBundle{}, err
		}
		bundle.Bags = append(bundle.Bags, core.AgentInputBag{
			Name:               strings.TrimSpace(inputBag.Name),
			BagID:              bag.BagID,
			Indexes:            cloneStringMap(inputBag.Indexes),
			ArtifactVersionIDs: append([]string(nil), bag.ArtifactVersionIDs...),
		})

		for _, versionID := range bag.ArtifactVersionIDs {
			if seenVersions[versionID] {
				continue
			}
			seenVersions[versionID] = true
			version, err := r.repository.GetArtifactVersion(ctx, versionID)
			if err != nil {
				return core.AgentInputBundle{}, err
			}
			logical, err := r.repository.GetLogicalArtifact(ctx, version.LogicalArtifactID)
			if err != nil {
				return core.AgentInputBundle{}, err
			}
			inputVersion := core.AgentInputVersion{
				ArtifactVersionID: version.ArtifactVersionID,
				LogicalArtifactID: version.LogicalArtifactID,
				LogicalKey:        logical.LogicalKey,
				ObjectIDs:         append([]string(nil), version.ObjectIDs...),
				Objects:           make([]core.AgentInputObject, 0, len(version.ObjectIDs)),
			}
			for _, objectID := range version.ObjectIDs {
				obj, err := r.repository.GetObject(ctx, objectID)
				if err != nil {
					return core.AgentInputBundle{}, err
				}
				inputVersion.Objects = append(inputVersion.Objects, core.AgentInputObject{
					ObjectID:   obj.ObjectID,
					ObjectType: obj.ObjectType,
					StorageURI: obj.StorageURI,
				})
				if inputVersion.ObjectType == "" {
					inputVersion.ObjectType = obj.ObjectType
				}
				if inputVersion.StorageURI == "" {
					inputVersion.StorageURI = obj.StorageURI
				}
			}
			inputVersion.LocalPath = localPathFromArtifactURI(inputVersion.StorageURI)
			bundle.Versions = append(bundle.Versions, inputVersion)
			if inputVersion.LogicalKey != "" && inputVersion.StorageURI != "" {
				bundle.Inputs = append(bundle.Inputs, core.InputArtifact{
					LogicalKey:        inputVersion.LogicalKey,
					Path:              inputVersion.LocalPath,
					ArtifactVersionID: inputVersion.ArtifactVersionID,
					LogicalArtifactID: inputVersion.LogicalArtifactID,
					ObjectType:        inputVersion.ObjectType,
					ContentType:       inputVersion.ContentType,
					Encoding:          inputVersion.Encoding,
				})
			}
		}
	}

	return bundle, nil
}

func InputBagBindingsFromIDs(ids []string) []core.BagBindingRef {
	out := make([]core.BagBindingRef, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, core.BagBindingRef{BagID: id})
	}
	return out
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
	return out
}

func localPathFromArtifactURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	return filepath.FromSlash(uri)
}
