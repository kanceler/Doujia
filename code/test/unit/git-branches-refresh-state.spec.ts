import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { getGitBranchesRefreshState } from '../../client/src/pages/RunWorkspacePage/git-branches-refresh-state';

describe('git branches refresh state', () => {
  it('separates first load from background refresh', () => {
    expect(getGitBranchesRefreshState(false, false)).toEqual({
      initialLoading: false,
      refreshing: false,
    });
    expect(getGitBranchesRefreshState(true, false)).toEqual({
      initialLoading: true,
      refreshing: false,
    });
    expect(getGitBranchesRefreshState(true, true)).toEqual({
      initialLoading: false,
      refreshing: true,
    });
    expect(getGitBranchesRefreshState(false, true)).toEqual({
      initialLoading: false,
      refreshing: false,
    });
  });

  it('keeps background refresh visually silent in the panel', () => {
    const source = readFileSync(
      join(process.cwd(), 'client/src/pages/RunWorkspacePage/GitBranchesPanel.tsx'),
      'utf8',
    );

    expect(source).toContain('initialLoading');
    expect(source).not.toContain('refreshing');
  });
});
