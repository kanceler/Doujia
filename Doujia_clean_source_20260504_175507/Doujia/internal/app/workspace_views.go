package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"devflow/internal/core"
	"devflow/internal/state/repo"
)

func (b *Bootstrap) buildWorkspaceOverview(ctx context.Context, runID core.RunID) (workspaceOverviewView, error) {
	run, err := b.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return workspaceOverviewView{}, err
	}
	iterations, err := b.Internals.RunIterationRepository.ListByRun(ctx, runID)
	if err != nil {
		return workspaceOverviewView{}, err
	}
	artifacts := make([]repo.SessionArtifactRecord, 0)
	if b.Internals.SessionArtifactRepository != nil {
		artifacts, err = b.Internals.SessionArtifactRepository.ListByRun(ctx, runID)
		if err != nil {
			return workspaceOverviewView{}, err
		}
	}
	improvements := make([]repo.ImprovementItemRecord, 0)
	if b.Internals.ImprovementItemRepository != nil {
		improvements, err = b.Internals.ImprovementItemRepository.ListByRun(ctx, runID)
		if err != nil {
			return workspaceOverviewView{}, err
		}
	}
	view := workspaceOverviewView{
		Run:                toRunView(run),
		CurrentIterationNo: run.CurrentIterationNo,
		AcceptanceCheckpoint: acceptanceCheckpointView{
			RunID:                    run.ID,
			CheckpointTaskID:         run.LatestAcceptanceCheckpointTaskID,
			CurrentIterationNo:       run.CurrentIterationNo,
			LatestDeliveryFrontierID: run.LatestDeliveryFrontierID,
			Status:                   run.Status,
		},
		Iterations:           make([]runIterationView, 0, len(iterations)),
		SessionArtifacts:     make([]sessionArtifactView, 0, len(artifacts)),
		OpenImprovementItems: make([]improvementItemView, 0, len(improvements)),
	}
	for _, iteration := range iterations {
		view.Iterations = append(view.Iterations, toRunIterationView(iteration))
	}
	for _, artifact := range artifacts {
		view.SessionArtifacts = append(view.SessionArtifacts, toSessionArtifactView(artifact))
	}
	for _, item := range improvements {
		if item.Status == "open" {
			view.OpenImprovementItems = append(view.OpenImprovementItems, toImprovementItemView(item))
		}
	}
	return view, nil
}

func (b *Bootstrap) buildPipelineWorkspace(ctx context.Context, runID core.RunID, focusInstanceID string) (pipelineWorkspaceView, error) {
	run, err := b.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return pipelineWorkspaceView{}, err
	}
	iterations, err := b.Internals.RunIterationRepository.ListByRun(ctx, runID)
	if err != nil {
		return pipelineWorkspaceView{}, err
	}
	instances, err := b.Internals.InstanceRepository.ListByRun(ctx, runID)
	if err != nil {
		return pipelineWorkspaceView{}, err
	}
	tasks, err := b.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		return pipelineWorkspaceView{}, err
	}

	instanceByID := make(map[core.PipelineInstanceID]repo.PipelineInstanceRecord, len(instances))
	tasksByInstance := make(map[core.PipelineInstanceID][]repo.TaskRecord)
	childInstancesByParent := make(map[core.PipelineInstanceID][]repo.PipelineInstanceRecord)

	for _, instance := range instances {
		instanceByID[instance.ID] = instance
	}
	for _, task := range tasks {
		tasksByInstance[task.PipelineInstanceID] = append(tasksByInstance[task.PipelineInstanceID], task)
	}
	for instanceID := range tasksByInstance {
		sortTaskRecords(tasksByInstance[instanceID])
	}
	sortInstanceRecords(instances)

	instanceStatusByID := make(map[core.PipelineInstanceID]string, len(instances))
	for _, instance := range instances {
		instanceStatusByID[instance.ID] = normalizeWorkspaceInstanceStatus(instance, tasksByInstance[instance.ID])
		if instance.ParentID != nil {
			childInstancesByParent[*instance.ParentID] = append(childInstancesByParent[*instance.ParentID], instance)
		}
	}
	for parentID := range childInstancesByParent {
		sortInstanceRecords(childInstancesByParent[parentID])
	}

	view := pipelineWorkspaceView{
		RunID:              run.ID,
		Status:             run.Status,
		CurrentIterationNo: run.CurrentIterationNo,
		Iterations:         make([]runIterationView, 0, len(iterations)),
		Instances:          make([]pipelineWorkspaceInstanceView, 0, len(instances)),
		Edges:              make([]pipelineWorkspaceEdgeView, 0),
	}
	for _, iteration := range iterations {
		view.Iterations = append(view.Iterations, toRunIterationView(iteration))
	}

	for _, instance := range instances {
		taskItems := tasksByInstance[instance.ID]
		instanceView := pipelineWorkspaceInstanceView{
			InstanceID:         instance.ID,
			PipelineID:         instance.PipelineID,
			ParentInstanceID:   instance.ParentID,
			ParentTransitionID: instance.ParentTransitionID,
			InstanceKey:        instance.InstanceKey,
			Status:             instanceStatusByID[instance.ID],
			DefaultCollapsed:   instance.ParentID != nil,
			Tasks:              make([]pipelineWorkspaceTaskView, 0, len(taskItems)),
		}
		for _, task := range taskItems {
			instanceView.Tasks = append(instanceView.Tasks, pipelineWorkspaceTaskView{
				TaskID:  task.ID,
				StageID: task.StageID,
				Op:      task.Op,
				Status:  normalizeWorkspaceTaskStatus(task),
			})
		}
		view.Instances = append(view.Instances, instanceView)
		if instance.ParentID != nil {
			view.Edges = append(view.Edges, pipelineWorkspaceEdgeView{
				FromInstanceID: *instance.ParentID,
				ToInstanceID:   instance.ID,
				Kind:           "child",
				Label:          instance.ParentTransitionID,
			})
		}
	}

	if strings.TrimSpace(focusInstanceID) != "" {
		focused, ok := instanceByID[core.PipelineInstanceID(focusInstanceID)]
		if !ok {
			return pipelineWorkspaceView{}, fmt.Errorf("pipeline instance %s not found", focusInstanceID)
		}
		view.FocusInstanceID = string(focused.ID)
		view.FocusPipelineID = string(focused.PipelineID)
		nodes, edges, collapsed := buildFocusedPipelineGraph(
			focused,
			tasksByInstance[focused.ID],
			childInstancesByParent[focused.ID],
			instanceStatusByID,
		)
		view.MainPipelineNodes = nodes
		view.MainPipelineEdges = edges
		view.CollapsedChildNodes = collapsed
		return view, nil
	}

	rootInstances := make([]repo.PipelineInstanceRecord, 0)
	for _, instance := range instances {
		if instance.ParentID == nil {
			rootInstances = append(rootInstances, instance)
		}
	}
	nodes, edges, collapsed := buildRootPipelineGraph(rootInstances, tasksByInstance, childInstancesByParent, instanceStatusByID)
	view.MainPipelineNodes = nodes
	view.MainPipelineEdges = edges
	view.CollapsedChildNodes = collapsed
	return view, nil
}

func buildRootPipelineGraph(
	rootInstances []repo.PipelineInstanceRecord,
	tasksByInstance map[core.PipelineInstanceID][]repo.TaskRecord,
	childInstancesByParent map[core.PipelineInstanceID][]repo.PipelineInstanceRecord,
	instanceStatusByID map[core.PipelineInstanceID]string,
) ([]pipelineWorkspaceMainNodeView, []pipelineWorkspaceMainEdgeView, []pipelineWorkspaceMainNodeView) {
	sortInstanceRecords(rootInstances)
	nodes := make([]pipelineWorkspaceMainNodeView, 0)
	edges := make([]pipelineWorkspaceMainEdgeView, 0)
	collapsed := make([]pipelineWorkspaceMainNodeView, 0)
	detachedRootTasks := append([]repo.TaskRecord(nil), tasksByInstance[""]...)
	sortTaskRecords(detachedRootTasks)

	for index, root := range rootInstances {
		rootTasks := append([]repo.TaskRecord(nil), tasksByInstance[root.ID]...)
		if index == 0 && len(detachedRootTasks) > 0 {
			rootTasks = append(rootTasks, detachedRootTasks...)
		}
		sortTaskRecords(rootTasks)

		taskByStage := make(map[string]repo.TaskRecord, len(rootTasks))
		for _, task := range rootTasks {
			stageID := normalizeMainStageID(task)
			if existing, exists := taskByStage[stageID]; !exists || preferMainStageTask(task, existing, stageID) {
				taskByStage[stageID] = task
			}
		}

		orderedStages := []string{
			"ceo_write_requirement",
			"pm_write_plan",
			"architect_write_plan",
		}
		for _, stageID := range orderedStages {
			if task, ok := taskByStage[stageID]; ok {
				nodes = append(nodes, buildTaskNode(task, stageID))
			}
		}

		forkPreparation := pipelineWorkspaceMainNodeView{
			NodeID:   "fork:prepare_delivery",
			NodeKind: "fork",
			StageID:  "fork_prepare_delivery",
			Label:    "并行准备",
			Status:   aggregateWorkspaceStatuses(collectStatuses(taskByStage["architect_create_container"], taskByStage["split_module"])),
		}
		containerTask, hasContainer := taskByStage["architect_create_container"]
		splitTask, hasSplit := taskByStage["split_module"]
		if hasContainer || hasSplit {
			nodes = append(nodes, forkPreparation)
			if hasContainer {
				nodes = append(nodes, buildTaskNode(containerTask, "architect_create_container"))
			}
			if hasSplit {
				nodes = append(nodes, buildTaskNode(splitTask, "split_module"))
			}
			nodes = append(nodes, pipelineWorkspaceMainNodeView{
				NodeID:   "join:prepare_delivery",
				NodeKind: "join",
				StageID:  "join_prepare_delivery",
				Label:    "准备完成",
				Status:   aggregateWorkspaceStatuses(collectStatuses(containerTask, splitTask)),
			})
		}

		moduleChildren := groupChildInstancesByTransition(childInstancesByParent[root.ID], "test_all_modules")
		globalChildren := groupChildInstancesByTransition(childInstancesByParent[root.ID], "write_global_test_data")
		if len(moduleChildren) > 0 || len(globalChildren) > 0 {
			nodes = append(nodes, pipelineWorkspaceMainNodeView{
				NodeID:   "fork:implementation",
				NodeKind: "fork",
				StageID:  "fork_implementation",
				Label:    "并行开发",
				Status:   aggregateWorkspaceStatuses(append(extractInstanceStatuses(moduleChildren, instanceStatusByID), extractInstanceStatuses(globalChildren, instanceStatusByID)...)),
			})
			if len(moduleChildren) > 0 {
				childNode := buildChildPipelineNode("test_all_modules", moduleChildren, instanceStatusByID)
				nodes = append(nodes, childNode)
				collapsed = append(collapsed, childNode)
			}
			if len(globalChildren) > 0 {
				childNode := buildChildPipelineNode("write_global_test_data", globalChildren, instanceStatusByID)
				nodes = append(nodes, childNode)
				collapsed = append(collapsed, childNode)
			}
			nodes = append(nodes, pipelineWorkspaceMainNodeView{
				NodeID:   "join:implementation",
				NodeKind: "join",
				StageID:  "join_implementation",
				Label:    "并行收敛",
				Status:   aggregateWorkspaceStatuses(append(extractInstanceStatuses(moduleChildren, instanceStatusByID), extractInstanceStatuses(globalChildren, instanceStatusByID)...)),
			})
		}

		mergeChildren := groupChildInstancesByTransition(childInstancesByParent[root.ID], "merge_code")
		if len(mergeChildren) > 0 {
			childNode := buildChildPipelineNode("merge_code", mergeChildren, instanceStatusByID)
			nodes = append(nodes, childNode)
			collapsed = append(collapsed, childNode)
		}
		globalTestChildren := groupChildInstancesByTransition(childInstancesByParent[root.ID], "global_test_code")
		if len(globalTestChildren) > 0 {
			childNode := buildChildPipelineNode("global_test_code", globalTestChildren, instanceStatusByID)
			nodes = append(nodes, childNode)
			collapsed = append(collapsed, childNode)
		}
		for _, stageID := range []string{"merge_code", "global_test_code", "acceptance"} {
			if task, ok := taskByStage[stageID]; ok {
				if stageID == "merge_code" && len(mergeChildren) > 0 {
					continue
				}
				if stageID == "global_test_code" && len(globalTestChildren) > 0 {
					continue
				}
				nodes = append(nodes, buildTaskNode(task, stageID))
			}
		}
	}

	edges = buildSequenceEdges(nodes)
	return nodes, edges, collapsed
}

func buildFocusedPipelineGraph(
	focus repo.PipelineInstanceRecord,
	tasks []repo.TaskRecord,
	children []repo.PipelineInstanceRecord,
	instanceStatusByID map[core.PipelineInstanceID]string,
) ([]pipelineWorkspaceMainNodeView, []pipelineWorkspaceMainEdgeView, []pipelineWorkspaceMainNodeView) {
	sortTaskRecords(tasks)
	nodes := make([]pipelineWorkspaceMainNodeView, 0, len(tasks)+len(children))
	collapsed := make([]pipelineWorkspaceMainNodeView, 0, len(children))
	for _, task := range tasks {
		stageID := normalizeMainStageID(task)
		nodes = append(nodes, buildTaskNode(task, stageID))
	}
	for _, child := range children {
		grouped := []repo.PipelineInstanceRecord{child}
		childNode := buildChildPipelineNode(firstNonEmpty(child.ParentTransitionID, child.InstanceKey, string(child.PipelineID)), grouped, instanceStatusByID)
		nodes = append(nodes, childNode)
		collapsed = append(collapsed, childNode)
	}
	if nodesWithModuleFlow, edges, ok := buildFocusedModulePipelineGraph(focus, nodes); ok {
		return nodesWithModuleFlow, edges, collapsed
	}
	edges := buildSequenceEdges(nodes)
	return nodes, edges, collapsed
}

func buildFocusedModulePipelineGraph(
	focus repo.PipelineInstanceRecord,
	nodes []pipelineWorkspaceMainNodeView,
) ([]pipelineWorkspaceMainNodeView, []pipelineWorkspaceMainEdgeView, bool) {
	if strings.TrimSpace(string(focus.PipelineID)) != "pipeline_module" {
		return nil, nil, false
	}
	writeCodeNodes := visibleNodesByStage(nodes, "write_code")
	writeTestDataNodes := visibleNodesByStage(nodes, "write_test_data")
	if len(writeCodeNodes) == 0 || len(writeTestDataNodes) == 0 {
		return nil, nil, false
	}

	forkNode := pipelineWorkspaceMainNodeView{
		NodeID:   "fork:" + string(focus.ID) + ":module_work",
		NodeKind: "fork",
		StageID:  "fork_module_work",
		Label:    "模块并行准备",
		Status:   aggregateWorkspaceStatuses(append(nodeStatuses(writeCodeNodes), nodeStatuses(writeTestDataNodes)...)),
	}
	joinNode := pipelineWorkspaceMainNodeView{
		NodeID:   "join:" + string(focus.ID) + ":module_test_input",
		NodeKind: "join",
		StageID:  "join_module_test_input",
		Label:    "模块测试输入就绪",
		Status:   aggregateWorkspaceStatuses(append(nodeStatuses(writeCodeNodes), nodeStatuses(writeTestDataNodes)...)),
	}

	nextNodes := make([]pipelineWorkspaceMainNodeView, 0, len(nodes)+2)
	edges := make([]pipelineWorkspaceMainEdgeView, 0)
	seenParallelNode := false
	joinAdded := false
	previousLinearNode := ""
	for _, node := range nodes {
		if node.StageID == "write_code" || node.StageID == "write_test_data" {
			if !seenParallelNode {
				if previousLinearNode != "" {
					edges = append(edges, pipelineWorkspaceMainEdgeView{
						FromNodeID: previousLinearNode,
						ToNodeID:   forkNode.NodeID,
						Kind:       "sequence",
					})
				}
				nextNodes = append(nextNodes, forkNode)
				seenParallelNode = true
			}
			nextNodes = append(nextNodes, node)
			edges = append(edges,
				pipelineWorkspaceMainEdgeView{FromNodeID: forkNode.NodeID, ToNodeID: node.NodeID, Kind: "fork"},
				pipelineWorkspaceMainEdgeView{FromNodeID: node.NodeID, ToNodeID: joinNode.NodeID, Kind: "join"},
			)
			continue
		}
		if seenParallelNode && !joinAdded {
			nextNodes = append(nextNodes, joinNode)
			previousLinearNode = joinNode.NodeID
			joinAdded = true
		}
		if previousLinearNode != "" {
			edges = append(edges, pipelineWorkspaceMainEdgeView{
				FromNodeID: previousLinearNode,
				ToNodeID:   node.NodeID,
				Kind:       "sequence",
			})
		}
		nextNodes = append(nextNodes, node)
		previousLinearNode = node.NodeID
	}
	if seenParallelNode && !joinAdded {
		nextNodes = append(nextNodes, joinNode)
	}
	return nextNodes, edges, true
}

func visibleNodesByStage(nodes []pipelineWorkspaceMainNodeView, stageID string) []pipelineWorkspaceMainNodeView {
	matches := make([]pipelineWorkspaceMainNodeView, 0)
	for _, node := range nodes {
		if node.StageID == stageID && node.NodeKind != "fork" && node.NodeKind != "join" {
			matches = append(matches, node)
		}
	}
	return matches
}

func nodeStatuses(nodes []pipelineWorkspaceMainNodeView) []string {
	statuses := make([]string, 0, len(nodes))
	for _, node := range nodes {
		status := strings.TrimSpace(node.Status)
		if status != "" {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func buildSequenceEdges(nodes []pipelineWorkspaceMainNodeView) []pipelineWorkspaceMainEdgeView {
	edges := make([]pipelineWorkspaceMainEdgeView, 0)
	previousLinearNode := ""
	openForkID := ""
	branchTargets := make([]string, 0)
	for _, node := range nodes {
		switch node.NodeKind {
		case "fork":
			if previousLinearNode != "" {
				edges = append(edges, pipelineWorkspaceMainEdgeView{
					FromNodeID: previousLinearNode,
					ToNodeID:   node.NodeID,
					Kind:       "sequence",
				})
			}
			openForkID = node.NodeID
			branchTargets = branchTargets[:0]
			previousLinearNode = node.NodeID
		case "join":
			for _, branchID := range branchTargets {
				edges = append(edges, pipelineWorkspaceMainEdgeView{
					FromNodeID: branchID,
					ToNodeID:   node.NodeID,
					Kind:       "join",
				})
			}
			if len(branchTargets) == 0 && previousLinearNode != "" {
				edges = append(edges, pipelineWorkspaceMainEdgeView{
					FromNodeID: previousLinearNode,
					ToNodeID:   node.NodeID,
					Kind:       "sequence",
				})
			}
			previousLinearNode = node.NodeID
			openForkID = ""
			branchTargets = branchTargets[:0]
		default:
			if openForkID != "" && previousLinearNode == openForkID {
				edges = append(edges, pipelineWorkspaceMainEdgeView{
					FromNodeID: openForkID,
					ToNodeID:   node.NodeID,
					Kind:       "fork",
				})
				branchTargets = append(branchTargets, node.NodeID)
			} else {
				if previousLinearNode != "" {
					edges = append(edges, pipelineWorkspaceMainEdgeView{
						FromNodeID: previousLinearNode,
						ToNodeID:   node.NodeID,
						Kind:       "sequence",
					})
				}
				if openForkID != "" {
					branchTargets = append(branchTargets, node.NodeID)
				}
			}
			if openForkID == "" {
				previousLinearNode = node.NodeID
			}
		}
	}
	return edges
}

func buildTaskNode(task repo.TaskRecord, stageID string) pipelineWorkspaceMainNodeView {
	return pipelineWorkspaceMainNodeView{
		NodeID:   "task:" + string(task.ID),
		NodeKind: "task",
		StageID:  stageID,
		Label:    workspaceStageLabel(stageID),
		Status:   normalizeWorkspaceTaskStatus(task),
		TaskID:   task.ID,
	}
}

func buildChildPipelineNode(
	stageID string,
	grouped []repo.PipelineInstanceRecord,
	instanceStatusByID map[core.PipelineInstanceID]string,
) pipelineWorkspaceMainNodeView {
	sortInstanceRecords(grouped)
	first := grouped[0]
	return pipelineWorkspaceMainNodeView{
		NodeID:                  "child:" + stageID + ":" + string(first.ID),
		NodeKind:                "child_pipeline",
		StageID:                 stageID,
		Label:                   workspaceStageLabel(stageID),
		Status:                  aggregateWorkspaceStatuses(extractInstanceStatuses(grouped, instanceStatusByID)),
		ChildPipelineInstanceID: string(first.ID),
	}
}

func normalizeMainStageID(task repo.TaskRecord) string {
	stageID := strings.TrimSpace(string(task.StageID))
	if stageID == "task_01" && strings.TrimSpace(task.Op) == "write_plan" {
		return "ceo_write_requirement"
	}
	return stageID
}

func preferMainStageTask(candidate repo.TaskRecord, existing repo.TaskRecord, stageID string) bool {
	candidateExact := strings.TrimSpace(string(candidate.StageID)) == strings.TrimSpace(stageID)
	existingExact := strings.TrimSpace(string(existing.StageID)) == strings.TrimSpace(stageID)
	if candidateExact != existingExact {
		return candidateExact
	}
	if candidate.CreatedAt.Equal(existing.CreatedAt) {
		return candidate.ID < existing.ID
	}
	return candidate.CreatedAt.Before(existing.CreatedAt)
}

func groupChildInstancesByTransition(items []repo.PipelineInstanceRecord, transitionID string) []repo.PipelineInstanceRecord {
	grouped := make([]repo.PipelineInstanceRecord, 0)
	for _, item := range items {
		if strings.TrimSpace(item.ParentTransitionID) == strings.TrimSpace(transitionID) {
			grouped = append(grouped, item)
		}
	}
	sortInstanceRecords(grouped)
	return grouped
}

func collectStatuses(tasks ...repo.TaskRecord) []string {
	statuses := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(string(task.ID)) == "" {
			continue
		}
		statuses = append(statuses, normalizeWorkspaceTaskStatus(task))
	}
	return statuses
}

func extractInstanceStatuses(items []repo.PipelineInstanceRecord, instanceStatusByID map[core.PipelineInstanceID]string) []string {
	statuses := make([]string, 0, len(items))
	for _, item := range items {
		status := strings.TrimSpace(instanceStatusByID[item.ID])
		if status != "" {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func sortTaskRecords(tasks []repo.TaskRecord) {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
}

func sortInstanceRecords(instances []repo.PipelineInstanceRecord) {
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].CreatedAt.Equal(instances[j].CreatedAt) {
			return instances[i].ID < instances[j].ID
		}
		return instances[i].CreatedAt.Before(instances[j].CreatedAt)
	})
}

func aggregateWorkspaceStatuses(statuses []string) string {
	if len(statuses) == 0 {
		return "not_started"
	}
	for _, candidate := range []string{"failed", "running", "waiting_human", "recovered"} {
		for _, status := range statuses {
			if status == candidate {
				return candidate
			}
		}
	}
	allCompleted := true
	for _, status := range statuses {
		if status != "completed" {
			allCompleted = false
			break
		}
	}
	if allCompleted {
		return "completed"
	}
	return "not_started"
}

func workspaceStageLabel(stageID string) string {
	switch strings.TrimSpace(stageID) {
	case "ceo_write_requirement":
		return "Doujia 写需求"
	case "pm_write_plan":
		return "产品经理写 PRD"
	case "architect_write_plan":
		return "架构师写架构书"
	case "architect_create_container":
		return "创建容器"
	case "split_module":
		return "拆分模块"
	case "test_all_modules":
		return "模块并行开发"
	case "write_global_test_data":
		return "编写全局测试数据"
	case "merge_code":
		return "合并代码"
	case "global_test_code":
		return "全局测试"
	case "acceptance":
		return "人工审核"
	case "write_code":
		return "编写代码"
	case "write_test_data":
		return "编写测试数据"
	case "test_code":
		return "测试代码"
	case "fork_prepare_delivery":
		return "并行准备"
	case "join_prepare_delivery":
		return "准备完成"
	case "fork_implementation":
		return "并行开发"
	case "join_implementation":
		return "并行收敛"
	default:
		if strings.TrimSpace(stageID) == "" {
			return "未命名节点"
		}
		return stageID
	}
}

func summarizeImprovementTitle(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "Untitled improvement"
	}
	lines := strings.Fields(content)
	if len(lines) == 0 {
		return "Untitled improvement"
	}
	title := strings.Join(lines, " ")
	if len([]rune(title)) > 40 {
		title = string([]rune(title)[:40])
	}
	return title
}

func normalizeWorkspaceInstanceStatus(instance repo.PipelineInstanceRecord, tasks []repo.TaskRecord) string {
	status := strings.TrimSpace(string(instance.Status))
	switch status {
	case "failed":
		return "failed"
	case "completed":
		return "completed"
	case "running":
		for _, task := range tasks {
			if task.Status == core.TaskStatusWaitingExternal {
				return "waiting_human"
			}
			if task.Result == core.TaskResultCodeBug {
				return "recovered"
			}
		}
		return "running"
	}
	for _, task := range tasks {
		switch task.Status {
		case core.TaskStatusWaitingExternal:
			return "waiting_human"
		case core.TaskStatusRunning, core.TaskStatusDispatched:
			return "running"
		case core.TaskStatusFailed:
			return "failed"
		}
		if task.Result == core.TaskResultCodeBug {
			return "recovered"
		}
	}
	if len(tasks) == 0 {
		return "not_started"
	}
	return "completed"
}

func normalizeWorkspaceTaskStatus(task repo.TaskRecord) string {
	switch task.Status {
	case core.TaskStatusPending:
		return "not_started"
	case core.TaskStatusWaitingExternal:
		return "waiting_human"
	case core.TaskStatusDispatched, core.TaskStatusRunning:
		return "running"
	case core.TaskStatusDone:
		if task.Result == core.TaskResultCodeBug {
			return "recovered"
		}
		return "completed"
	case core.TaskStatusFailed, core.TaskStatusBlocked:
		return "failed"
	default:
		return strings.TrimSpace(string(task.Status))
	}
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = time.Now
