package agentengine

import (
	"context"
	"devflow/internal/core"
	"fmt"
	"path"
	"path/filepath"
)

type Reader interface {
	Read(ctx context.Context, uri string) ([]byte, error)
}

type Writer interface {
	Write(ctx context.Context, uri string, content []byte) error
}

func ResolveArtifacts(ctx context.Context, store Reader, refs []ArtifactRef) ([]ArtifactDocument, error) {
	docs := make([]ArtifactDocument, 0, len(refs))
	for _, ref := range refs {
		content, err := store.Read(ctx, ref.URI)
		if err != nil {
			return nil, err
		}
		doc := ArtifactDocument{
			URI:     ref.URI,
			Kind:    ref.Kind,
			Content: string(content),
		}
		doc.Chunks = SplitMarkdown(doc.URI, doc.Content)
		docs = append(docs, doc)
	}
	return docs, nil
}

func WriteArtifacts(ctx context.Context, store Writer, runID core.RunID, agentID core.AgentID, outputKind string, output ModelOutput) ([]string, error) {
	uris := make([]string, 0, len(output.ArtifactOutputs))
	for _, file := range output.ArtifactOutputs {
		uri := path.Join("projects", string(runID), "agents", string(agentID), "artifacts", outputKind, path.Base(filepath.ToSlash(file.Filename)))
		if err := store.Write(ctx, uri, []byte(file.Content)); err != nil {
			return nil, fmt.Errorf("write artifact %s: %w", uri, err)
		}
		uris = append(uris, uri)
	}
	return uris, nil
}
