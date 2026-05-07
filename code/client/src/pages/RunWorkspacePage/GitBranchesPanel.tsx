import type {
  DevflowGitBranchesView,
  DevflowGitMergeView,
  DevflowGitModuleBranchView,
} from '@shared/devflow-api';
import { getGitBranchesRefreshState } from './git-branches-refresh-state';

interface GitBranchesPanelProps {
  data?: DevflowGitBranchesView | null;
  loading?: boolean;
}

const branchColors = ['#7c3aed', '#2563eb', '#16a34a', '#f97316', '#dc2626'];

function shortSha(value?: string): string {
  if (!value) return '';
  return value.length > 10 ? value.slice(0, 10) : value;
}

function moduleLabel(row: DevflowGitModuleBranchView): string {
  return row.module_name || row.module_id || row.agent_id || row.task_id || 'Unknown module';
}

function resultBadgeClass(result?: string, testPassed?: boolean): string {
  if (testPassed === false || result === 'failed' || result === 'error' || result === 'kfail') {
    return 'border-red-200 bg-red-50 text-red-700';
  }
  if (testPassed === true || result === 'kok' || result === 'ok' || result === 'completed') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700';
  }
  return 'border-slate-200 bg-slate-50 text-slate-600';
}

function buildRows(data?: DevflowGitBranchesView | null): DevflowGitModuleBranchView[] {
  if (!data) return [];
  const rows = [...(data.module_branches ?? [])];
  const existingCommits = new Set(rows.map((item) => item.commit).filter(Boolean));
  for (const item of data.merge?.modules ?? []) {
    if (item.commit && existingCommits.has(item.commit)) continue;
    rows.push(item);
  }
  return rows;
}

function GitGraphColumn({
  branchCount,
  hasMerge,
  hasBase,
}: {
  branchCount: number;
  hasMerge: boolean;
  hasBase: boolean;
}) {
  const rowHeight = 76;
  const baseRows = Math.max(branchCount, 1);
  const totalRows = baseRows + (hasMerge ? 1 : 0);
  const height = totalRows * rowHeight;
  const mainX = 28;
  const mergeX = 28;
  const baseY = hasBase ? 18 : rowHeight / 2;
  const mergeY = hasMerge ? height - rowHeight / 2 : height - 18;
  const visibleBranchCount = Math.min(Math.max(branchCount, 1), 4);
  const branchLanes = Array.from({ length: visibleBranchCount }, (_, index) => ({
    color: branchColors[index % branchColors.length],
    x: 58 + index * 9,
    y: rowHeight / 2 + index * rowHeight,
  }));

  return (
    <div className="flex h-full min-h-[180px] items-stretch justify-center">
      <svg
        width="96"
        height={height}
        viewBox={`0 0 96 ${height}`}
        aria-hidden="true"
        className="block min-h-full"
        preserveAspectRatio="xMidYMin meet"
      >
        {hasBase || hasMerge ? (
          <line
            x1={mainX}
            y1={baseY}
            x2={mainX}
            y2={hasMerge ? mergeY : height - 18}
            stroke="#7c3aed"
            strokeWidth="2.5"
          />
        ) : null}
        {hasBase ? <circle cx={mainX} cy={baseY} r="4.5" fill="#7c3aed" /> : null}
        {branchLanes.map((lane, index) => (
          <g key={`${lane.x}-${index}`}>
            <path
              d={`M ${mainX} ${baseY} C ${mainX + 10} ${baseY + 18}, ${lane.x - 14} ${
                lane.y - 18
              }, ${lane.x} ${lane.y}`}
              fill="none"
              stroke={lane.color}
              strokeWidth="2"
            />
            {hasMerge ? (
              <path
                d={`M ${lane.x} ${lane.y} C ${lane.x - 8} ${lane.y + 26}, ${
                  mergeX + 18
                } ${mergeY - 22}, ${mergeX} ${mergeY}`}
                fill="none"
                stroke={lane.color}
                strokeWidth="2"
              />
            ) : null}
            <circle cx={lane.x} cy={lane.y} r="4.5" fill={lane.color} />
          </g>
        ))}
        {branchCount > visibleBranchCount ? (
          <text x="44" y={height - 8} fill="#64748b" fontSize="10" fontWeight="600">
            +{branchCount - visibleBranchCount}
          </text>
        ) : null}
        {hasMerge ? (
          <g>
            <circle cx={mergeX} cy={mergeY} r="5" fill="#059669" />
            <circle cx={mergeX} cy={mergeY} r="8" fill="none" stroke="#bbf7d0" strokeWidth="2" />
          </g>
        ) : null}
      </svg>
    </div>
  );
}

function GraphCell({
  rowSpan,
  branchCount,
  hasMerge,
  hasBase,
}: {
  rowSpan: number;
  branchCount: number;
  hasMerge: boolean;
  hasBase: boolean;
}) {
  return (
    <td rowSpan={rowSpan} className="border-b border-slate-100 px-2 py-0 align-top">
      <GitGraphColumn branchCount={branchCount} hasMerge={hasMerge} hasBase={hasBase} />
    </td>
  );
}

function mergeModuleLabel(merge: DevflowGitMergeView): string {
  const modules = merge.modules?.map((item) => item.module_id || item.module_name).filter(Boolean);
  if (modules?.length) return modules.join(', ');
  return 'Merged modules';
}

export function GitBranchesPanel({ data, loading }: GitBranchesPanelProps) {
  const rows = buildRows(data);
  const merge = data?.merge;
  const hasMerge = Boolean(merge?.merged_commit);
  const graphRowSpan = Math.max(rows.length, 1) + (hasMerge ? 1 : 0);
  const hasRenderedData = Boolean(data);
  const { initialLoading } = getGitBranchesRefreshState(Boolean(loading), hasRenderedData);

  return (
    <section className="flex h-full min-h-0 flex-col rounded-lg border border-slate-200 bg-white">
      <div className="flex items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-slate-950">Git branches</h3>
          <p className="mt-0.5 text-xs text-slate-500">From real DoujiaGit output bags</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {merge?.merged_commit ? (
            <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700">
              merged {shortSha(merge.merged_commit)}
            </span>
          ) : null}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {initialLoading ? (
          <div className="p-6 text-sm text-slate-500">Loading real branch data...</div>
        ) : rows.length === 0 && !merge ? (
          <div className="flex min-h-[220px] items-center justify-center px-6 text-center">
            <div>
              <div className="text-sm font-medium text-slate-800">No real branch artifacts yet</div>
              <div className="mt-1 max-w-sm text-xs leading-5 text-slate-500">
                Git branches will appear after write_code or merge_code nodes produce coder_branch
                or merged_main_branch bags.
              </div>
            </div>
          </div>
        ) : (
          <table className="w-full min-w-[760px] table-fixed border-collapse text-left text-xs">
            <thead className="sticky top-0 z-10 bg-slate-50 text-[11px] uppercase tracking-wide text-slate-500">
              <tr>
                <th className="w-24 border-b border-slate-200 px-4 py-2 font-semibold">Graph</th>
                <th className="w-52 border-b border-slate-200 px-3 py-2 font-semibold">
                  Branch / Commit
                </th>
                <th className="w-44 border-b border-slate-200 px-3 py-2 font-semibold">Module</th>
                <th className="w-44 border-b border-slate-200 px-3 py-2 font-semibold">
                  Container
                </th>
                <th className="w-32 border-b border-slate-200 px-3 py-2 font-semibold">Result</th>
                <th className="w-44 border-b border-slate-200 px-3 py-2 font-semibold">
                  Task / Bag
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, index) => {
                return (
                  <tr
                    key={`${row.bag_id ?? row.task_id ?? index}-${row.commit ?? row.branch ?? index}`}
                    className="border-b border-slate-100"
                  >
                    {index === 0 ? (
                      <GraphCell
                        rowSpan={graphRowSpan}
                        branchCount={rows.length}
                        hasMerge={hasMerge}
                        hasBase={Boolean(row.base_commit || data?.base_commit)}
                      />
                    ) : null}
                    <td className="px-3 py-3 align-top">
                      {row.branch ? (
                        <span className="inline-flex max-w-full items-center rounded-md border border-blue-200 bg-blue-50 px-2 py-1 font-medium text-blue-700">
                          <span className="truncate">{row.branch}</span>
                        </span>
                      ) : null}
                      {row.commit ? (
                        <div className="mt-1 font-mono text-[11px] text-slate-600">
                          {shortSha(row.commit)}
                        </div>
                      ) : null}
                      {row.base_branch || row.base_commit ? (
                        <div className="mt-1 text-[11px] text-slate-400">
                          base {row.base_branch || ''} {shortSha(row.base_commit)}
                        </div>
                      ) : null}
                    </td>
                    <td className="px-3 py-3 align-top">
                      <div className="font-medium text-slate-800">{moduleLabel(row)}</div>
                      {row.module_id ? (
                        <div className="mt-1 font-mono text-[11px] text-slate-400">
                          {row.module_id}
                        </div>
                      ) : null}
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.container_id ? (
                        <div className="truncate font-mono text-[11px] text-slate-600">
                          {shortSha(row.container_id)}
                        </div>
                      ) : (
                        <span className="text-slate-300">-</span>
                      )}
                      {row.changed_files?.length ? (
                        <div className="mt-1 text-[11px] text-slate-400">
                          {row.changed_files.length} files
                        </div>
                      ) : null}
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.result || typeof row.test_passed === 'boolean' ? (
                        <span
                          className={`inline-flex rounded-full border px-2 py-1 text-[11px] font-medium ${resultBadgeClass(
                            row.result,
                            row.test_passed,
                          )}`}
                        >
                          {row.result || (row.test_passed ? 'test passed' : 'test failed')}
                        </span>
                      ) : (
                        <span className="text-slate-300">-</span>
                      )}
                    </td>
                    <td className="px-3 py-3 align-top">
                      {row.task_id ? (
                        <div className="truncate font-mono text-[11px] text-slate-600">
                          {row.task_id}
                        </div>
                      ) : null}
                      {row.bag_id ? (
                        <div className="mt-1 truncate font-mono text-[11px] text-slate-400">
                          {row.bag_id}
                        </div>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
              {merge?.merged_commit ? (
                <tr className="border-b border-emerald-100 bg-emerald-50/40">
                  {rows.length === 0 ? (
                    <GraphCell
                      rowSpan={graphRowSpan}
                      branchCount={merge.applied_commits?.length || 1}
                      hasMerge={hasMerge}
                      hasBase={Boolean(data?.base_commit)}
                    />
                  ) : null}
                  <td className="px-3 py-3 align-top">
                    <span className="inline-flex max-w-full items-center rounded-md border border-emerald-200 bg-white px-2 py-1 font-medium text-emerald-700">
                      <span className="truncate">{merge.base_branch || data?.base_branch || 'main'}</span>
                    </span>
                    <div className="mt-1 font-mono text-[11px] text-slate-700">
                      {shortSha(merge.merged_commit)}
                    </div>
                    {merge.applied_commits?.length ? (
                      <div className="mt-1 text-[11px] text-emerald-700">
                        applied {merge.applied_commits.length} commits
                      </div>
                    ) : null}
                  </td>
                  <td className="px-3 py-3 align-top">
                    <div className="font-medium text-slate-800">Merge result</div>
                    <div className="mt-1 text-[11px] text-slate-500">{mergeModuleLabel(merge)}</div>
                  </td>
                  <td className="px-3 py-3 align-top">
                    {merge.container_id ? (
                      <div className="truncate font-mono text-[11px] text-slate-600">
                        {shortSha(merge.container_id)}
                      </div>
                    ) : (
                      <span className="text-slate-300">-</span>
                    )}
                    {merge.created_at ? (
                      <div className="mt-1 text-[11px] text-slate-400">
                        {new Date(merge.created_at).toLocaleString()}
                      </div>
                    ) : null}
                  </td>
                  <td className="px-3 py-3 align-top">
                    {merge.result ? (
                      <span
                        className={`inline-flex rounded-full border px-2 py-1 text-[11px] font-medium ${resultBadgeClass(
                          merge.result,
                        )}`}
                      >
                        {merge.result}
                      </span>
                    ) : (
                      <span className="text-slate-300">-</span>
                    )}
                  </td>
                  <td className="px-3 py-3 align-top">
                    {merge.task_id ? (
                      <div className="truncate font-mono text-[11px] text-slate-600">
                        {merge.task_id}
                      </div>
                    ) : null}
                    {merge.bag_id ? (
                      <div className="mt-1 truncate font-mono text-[11px] text-slate-400">
                        {merge.bag_id}
                      </div>
                    ) : null}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        )}
      </div>

      <div className="flex items-center justify-between gap-3 border-t border-slate-100 px-4 py-3 text-xs text-slate-500">
        <div className="flex min-w-0 flex-wrap gap-3">
          {rows.slice(0, 5).map((row, index) => (
            <span
              key={`${row.branch ?? row.commit ?? index}-legend`}
              className="inline-flex items-center gap-1.5"
            >
              <span
                className="h-2 w-2 rounded-full"
                style={{ backgroundColor: branchColors[index % branchColors.length] }}
              />
              <span className="max-w-[160px] truncate">
                {row.branch || row.module_id || row.agent_id || 'branch'}
              </span>
            </span>
          ))}
          {merge?.merged_commit ? (
            <span className="inline-flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-emerald-600" />
              <span className="max-w-[160px] truncate">
                {merge.base_branch || data?.base_branch || 'main'}
              </span>
            </span>
          ) : null}
        </div>
        <div className="shrink-0 font-medium text-slate-700">
          {rows.length} branches
          {merge?.merged_commit ? ` / merged to ${merge.base_branch || data?.base_branch || 'main'}` : ''}
          {merge?.applied_commits?.length ? ` / ${merge.applied_commits.length} applied commits` : ''}
        </div>
      </div>
    </section>
  );
}
