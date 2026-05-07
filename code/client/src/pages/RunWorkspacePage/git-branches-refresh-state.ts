export interface GitBranchesRefreshState {
  initialLoading: boolean;
  refreshing: boolean;
}

export function getGitBranchesRefreshState(
  loading: boolean,
  hasRenderedData: boolean,
): GitBranchesRefreshState {
  return {
    initialLoading: loading && !hasRenderedData,
    refreshing: loading && hasRenderedData,
  };
}
