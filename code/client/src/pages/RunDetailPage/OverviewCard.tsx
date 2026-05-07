import type { RunItem, RunStatus } from '@shared/api.interface';
import { Badge } from '@client/src/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@client/src/components/ui/card';

const STATUS_CONFIG: Record<RunStatus, { label: string; className: string }> = {
  pending: { label: '等待中', className: 'bg-status-pending/15 text-status-pending border-status-pending/30' },
  running: { label: '运行中', className: 'bg-status-running/15 text-status-running border-status-running/30' },
  success: { label: '成功', className: 'bg-status-success/15 text-status-success border-status-success/30' },
  failed: { label: '失败', className: 'bg-status-failed/15 text-status-failed border-status-failed/30' },
  rejected: { label: '已拒绝', className: 'bg-status-rejected/15 text-status-rejected border-status-rejected/30' },
  recovered: { label: '已恢复', className: 'bg-status-recovered/15 text-status-recovered border-status-recovered/30' },
};

interface OverviewCardProps {
  run: RunItem;
}

const OverviewCard: React.FC<OverviewCardProps> = ({ run }) => {
  const statusCfg = STATUS_CONFIG[run.status];
  const createdAt = new Date(run.createdAt).toLocaleString('zh-CN');
  const updatedAt = new Date(run.updatedAt).toLocaleString('zh-CN');

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between gap-4">
          <div className="flex-1 min-w-0">
            <CardTitle className="text-xl font-bold tracking-tight truncate">
              {run.name}
            </CardTitle>
            {run.demandSummary && (
              <p className="mt-1 text-sm text-muted-foreground line-clamp-2">
                {run.demandSummary}
              </p>
            )}
          </div>
          <Badge className={`border ${statusCfg.className} shrink-0`}>
            {statusCfg.label}
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <div className="text-muted-foreground text-xs">模型</div>
            <div className="font-medium truncate" title={run.config?.modelName}>
              {run.config?.modelName || '-'}
            </div>
          </div>
          <div>
            <div className="text-muted-foreground text-xs">目标仓库</div>
            <div className="font-medium truncate" title={run.config?.targetRepo}>
              {run.config?.targetRepo || '-'}
            </div>
          </div>
          <div>
            <div className="text-muted-foreground text-xs">当前 Ref</div>
            <div className="font-mono text-xs truncate" title={run.ref || ''}>
              {run.ref || '-'}
            </div>
          </div>
          <div>
            <div className="text-muted-foreground text-xs">创建时间</div>
            <div className="font-medium text-xs">{createdAt}</div>
          </div>
        </div>
        <div className="mt-3 pt-3 border-t border-border text-xs text-muted-foreground">
          更新于 {updatedAt}
        </div>
      </CardContent>
    </Card>
  );
};

export default OverviewCard;
