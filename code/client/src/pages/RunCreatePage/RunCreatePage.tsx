import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { logger } from '@lark-apaas/client-toolkit/logger';
import type {
  PipelineTemplateItem,
  RegistrationStateItem,
  RunConfig,
} from '@shared/api.interface';
import type { DevflowProjectView } from '@shared/devflow-api';
import { Button } from '@/components/ui/button';
import { CheckCircle2 } from 'lucide-react';
import {
  createDevflowProject,
  createDevflowRun,
  getDevflowDemoRuns,
  getDevflowProjects,
} from '@/api/devflow-client';
import ConfigPanel from './ConfigPanel';
import LaunchDialog from './LaunchDialog';

interface DemoRunShortcut {
  id: string;
  name: string;
  description: string;
}

const DEFAULT_TARGET_REPO = 'local:self';

const DEFAULT_CONFIG: RunConfig = {
  projectId: '',
  projectName: '',
  modelProvider: 'openai',
  modelName: 'gpt-5.5',
  apiKey: '',
  baseUrl: '',
  apiStyle: 'chat_completions',
  targetRepo: DEFAULT_TARGET_REPO,
  templateId: '',
  enableWebInject: false,
  enableObservability: true,
};

const PodLogo: React.FC<{ className?: string }> = ({ className }) => (
  <svg viewBox="0 0 24 24" fill="none" className={className}>
    <path
      d="M5 20C3.5 14 4.5 8 8 4.5c2-1.5 5-1.5 7.5 0C18.5 7.5 19 12 18 17c-.3 1.2-1.5 2-3 2H8c-1.5 0-2.7-.8-3-2"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
    <circle cx="8.5" cy="11" r="1.8" stroke="currentColor" strokeWidth="1.4" />
    <circle cx="12" cy="9" r="2" stroke="currentColor" strokeWidth="1.4" />
    <circle cx="15.5" cy="11" r="2.2" stroke="currentColor" strokeWidth="1.4" />
    <path
      d="M15.5 4.5c.5-1.2 1-2.5 1.5-3"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
    />
    <path
      d="M17 1.5c1.2.8 2.2 2 2.5 3"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
    />
  </svg>
);

const RunCreatePage: React.FC = () => {
  const navigate = useNavigate();
  const [config, setConfig] = useState<RunConfig>(DEFAULT_CONFIG);
  const [templates] = useState<PipelineTemplateItem[]>([
    {
      id: 'phase_two_delivery_flow',
      name: 'Phase Two Delivery Flow',
      description: 'Go backend built-in end-to-end delivery pipeline',
      isDefault: true,
    },
  ]);
  const [registrationStates] = useState<RegistrationStateItem[]>([
    {
      id: 'go-runtime',
      type: 'handler',
      name: 'Go Runtime API',
      status: 'registered',
      errorMessage: null,
    },
    {
      id: 'doujiagit',
      type: 'op',
      name: 'DoujiaGit Committer',
      status: 'registered',
      errorMessage: null,
    },
    {
      id: 'phase-two',
      type: 'pipeline',
      name: 'phase_two_delivery_flow',
      status: 'registered',
      errorMessage: null,
    },
  ]);
  const [demoRuns, setDemoRuns] = useState<DemoRunShortcut[]>([]);
  const [showLaunchDialog, setShowLaunchDialog] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [launchError, setLaunchError] = useState<string | null>(null);
  const [projects, setProjects] = useState<DevflowProjectView[]>([]);

  useEffect(() => {
    getDevflowDemoRuns()
      .then((demoRes) => {
        setDemoRuns(
          demoRes.items.map((run) => ({
            id: run.run_id,
            name: run.run_id,
            description: `${run.pipeline_id} / ${run.status}`,
          })),
        );
        setConfig((prev) => ({
          ...prev,
          templateId: prev.templateId || 'phase_two_delivery_flow',
        }));
      })
      .catch((err: unknown) => {
        logger.error('Failed to load DevFlow demo runs:', err);
      });

    getDevflowProjects()
      .then((projectRes) => {
        setProjects(projectRes.items);
        if (projectRes.items.length > 0) {
          const project = projectRes.items[0];
          setConfig((prev) => ({
            ...prev,
            projectId: prev.projectId || project.project_id,
            projectName: prev.projectName || project.name,
            modelProvider: prev.modelProvider || project.llm.provider || 'openai',
            modelName: prev.modelName || project.llm.model || 'gpt-5.5',
            apiKey: prev.apiKey || project.llm.api_key || '',
            baseUrl: prev.baseUrl || project.llm.base_url || '',
            apiStyle:
              prev.apiStyle ||
              (project.llm.api_style as RunConfig['apiStyle']) ||
              'chat_completions',
          }));
        }
      })
      .catch((err: unknown) => {
        logger.error('Failed to load projects:', err);
      });
  }, []);

  const handleProjectSelect = (projectId: string) => {
    const project = projects.find((item) => item.project_id === projectId);
    if (!project) {
      setConfig((prev) => ({ ...prev, projectId }));
      return;
    }
    setConfig((prev) => ({
      ...prev,
      projectId: project.project_id,
      projectName: project.name,
      modelProvider: project.llm.provider || prev.modelProvider || 'openai',
      modelName: project.llm.model || prev.modelName || 'gpt-5.5',
      apiKey: project.llm.api_key || prev.apiKey,
      baseUrl: project.llm.base_url || prev.baseUrl,
      apiStyle:
        (project.llm.api_style as RunConfig['apiStyle']) || prev.apiStyle,
    }));
  };

  const handleValidateAndLaunch = () => {
    setLaunchError(null);
    setShowLaunchDialog(true);
  };

  const handleConfirmLaunch = async () => {
    setIsCreating(true);
    setLaunchError(null);

    try {
      let projectId = config.projectId;
      if (!projectId) {
        const createdProject = await createDevflowProject({
          name: config.projectName.trim() || 'Doujia Project',
          llm: {
            provider: config.modelProvider,
            model: config.modelName,
            api_key: config.apiKey,
            base_url: config.baseUrl,
            api_style: config.apiStyle,
          },
        });
        projectId = createdProject.project_id;
        setProjects((current) => [createdProject, ...current]);
        setConfig((prev) => ({
          ...prev,
          projectId: createdProject.project_id,
          projectName: createdProject.name,
        }));
      }

      const result = await createDevflowRun({
        project_id: projectId,
        pipeline_id: config.templateId || 'phase_two_delivery_flow',
        target_repo: config.targetRepo,
        main_branch: 'main',
        model_provider: config.modelProvider,
        model_name: config.modelName,
        api_key: config.apiKey,
        base_url: config.baseUrl,
        api_style: config.apiStyle,
        start_immediately: true,
      });

      setShowLaunchDialog(false);
      navigate(`/run/${result.run_id}/workspace`);
    } catch (err: unknown) {
      logger.error('Run creation failed:', err);
      setLaunchError(err instanceof Error ? err.message : 'Failed to create run.');
    } finally {
      setIsCreating(false);
    }
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-auto px-4 py-6">
        <div className="mx-auto max-w-3xl">
          <div className="flex min-h-[60vh] flex-col items-center justify-center text-center">
            <div className="mb-6 flex size-16 items-center justify-center rounded-2xl bg-accent">
              <PodLogo className="size-8 text-accent-foreground" />
            </div>
            <h1 className="mb-3 text-4xl font-extrabold tracking-tight text-foreground">
              Doujia
            </h1>
            <p className="mb-8 max-w-md text-base text-muted-foreground">
              Create the real run first, then continue requirement convergence with Doujia inside the workspace.
            </p>
            <div className="mb-8 flex justify-center">
              <Button size="lg" onClick={handleValidateAndLaunch}>
                <CheckCircle2 className="size-4" />
                Create Run
              </Button>
            </div>

            {demoRuns.length > 0 && (
              <div className="w-full max-w-lg">
                <h3 className="mb-3 text-left text-sm font-medium text-muted-foreground">
                  Recent runs
                </h3>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {demoRuns.slice(0, 4).map((demo) => (
                    <div
                      key={demo.id}
                      className="flex flex-col items-start gap-1 rounded-md border bg-card p-3 text-left"
                    >
                      <span className="w-full truncate text-sm font-semibold text-foreground">
                        {demo.name}
                      </span>
                      <span className="line-clamp-2 text-xs text-muted-foreground">
                        {demo.description}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      <ConfigPanel
        config={config}
        onConfigChange={setConfig}
        onProjectSelect={handleProjectSelect}
        projects={projects}
        templates={templates}
        registrationStates={registrationStates}
      />

      <div className="shrink-0 border-t border-border bg-card p-4">
        <div className="mx-auto flex max-w-3xl items-center justify-between gap-3">
          <p className="text-xs text-muted-foreground">
            Requirement chat and confirmation now happen in the workspace after the run is created.
          </p>
          <Button variant="outline" size="sm" onClick={handleValidateAndLaunch}>
            <CheckCircle2 className="size-4" />
            Create Run
          </Button>
        </div>
      </div>

      <LaunchDialog
        open={showLaunchDialog}
        onOpenChange={setShowLaunchDialog}
        config={config}
        templates={templates}
        aiSummary=""
        isCreating={isCreating}
        error={launchError}
        onConfirm={handleConfirmLaunch}
      />
    </div>
  );
};

export default RunCreatePage;
