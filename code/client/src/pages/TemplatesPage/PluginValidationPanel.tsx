import type { DevflowPluginValidationResultView } from '@shared/devflow-api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

interface PluginValidationPanelProps {
  validation: DevflowPluginValidationResultView | null;
}

export const PluginValidationPanel: React.FC<PluginValidationPanelProps> = ({ validation }) => {
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="text-base">Validation result</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {!validation && (
          <div className="rounded-md border border-dashed border-border px-3 py-4 text-sm text-muted-foreground">
            Upload a plugin pack or pipeline json to start a validation job.
          </div>
        )}

        {validation && (
          <>
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-medium text-foreground">{validation.file_name}</div>
                <div className="text-xs text-muted-foreground">{validation.job_id}</div>
              </div>
              <Badge variant={validation.status === 'succeeded' ? 'default' : validation.status === 'failed' ? 'destructive' : 'secondary'}>
                {validation.status}
              </Badge>
            </div>

            <section className="space-y-2">
              <div className="text-sm font-medium text-foreground">activated_pipelines</div>
              <pre className="rounded-md border border-border bg-muted/30 p-3 text-xs">
{JSON.stringify({
  activated_handlers: validation.activated_handlers ?? [],
  activated_ops: validation.activated_ops ?? [],
  activated_roles: validation.activated_roles ?? [],
  activated_pipelines: validation.activated_pipelines ?? [],
}, null, 2)}
              </pre>
            </section>

            {(validation.errors?.length ?? 0) > 0 && (
              <section className="space-y-2">
                <div className="text-sm font-medium text-foreground">errors</div>
                <div className="space-y-2">
                  {validation.errors?.map((error) => (
                    <div
                      key={error}
                      className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive"
                    >
                      {error}
                    </div>
                  ))}
                </div>
              </section>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
};
