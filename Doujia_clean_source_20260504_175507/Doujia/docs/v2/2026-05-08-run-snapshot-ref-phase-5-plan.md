# Run Snapshot Ref Phase 5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retire the legacy no-DoujiaGit task-status scheduler and make DoujiaGit facts the only scheduler truth.

**Architecture:** Phase 5 keeps `task` as the execution unit and keeps Phase 4 frontier replacement as the scheduler engine. The main change is no longer a dual-mode boundary: orchestrator scheduling requires a DoujiaGit repository, advances from active ref facts, and treats task/run/pipeline-instance statuses as operational projections only. Missing DoujiaGit configuration becomes a startup/test setup error rather than a legacy fallback.

**Tech Stack:** Go, orchestrator active-ref scheduler, DoujiaGit repository, SQLite-backed fact layer, legacy task/run repositories as projections, pipeline instance repository as materialized cache, debug/archive views, orchestrator and DoujiaGit tests.

---

## 1. Plain Phase 5 Summary

Phases 1-4 make facts expressive enough to drive scheduling:

- snapshots record committed runtime arrivals
- refs record the active frontier
- processing decisions record consumed and produced members
- frontier replacement explains fanout, merge, call-return, and recover

Phase 5 removes the old scheduler authority.

Before Phase 5:

- some paths can still treat task/run/pipeline-instance status as scheduling truth
- some tests and lightweight paths may run with `s.doujiaGit == nil`
- `advanceReadyTasks()` can still represent the old task-status scheduler

After Phase 5:

- DoujiaGit is required for scheduling
- active ref facts are the only scheduler input
- task/run/pipeline-instance statuses are projections and operational caches
- stale legacy status must not dispatch extra work, block valid work, or mark a run failed
- old nil-DoujiaGit task mode is removed or rewritten to use an in-memory DoujiaGit repository

---

## 2. Required Confirmation Before Implementation

Please confirm these five rules before implementation.

### Rule 1: DoujiaGit Facts Are the Only Scheduler Truth

The scheduler's source of truth is:

- active `doujiagit.Ref`
- `Ref.FrontierMemberSnapshotIDs`
- `doujiagit.TaskSnapshot`
- `doujiagit.SnapshotProcessingDecision`
- legal frontier replacement validation

Legacy task/run statuses may be read for display, logging, and projection sync, but never for readiness or continuation decisions.

### Rule 2: DoujiaGit Repository Is Required

`s.doujiaGit == nil` is no longer a supported scheduling mode.

Required behavior:

- orchestration entrypoints that need scheduling must fail fast when the repository is missing
- tests that previously relied on nil DoujiaGit must install `doujiagit.NewMemoryRepository()`
- CLI/demo/runtime setup must provide SQLite-backed or memory-backed DoujiaGit
- no new fallback to task-status scheduling may be added

### Rule 3: Legacy Statuses Are Still Written as Projections

Phase 5 does not delete these fields yet:

- `core.Task.Status`
- `core.PipelineRun.Status`
- `core.PipelineInstance.Status`

They remain useful for UI, debug pages, progress display, and old storage readers. The change is authority, not immediate storage removal.

### Rule 4: Projection Can Be Rebuilt From Facts

For every run, projection should be derivable from facts:

- active members
- unconsumed members
- consumed decisions
- terminal decisions
- in-flight dispatched tasks
- acceptance or failure facts

If projection and legacy status disagree, Phase 5 must prefer facts and refresh legacy status only when facts clearly prove the projected state.

### Rule 5: Pipeline Instance State Is a Materialized Cache

Pipeline instance repositories still exist and are still updated.

However:

- continuation must be explained by active frontier facts
- instance state is a cache/materialized view for bag maps and UI
- a stale instance status must not by itself create or suppress scheduler decisions

Full deletion of instance status storage is deferred. Scheduler authority is removed now.

---

## 3. Phase 5 Outcome

When Phase 5 is complete:

1. Orchestrator scheduling requires DoujiaGit.
2. `advanceReadyTasks()` is deleted, or reduced to an unused migration/test helper with no production call sites.
3. Active-ref projection can explain whether a run is runnable, waiting, terminal, failed, or awaiting acceptance.
4. Stale task/run/pipeline-instance statuses do not cause duplicate dispatch, missed dispatch, or false failure.
5. Legacy statuses remain available as projections and are refreshed from fact truth where practical.
6. Tests no longer rely on nil-DoujiaGit task scheduling.
7. Debug views can show both fact truth and legacy projection status.

---

## 4. Phase 5 Scope

### In Scope

- Require DoujiaGit repository for scheduling entrypoints.
- Remove nil-DoujiaGit fallback behavior.
- Remove or isolate the old task-readiness scheduler.
- Add a fact-derived run projection.
- Audit post-feedback, resume, recover, and pipeline-instance advancement paths for legacy status reads.
- Keep legacy status writeback as projection.
- Rewrite old nil-DoujiaGit tests to use memory DoujiaGit or delete them if they only prove retired behavior.
- Add tests proving stale legacy statuses do not control scheduling.

### Out of Scope

- Do not remove task/run/pipeline-instance status fields yet.
- Do not remove task as execution unit.
- Do not change agent/runtime protocol.
- Do not introduce a new event-sourcing replay engine.
- Do not redesign bag contracts.
- Do not implement missing Phase 4 frontier algebra in Phase 5. Finish Phase 4 exit gate first.
- Do not fix unrelated fixture/stub failures unless they block direct Phase 5 coverage.

---

## 5. File Responsibilities

### Orchestrator Fact Scheduler

- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
  - Add `ErrDoujiaGitRequired`.
  - Add `requireDoujiaGit()` helper.
  - Remove scheduling fallbacks based on `s.doujiaGit == nil`.
  - Replace remaining production `advanceReadyTasks()` calls with active-ref/fact advancement.
  - Add fact-derived run projection helpers.
  - Sync legacy status from projection after fact-mode decisions.

- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_internal_test.go`
  - Add focused tests for required DoujiaGit setup, projection derivation, stale status immunity, and no legacy fallback.

- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_test.go`
  - Rewrite old nil-DoujiaGit tests to install a memory DoujiaGit repository.
  - Add integration tests for root, fanout/merge, call-return, recover, run completion, and stale legacy status.

- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/exception_handlers_test.go`
  - Add tests proving debug/recover decisions use facts even if legacy statuses are stale.

### Runtime, Demo, and Test Setup

- Modify if needed: `Doujia_clean_source_20260504_175507/Doujia/cmd/...`
  - Ensure every runnable orchestrator setup creates or opens a DoujiaGit repository.

- Modify if needed: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/*_test.go`
  - Replace no-DoujiaGit service fixtures with fact-mode fixtures.

### DoujiaGit Query and Debug

- Modify if needed: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/query.go`
  - Add query helpers only if projection code would otherwise duplicate graph traversal logic.

- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/debug_http.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/debug_http_test.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/debug_ui.go`
  - Show fact scheduler truth, legacy projection status, and stale-status warnings.

### Documentation

- Modify: `Doujia_clean_source_20260504_175507/Doujia/docs/v2/2026-05-08-run-snapshot-ref-current-implementation-status.md`
  - Record that no-DoujiaGit task scheduling has been retired.

- Modify: `Doujia_clean_source_20260504_175507/Doujia/docs/v2/2026-05-08-run-snapshot-ref-phase-5-plan.md`
  - Update checkboxes and record compatibility compromises discovered during implementation.

---

## 6. Proposed Internal Orchestrator Shape

Add internal error/helper:

```go
var ErrDoujiaGitRequired = errors.New("doujiagit repository is required for scheduling")

func (s *Service) requireDoujiaGit() (doujiagit.Repository, error) {
	if s.doujiaGit == nil {
		return nil, ErrDoujiaGitRequired
	}
	return s.doujiaGit, nil
}
```

Add projection type:

```go
type runFactProjection struct {
	RunID                  core.RunID
	RefName                string
	FrontierSnapshotID     string
	ActiveSnapshotIDs       []string
	UnconsumedSnapshotIDs   []string
	InFlightTaskIDs         []string
	TerminalSnapshotIDs     []string
	FailedSnapshotIDs       []string
	AwaitingAcceptance      bool
	HasAdvanceableWork      bool
	LegacyRunStatus         core.RunStatus
	LegacyProjectionIsStale bool
}
```

Add internal helpers:

```go
func (s *Service) buildRunFactProjection(ctx context.Context, run core.PipelineRun) (runFactProjection, error)
func (s *Service) activeRefProjection(ctx context.Context, run core.PipelineRun) (runFactProjection, error)
func (s *Service) syncLegacyStatusProjection(ctx context.Context, run core.PipelineRun, projection runFactProjection) error
func (s *Service) advanceByFacts(ctx context.Context, run core.PipelineRun) error
```

Expected behavior:

- missing DoujiaGit returns `ErrDoujiaGitRequired`
- `advanceByFacts()` calls active-ref advancement and projection sync
- projection helpers do not dispatch by themselves; they describe current fact truth
- status sync happens after fact decisions, not before
- old task-readiness scanning is not a scheduler fallback

---

## 7. Phase 5 Tasks

### Task 1: Make DoujiaGit Required

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_internal_test.go`

- [ ] Add tests for `requireDoujiaGit()`:
  - missing repository returns `ErrDoujiaGitRequired`
  - configured repository is returned

- [ ] Add tests for scheduling entrypoints:
  - `advanceByFacts()` without repository returns `ErrDoujiaGitRequired`
  - feedback path without repository fails fast instead of silently using task readiness

- [ ] Implement `ErrDoujiaGitRequired` and `requireDoujiaGit()`.

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'TestRequireDoujiaGit|Test.*DoujiaGitRequired' -count=1
```

**Acceptance**

- no scheduler entrypoint silently supports nil DoujiaGit
- tests must opt into a memory or SQLite DoujiaGit repository

### Task 2: Remove Legacy Task-Readiness Scheduling

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_internal_test.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_test.go`

- [ ] Search for all `advanceReadyTasks()` call sites.

- [ ] Add tests proving:
  - an unrelated legacy ready task does not dispatch without active-ref facts
  - active-ref facts dispatch even when legacy task status is stale

- [ ] Replace production call sites with `advanceByFacts()`.

- [ ] Delete `advanceReadyTasks()` if no longer needed.

- [ ] If deletion is too large for one step, mark it as legacy-only and make tests prove it has no production call sites.

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'Test.*LegacyReadyTaskIgnored|Test.*StaleTaskStatus|Test.*AdvanceByFacts' -count=1
```

**Acceptance**

- task status cannot act as scheduler truth
- old no-DoujiaGit task scheduler is gone from production flow

### Task 3: Add Fact-Derived Run Projection

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_internal_test.go`

- [ ] Add tests for `buildRunFactProjection()`:
  - active ref with one unconsumed member reports `HasAdvanceableWork`
  - active ref with consumed members only reports no advanceable work without failure
  - terminal decision reports terminal snapshot
  - failed terminal snapshot reports failed snapshot
  - missing DoujiaGit returns `ErrDoujiaGitRequired`

- [ ] Implement `runFactProjection`.

- [ ] Implement projection loading from:
  - current ref
  - active member snapshots
  - processing decisions for the active ref
  - current legacy run status for comparison only

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'TestRunFactProjection|TestActiveRefProjection' -count=1
```

**Acceptance**

- fact projection can explain runnable/waiting/terminal state without task readiness scanning
- no-work is represented as waiting/running, not failed

### Task 4: Demote Task Status From Scheduling

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_test.go`

- [ ] Add an integration test:
  - configure memory DoujiaGit
  - create an active unconsumed snapshot
  - deliberately set the corresponding legacy task status to a stale non-ready value
  - run advancement
  - assert the fact is consumed and the correct follow-up task is dispatched

- [ ] Add an integration test:
  - configure memory DoujiaGit
  - mark an unrelated legacy task ready
  - keep active ref with no unconsumed member
  - run advancement
  - assert no extra dispatch happens

- [ ] Remove readiness checks that depend on `core.Task.Status` for DoujiaGit scheduling.

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'Test.*StaleTaskStatus|Test.*LegacyReadyTaskIgnoredInFactMode|TestOnFeedback.*ActiveRef' -count=1
```

**Acceptance**

- stale task status cannot block fact-mode work
- unrelated ready task status cannot create fact-mode work

### Task 5: Demote Run Status From Scheduling

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_test.go`

- [ ] Add a test:
  - configure memory DoujiaGit
  - set legacy run status to failed
  - keep active ref with legal unconsumed work
  - run advancement
  - assert legal work still advances

- [ ] Add a test:
  - active ref has no unconsumed members
  - run status remains running/waiting unless facts prove terminal/failure

- [ ] Add a test:
  - final frontier/decision indicates awaiting acceptance
  - projection sync updates legacy run status to awaiting acceptance

- [ ] Implement run-status projection sync after active-ref advancement.

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'Test.*StaleRunStatus|Test.*RunProjection|Test.*AwaitingAcceptance' -count=1
```

**Acceptance**

- run status is derived, not authoritative
- no-work still does not mean failed or blocked

### Task 6: Demote Pipeline Instance Status From Scheduling

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator.go`
- Test: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_test.go`

- [ ] Add a pipeline-instance test:
  - configure memory DoujiaGit
  - create child/transition snapshots and decisions that prove continuation
  - set instance status to a stale non-advanceable value
  - assert active frontier facts still drive the continuation

- [ ] Add a test:
  - instance status says complete
  - active ref has no child completion or return snapshot
  - assert no parent continuation is dispatched from instance status alone

- [ ] Update pipeline-instance advancement to read instance state only as materialized cache for bag maps and metadata.

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'TestPipelineInstance.*FactTruth|TestPipelineInstance.*StaleStatus' -count=1
```

**Acceptance**

- pipeline instance status alone cannot create or suppress scheduling
- existing instance bag maps remain usable

### Task 7: Rewrite No-DoujiaGit Fixtures and Runtime Setup

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_test.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/orchestrator/orchestrator_internal_test.go`
- Modify if needed: `Doujia_clean_source_20260504_175507/Doujia/cmd/...`

- [ ] Find tests that construct `Service` without calling `SetDoujiaGitRepository`.

- [ ] For tests that still represent valid behavior, add `doujiagit.NewMemoryRepository()`.

- [ ] For tests that only prove old task-mode scheduling, delete or rewrite them to assert fact-mode scheduling.

- [ ] Ensure runnable commands/demos configure SQLite-backed DoujiaGit.

- [ ] Run:

```bash
go test ./internal/orchestrator -run 'Test.*DoujiaGitRequired|Test.*ActiveRef|Test.*LegacyTaskDriven' -count=1
```

**Acceptance**

- old no-DoujiaGit task scheduling tests are gone
- every valid orchestrator setup has a DoujiaGit repository

### Task 8: Debug and Archive Visibility

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/debug_http.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/debug_http_test.go`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/debug_ui.go`
- Modify if needed: `Doujia_clean_source_20260504_175507/Doujia/internal/doujiagit/query.go`

- [ ] Add debug HTTP tests that show:
  - fact scheduler truth
  - active fact projection
  - legacy run status
  - stale projection warning when available

- [ ] Update debug UI labels so users can tell:
  - fact truth
  - legacy projection
  - stale or synced status

- [ ] Run:

```bash
go test ./internal/doujiagit -run 'TestDebug|TestQuery' -count=1
```

**Acceptance**

- users can inspect why facts and legacy statuses disagree
- debug output does not imply legacy status is scheduler truth

### Task 9: Phase Boundary and Documentation

**Files:**
- Modify: `Doujia_clean_source_20260504_175507/Doujia/docs/v2/2026-05-08-run-snapshot-ref-current-implementation-status.md`
- Modify: `Doujia_clean_source_20260504_175507/Doujia/docs/v2/2026-05-08-run-snapshot-ref-phase-5-plan.md`

- [ ] Add notes that Phase 5 retires no-DoujiaGit task scheduling.

- [ ] Add notes that Phase 5 does not remove status fields.

- [ ] Add notes that full event replay and storage removal are deferred.

- [ ] Record any compatibility compromise found during implementation.

- [ ] Run final verification commands.

**Acceptance**

- reviewers can tell Phase 5 is scheduler authority cleanup, not status deletion
- Phase 6 can focus on replay/hardening/storage cleanup if needed

---

## 8. Verification Commands

Run DoujiaGit tests:

```bash
go test ./internal/doujiagit/... -count=1
```

Expected:

- PASS

Run focused orchestrator truth-boundary tests:

```bash
go test ./internal/orchestrator -run 'TestRequireDoujiaGit|TestRunFactProjection|Test.*Stale.*Status|Test.*DoujiaGitRequired|Test.*LegacyReadyTaskIgnored|TestPipelineInstance.*FactTruth' -count=1
```

Expected:

- PASS

Run broader orchestrator tests:

```bash
go test ./internal/orchestrator/... -count=1
```

Expected:

- PASS after known fixture/stub issues are fixed, or failures limited to those pre-existing issues.

Run shared packages:

```bash
go test ./internal/core/... ./internal/orchestrator/... ./internal/doujiagit/... -count=1
```

Expected:

- PASS after known fixture/stub issues are fixed, or failures explicitly documented as unrelated.

---

## 9. Phase 5 Review Questions

- [ ] Does any scheduling entrypoint still silently work with `s.doujiaGit == nil`?
- [ ] Can a stale task status block valid active-ref work?
- [ ] Can an unrelated ready task status dispatch work that facts do not justify?
- [ ] Can a stale run status mark a fact-runnable run failed?
- [ ] Is no-work/no-unconsumed-member still treated as waiting, not failure?
- [ ] Is pipeline instance state used as a cache, not scheduler authority?
- [ ] Can debug views explain fact truth versus legacy projection?
- [ ] Did we avoid deleting status fields before a later cleanup phase?

---

## 10. Exit Gate

Do not start Phase 6 until:

1. DoujiaGit is required for orchestrator scheduling.
2. No production scheduling path falls back to task-status readiness.
3. `advanceReadyTasks()` is deleted or has no production call sites.
4. Stale task, run, and pipeline-instance statuses cannot control scheduling.
5. Legacy statuses are still written as projections for UI/debug compatibility.
6. Tests no longer depend on no-DoujiaGit task scheduling.
7. Debug views clearly show fact truth and legacy projection status.
8. Remaining work can be described as replay/hardening/status storage cleanup, not scheduler truth demotion.
