import { Link } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { getDevflowDemoRuns } from '@/api/devflow-client';
import type { DevflowRunView } from '@shared/devflow-api';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { Clock, GitBranch, Grid2X2, Plug, Plus } from 'lucide-react';

interface WorkspaceLeftNavProps {
  currentRun: DevflowRunView;
}

const workspaceNavItems = [
  { to: '/run/create', label: '新项目', icon: Plus },
  { to: '/demo', label: '导入 doujia-git', icon: GitBranch },
  { to: '/', label: '我的应用', icon: Grid2X2 },
  { to: '/templates', label: '插件', icon: Plug },
];

export const WorkspaceLeftNav: React.FC<WorkspaceLeftNavProps> = ({ currentRun }) => {
  const [recentRuns, setRecentRuns] = useState<DevflowRunView[]>([]);

  useEffect(() => {
    getDevflowDemoRuns()
      .then((res: { items: DevflowRunView[] }) => {
        setRecentRuns(res.items);
      })
      .catch(() => {
        setRecentRuns([]);
      });
  }, []);

  return (
    <ScrollArea className="h-full">
      <div className="p-3 space-y-4">
        <div>
          <div className="flex items-center gap-1.5 px-2 py-1.5 text-xs font-medium text-muted-foreground">
            <GitBranch className="size-3.5" />
            <span>当前项目</span>
          </div>
          <div className="mt-1 px-2 py-1.5 rounded-md bg-accent/50 text-sm font-medium text-accent-foreground truncate">
            {currentRun.run_id}
          </div>
        </div>

        <Separator />

        <div className="space-y-1">
          {workspaceNavItems.map((item) => (
            <Link
              key={item.label}
              to={item.to}
              className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-foreground hover:bg-accent/50 transition-colors"
            >
              <item.icon className="size-3.5 text-muted-foreground" />
              <span>{item.label}</span>
            </Link>
          ))}
        </div>

        <Separator />

        <div>
          <div className="flex items-center gap-1.5 px-2 py-1.5 text-xs font-medium text-muted-foreground">
            <Clock className="size-3.5" />
            <span>最近的项目</span>
          </div>
          <div className="mt-1 space-y-0.5">
            {recentRuns.slice(0, 8).map((item: DevflowRunView) => (
              <Link
                key={item.run_id}
                to={`/run/${item.run_id}/workspace`}
                className="block px-2 py-1.5 rounded-md text-sm text-foreground hover:bg-accent/50 transition-colors truncate"
              >
                {item.run_id}
              </Link>
            ))}
            {recentRuns.length === 0 && (
              <div className="px-2 py-1.5 text-xs text-muted-foreground">暂无最近项目</div>
            )}
          </div>
        </div>
      </div>
    </ScrollArea>
  );
};
