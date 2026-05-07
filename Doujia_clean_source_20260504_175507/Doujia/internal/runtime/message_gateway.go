package runtime

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"fmt"
	"strings"
	"sync"
)

type Binder interface {
	Bind(ctx context.Context, runID core.RunID, agentID core.AgentID, runtimeID core.RuntimeID) error
}

type Dispatcher interface {
	Dispatch(ctx context.Context, task core.TaskMetaData) error
}

type RuntimeSubmitter interface {
	Submit(ctx context.Context, runtimeID core.RuntimeID, task core.TaskMetaData) error
}

type MessageGateway struct {
	mu        sync.RWMutex
	bindings  map[agentRuntimeKey]core.RuntimeID
	submitter RuntimeSubmitter
	resolver  InputBundleResolver
	logger    logging.RunLogger
}

func NewMessageGateway(submitter RuntimeSubmitter, logger logging.RunLogger) *MessageGateway {
	return &MessageGateway{
		bindings:  make(map[agentRuntimeKey]core.RuntimeID),
		submitter: submitter,
		logger:    logger,
	}
}

func (g *MessageGateway) SetInputBundleResolver(resolver InputBundleResolver) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resolver = resolver
}

func (g *MessageGateway) Bind(_ context.Context, runID core.RunID, agentID core.AgentID, runtimeID core.RuntimeID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bindings[newAgentRuntimeKey(runID, agentID)] = runtimeID
	return nil
}

func (g *MessageGateway) Dispatch(ctx context.Context, task core.TaskMetaData) error {
	g.mu.RLock()
	runtimeID, ok := g.bindings[newAgentRuntimeKey(task.RunID, task.AgentID)]
	resolver := g.resolver
	g.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent %q in run %q is not bound to a runtime", task.AgentID, task.RunID)
	}
	if resolver != nil && (len(task.InputBags) > 0 || len(task.InputBagIDs) > 0) && task.InputBundle == nil {
		inputBags := task.InputBags
		if len(inputBags) == 0 {
			inputBags = InputBagBindingsFromIDs(task.InputBagIDs)
		}
		bundle, err := resolver.ResolveInputBundle(ctx, inputBags)
		if err != nil {
			return fmt.Errorf("resolve input bags for task %q: %w", task.TaskID, err)
		}
		task.InputBags = append([]core.BagBindingRef(nil), inputBags...)
		task.InputBundle = &bundle
	}
	if len(task.ArtifactURIs) == 0 && task.InputBundle != nil {
		task.ArtifactURIs = artifactURIsFromInputBundle(*task.InputBundle)
	}
	if g.logger != nil {
		_ = g.logger.LogTaskMeta(task.RunID, "MessageGateway", fmt.Sprintf("dispatch_to runtime_id=%s", runtimeID), task)
	}
	return g.submitter.Submit(ctx, runtimeID, task)
}

func artifactURIsFromInputBundle(bundle core.AgentInputBundle) []string {
	seen := map[string]bool{}
	uris := make([]string, 0)
	for _, version := range bundle.Versions {
		for _, object := range version.Objects {
			uri := strings.TrimSpace(object.StorageURI)
			if uri == "" || seen[uri] {
				continue
			}
			seen[uri] = true
			uris = append(uris, uri)
		}
	}
	return uris
}

type agentRuntimeKey struct {
	runID   core.RunID
	agentID core.AgentID
}

func newAgentRuntimeKey(runID core.RunID, agentID core.AgentID) agentRuntimeKey {
	return agentRuntimeKey{runID: runID, agentID: agentID}
}
