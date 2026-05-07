import type { DevflowRunView } from '@shared/devflow-api';

export const RECENT_RUN_HISTORY_CUTOFF = '2026-05-06T14:25:30.000Z';

const recentRunHistoryCutoffTime = Date.parse(RECENT_RUN_HISTORY_CUTOFF);

export function filterVisibleRecentRuns(runs: DevflowRunView[]): DevflowRunView[] {
  return runs.filter((run) => Date.parse(run.created_at) >= recentRunHistoryCutoffTime);
}
