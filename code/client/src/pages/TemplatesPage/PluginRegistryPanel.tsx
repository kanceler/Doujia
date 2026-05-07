import type { DevflowPluginRegistryStateView } from '@shared/devflow-api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

interface PluginRegistryPanelProps {
  state: DevflowPluginRegistryStateView | null;
}

export const PluginRegistryPanel: React.FC<PluginRegistryPanelProps> = ({ state }) => {
  const sections = [
    { title: 'handlers', items: state?.handlers ?? [], getKey: (item: { handler_id: string }) => item.handler_id, getLabel: (item: { handler_id: string }) => item.handler_id },
    { title: 'ops', items: state?.ops ?? [], getKey: (item: { op_id: string }) => item.op_id, getLabel: (item: { op_id: string }) => item.op_id },
    { title: 'roles', items: state?.roles ?? [], getKey: (item: { role_id: string }) => item.role_id, getLabel: (item: { role_id: string }) => item.role_id },
    { title: 'pipelines', items: state?.pipelines ?? [], getKey: (item: { pipeline_id: string }) => item.pipeline_id, getLabel: (item: { pipeline_id: string; name?: string }) => item.name || item.pipeline_id },
  ];

  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="text-base">Runtime registry</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {sections.map((section) => (
          <section key={section.title} className="space-y-2">
            <div className="flex items-center justify-between">
              <div className="text-sm font-medium text-foreground">{section.title}</div>
              <Badge variant="outline">{section.items.length}</Badge>
            </div>
            <div className="space-y-2">
              {section.items.length === 0 && (
                <div className="rounded-md border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                  No registered {section.title}
                </div>
              )}
              {section.items.map((item) => (
                <div
                  key={section.getKey(item as never)}
                  className="rounded-md border border-border bg-background px-3 py-2 text-xs"
                >
                  <div className="font-mono text-foreground">{section.getLabel(item as never)}</div>
                </div>
              ))}
            </div>
          </section>
        ))}
      </CardContent>
    </Card>
  );
};
