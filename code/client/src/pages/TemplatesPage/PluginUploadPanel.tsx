import { useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';

interface PluginUploadPanelProps {
  uploading?: boolean;
  onUploadPluginPack: (file: File) => Promise<void> | void;
  onUploadPipelineJson: (file: File) => Promise<void> | void;
}

export const PluginUploadPanel: React.FC<PluginUploadPanelProps> = ({
  uploading = false,
  onUploadPluginPack,
  onUploadPipelineJson,
}) => {
  const pluginPackRef = useRef<HTMLInputElement | null>(null);
  const pipelineJsonRef = useRef<HTMLInputElement | null>(null);
  const [pluginPackLabel, setPluginPackLabel] = useState('');
  const [pipelineJsonLabel, setPipelineJsonLabel] = useState('');

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Upload resources</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <section className="space-y-2">
          <div className="text-sm font-medium text-foreground">plugin pack</div>
          <Input
            ref={pluginPackRef}
            type="file"
            accept=".zip"
            onChange={(event) => {
              const file = event.target.files?.[0];
              setPluginPackLabel(file?.name ?? '');
            }}
          />
          {pluginPackLabel && <div className="text-xs text-muted-foreground">{pluginPackLabel}</div>}
          <Button
            onClick={() => {
              const file = pluginPackRef.current?.files?.[0];
              if (file) {
                void onUploadPluginPack(file);
              }
            }}
            disabled={uploading}
          >
            Upload plugin pack
          </Button>
        </section>

        <section className="space-y-2">
          <div className="text-sm font-medium text-foreground">pipeline json</div>
          <Input
            ref={pipelineJsonRef}
            type="file"
            accept=".json,application/json"
            onChange={(event) => {
              const file = event.target.files?.[0];
              setPipelineJsonLabel(file?.name ?? '');
            }}
          />
          {pipelineJsonLabel && <div className="text-xs text-muted-foreground">{pipelineJsonLabel}</div>}
          <Button
            variant="outline"
            onClick={() => {
              const file = pipelineJsonRef.current?.files?.[0];
              if (file) {
                void onUploadPipelineJson(file);
              }
            }}
            disabled={uploading}
          >
            Upload pipeline json
          </Button>
        </section>
      </CardContent>
    </Card>
  );
};
