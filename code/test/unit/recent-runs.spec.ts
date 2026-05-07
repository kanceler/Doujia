import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(__dirname, '..', '..');

describe('recent run history boundary', () => {
  it('keeps the old restored runs out of the visible recent run list', () => {
    const helperSource = readFileSync(join(root, 'client/src/api/recent-runs.ts'), 'utf8');
    const clientSource = readFileSync(join(root, 'client/src/api/devflow-client.ts'), 'utf8');

    expect(helperSource).toContain("RECENT_RUN_HISTORY_CUTOFF = '2026-05-06T14:25:30.000Z'");
    expect(helperSource).toContain('filterVisibleRecentRuns');
    expect(clientSource).toContain('filterVisibleRecentRuns(result.items)');
  });
});
