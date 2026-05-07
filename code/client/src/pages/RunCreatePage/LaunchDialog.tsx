import type { RunConfig, PipelineTemplateItem } from '@shared/api.interface';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Loader2, Rocket } from 'lucide-react';

interface LaunchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  config: RunConfig;
  templates: PipelineTemplateItem[];
  aiSummary: string;
  isCreating: boolean;
  error?: string | null;
  onConfirm: () => void;
}

const LaunchDialog: React.FC<LaunchDialogProps> = ({
  open,
  onOpenChange,
  config,
  templates,
  aiSummary,
  isCreating,
  error,
  onConfirm,
}) => {
  const templateName =
    templates.find((template) => template.id === config.templateId)?.name ??
    config.templateId;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>确认启动 Run</DialogTitle>
          <DialogDescription>
            确认后会在 Go 后端创建新的 Run，并进入工作台查看真实执行流程。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {error && (
            <Alert variant="destructive">
              <AlertTitle>启动失败</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <div className="rounded-md bg-accent/50 p-3">
            <p className="mb-1 text-xs font-medium text-accent-foreground">
              需求摘要
            </p>
            <p className="line-clamp-4 whitespace-pre-wrap text-sm text-foreground">
              {aiSummary || '还没有生成需求摘要，请先和 Doujia 对话。'}
            </p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs text-muted-foreground">模型</p>
              <p className="text-sm font-medium text-foreground">
                {config.modelProvider} / {config.modelName}
              </p>
            </div>

            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs text-muted-foreground">模板</p>
              <p className="text-sm font-medium text-foreground">{templateName}</p>
            </div>

            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs text-muted-foreground">目标仓库</p>
              <p className="truncate text-sm font-medium text-foreground">
                {config.targetRepo || '未配置'}
              </p>
            </div>

            <div className="rounded-md border p-3">
              <p className="mb-1 text-xs text-muted-foreground">网页注入</p>
              <p className="text-sm font-medium text-foreground">
                {config.enableWebInject ? '已启用' : '未启用'}
              </p>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            返回修改
          </Button>
          <Button onClick={onConfirm} disabled={isCreating}>
            {isCreating ? (
              <>
                <Loader2 className="size-4 animate-spin" />
                创建中...
              </>
            ) : (
              <>
                <Rocket className="size-4" />
                确认启动
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default LaunchDialog;
