import { useEffect, useState } from 'react';
import {
  getDevflowPluginRegistryState,
  getDevflowPluginValidationResult,
  uploadDevflowPipeline,
  uploadDevflowPluginPack,
} from '@/api/devflow-client';
import type {
  DevflowPluginRegistryStateView,
  DevflowPluginValidationResultView,
} from '@shared/devflow-api';
import { PluginRegistryPanel } from './PluginRegistryPanel';
import { PluginUploadPanel } from './PluginUploadPanel';
import { PluginValidationPanel } from './PluginValidationPanel';
import { Badge } from '@/components/ui/badge';
import { Spinner } from '@/components/ui/spinner';

const TemplatesPage: React.FC = () => {
  const [registryState, setRegistryState] = useState<DevflowPluginRegistryStateView | null>(null);
  const [validationResult, setValidationResult] = useState<DevflowPluginValidationResultView | null>(null);
  const [validationJobId, setValidationJobId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);

  const loadRegistryState = async () => {
    const state = await getDevflowPluginRegistryState();
    setRegistryState(state);
  };

  useEffect(() => {
    const init = async () => {
      try {
        await loadRegistryState();
      } finally {
        setLoading(false);
      }
    };
    void init();
  }, []);

  useEffect(() => {
    if (!validationJobId) return;
    let cancelled = false;
    const poll = async () => {
      const result = await getDevflowPluginValidationResult(validationJobId);
      if (cancelled) return;
      setValidationResult(result);
      if (result.status === 'pending' || result.status === 'running') {
        window.setTimeout(() => {
          void poll();
        }, 1500);
      } else if (result.status === 'succeeded') {
        await loadRegistryState();
      }
    };
    void poll();
    return () => {
      cancelled = true;
    };
  }, [validationJobId]);

  const totalResources = (registryState?.handlers.length ?? 0)
    + (registryState?.ops.length ?? 0)
    + (registryState?.roles.length ?? 0)
    + (registryState?.pipelines.length ?? 0);

  const handleUploadPluginPack = async (file: File) => {
    setUploading(true);
    try {
      const accepted = await uploadDevflowPluginPack(file);
      setValidationJobId(accepted.job_id);
    } finally {
      setUploading(false);
    }
  };

  const handleUploadPipelineJson = async (file: File) => {
    setUploading(true);
    try {
      const accepted = await uploadDevflowPipeline(file);
      setValidationJobId(accepted.job_id);
    } finally {
      setUploading(false);
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-7" />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-foreground">Plugin center</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Inspect runtime registrations, upload plugin packs, and validate standalone pipeline json files.
          </p>
        </div>
        <Badge variant="outline">{totalResources} registered resources</Badge>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-6 xl:grid-cols-[1.1fr_0.9fr]">
        <div className="min-h-0">
          <PluginRegistryPanel state={registryState} />
        </div>
        <div className="grid min-h-0 grid-cols-1 gap-6">
          <PluginUploadPanel
            uploading={uploading}
            onUploadPluginPack={handleUploadPluginPack}
            onUploadPipelineJson={handleUploadPipelineJson}
          />
          <PluginValidationPanel validation={validationResult} />
        </div>
      </div>
    </div>
  );
};

export default TemplatesPage;
