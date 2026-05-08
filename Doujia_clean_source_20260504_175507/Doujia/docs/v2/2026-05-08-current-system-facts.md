# Current System Facts

> Date: 2026-05-08
>
> Scope: record what the new system has already implemented and what has
> already been verified in the current worktree, so the next decision can be:
> continue into `awaiting_acceptance / iteration`, or pause and harden the
> existing system first.

---

## 1. Plain Summary

The current worktree has already moved the runtime onto the new
DoujiaGit-fact-based scheduler model and has a working full-delivery JSON flow
that reaches `awaiting_acceptance`.

In practical terms:

- the real full-delivery registry now lives in
  `internal/orchestrator/testdata/full_delivery/pipeline_full_delivery.spec.json`
- the legacy task prefix can hand off into the JSON pipeline graph
- the JSON full-delivery flow can run through fanout, merge, global test,
  recover, and output-handler repair
- the system can complete the root pipeline into `awaiting_acceptance`
- the acceptance manager currently has only a basic shell for
  `approve / continue`; the new archive-baseline iteration semantics are not
  implemented yet

---

## 2. Core Facts Already Implemented

### 2.1 Scheduler Truth

The scheduler truth is already centered on DoujiaGit facts:

- `TaskSnapshot`
- active `Ref`
- active frontier members
- processing decisions
- fact-derived advancement and projection sync

Legacy task/run/pipeline-instance statuses still exist, but current scheduling
work no longer treats them as the primary authority.

### 2.2 Full-Delivery Registry Source Of Truth

The real full-delivery registry fixture is now:

`internal/orchestrator/testdata/full_delivery/pipeline_full_delivery.spec.json`

The app bootstrap path and related tests now resolve to that fixture instead of
the removed `docs/v2/pipeline_full_delivery.spec.json`.

### 2.3 Full-Delivery Mainline

The new full-delivery flow has verified coverage for:

- legacy main-task prefix
- `architect_split_modules`
- three-way fanout:
  - front module
  - backend module group
  - global test data
- backend foreach fanout/fanin
- `merge_code`
- `global_test_code`
- root completion into `awaiting_acceptance`

### 2.4 Recover / Repair Semantics

The current system already has working coverage for:

- child-pipeline local recover loops
- owner-handler repair and retry
- partial repair with reusable sibling bags
- root-visible exported output handlers
- post-delivery repair of an already-completed child pipeline, with refreshed
  outputs propagating back into the completed root instance

### 2.5 Acceptance Manager Shell

`RunManager` already has the basic acceptance APIs:

- `ApproveAcceptance(...)`
- `ContinueIteration(...)`

Current behavior:

- `ApproveAcceptance(...)` marks the run completed
- `ContinueIteration(...)` increments `CurrentIterationNo`, clears the latest
  acceptance checkpoint, records a continuation message, and creates a new
  `ceo_write_requirement` task for the next iteration

What it does **not** do yet:

- archive the active ref at decision time
- persist an iteration baseline record that points at an archived ref/frontier
- feed that archived baseline into `requirement v2`

---

## 3. Fresh Verification Evidence

The following commands were re-run in this worktree and passed on
2026-05-08:

### 3.1 Pipeline Fixture And Legacy-Adapter Coverage

```bash
go test ./internal/pipeline -run 'TestLoadRegistrySpecFullDeliveryJSON|TestFullDeliveryMergeCodeCarriesRequiredContext|TestLoadJSONRegistryLooksUpDefinitions|TestLegacyAdapterCompilesFullDeliveryTaskPrefix|TestNewFullDeliveryJSONFixtureLoads' -count=1
```

Result:

- PASS

### 3.2 Full-Delivery Mainline Scheduling

```bash
go test ./internal/orchestrator -run 'TestFullDeliveryJSONRegistryDrivesLegacyMainTaskPrefix|TestFullDeliveryJSONDispatchesThreeWayFanoutAfterSplit|TestFullDeliveryJSONBackendGroupReturnsOnlyAfterAllModuleChildrenComplete|TestFullDeliveryJSONDispatchesMergeCodeAfterThreeBranchReturns|TestFullDeliveryJSONDispatchesGlobalTestCodeAfterMergeReturn|TestFullDeliveryJSONAwaitsAcceptanceAfterGlobalTestCodeReturn|TestLegacyTerminalSnapshotProjectionExposesRootInputBags' -count=1
```

Result:

- PASS

### 3.3 Recover / Repair / Output-Handler Coverage

```bash
go test ./internal/orchestrator -run 'TestMergeChildInstanceHandlersRequiresExplicitOutputHandlers|TestPipelineBugBubblesToParentReceivedHandler|TestPipelineJSONRecoverRepairPreservesFailedSnapshotAndRetriesWithReplacementBag|TestPipelineJSONPartialRepairPreservesReusableSiblingBag|TestFullDeliveryJSONGlobalTestKbugDispatchesDebugAndRetries|TestFullDeliveryJSONGlobalTestRecoverStillCompletesDelivery|TestFullDeliveryJSONRootOutputHandlerRepairsDeliveredGlobalTestChild' -count=1
```

Result:

- PASS

### 3.4 App Bootstrap / Bundled Registry Smoke

```bash
go test ./internal/app -run 'TestBundledPipelineRegistryPathPointsToRealFullDeliveryFixture|TestNewBootstrapWithOptionsLoadsFullDeliveryJSONTaskPrefix|TestProtocolMockAgentsRunFullDeliveryJSON' -count=1
```

Result:

- PASS

---

## 4. Important Boundaries That Still Exist

These are the main reasons the next decision still matters.

### 4.1 Acceptance-After-Delivery Is Only Partially Implemented

The system can reach `awaiting_acceptance`, but the semantics after that are
still minimal.

Today the system does **not** yet define:

- decision-time archiving of the active ref
- archive refs per acceptance decision
- structured iteration baselines derived from archived frontiers
- `requirement v2` semantics that explicitly combine:
  - prior accepted/continued baseline
  - prior delivery result
  - user feedback / new demand

### 4.2 Some Older Tests Still Reflect Retired Semantics

There are still older integration tests outside the focused verified set that
assume:

- old legacy/from-control semantics
- pre-JSON fixture locations
- older task IDs or stub flow shapes

That means the new system is already functional in the verified paths, but the
broader test surface still needs migration and cleanup.

### 4.3 The Next Iteration Rule Is Not Locked In Code

The current code supports "continue into a new iteration" mechanically, but it
does **not** yet encode the newer product rule we discussed:

- do not archive before the user decides
- archive the current active ref when the user decides
- both `approve` and `continue` archive
- the next iteration always restarts from `ceo_write_requirement`
- that task should produce a `requirement v2`

That rule is still a design consensus, not implemented behavior.

---

## 5. What This Means For The Next Decision

Based on the verified state in this worktree:

- it is reasonable to continue into `awaiting_acceptance / iteration` design
  and implementation now
- it is **not** necessary to stop because the new full-delivery mainline is
  missing core scheduler behavior
- if we choose to pause for hardening first, the main hardening targets are:
  - acceptance/iteration data model
  - archive/baseline semantics
  - cleanup of older integration tests still tied to retired semantics

In short:

> The delivery-to-acceptance path is already strong enough to justify designing
> the next-stage `awaiting_acceptance / iteration` rules. The biggest remaining
> gap is no longer the delivery scheduler itself; it is the post-acceptance
> archive-and-baseline model.
