import type {
  RunConfig,
  PipelineTemplateItem,
  RegistrationStateItem,
} from '@shared/api.interface';
import type { DevflowProjectView } from '@shared/devflow-api';
import { Badge } from '@/components/ui/badge';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import {
  AlertTriangle,
  CheckCircle2,
  Package,
  Rocket,
  Settings,
} from 'lucide-react';

interface ConfigPanelProps {
  config: RunConfig;
  onConfigChange: (config: RunConfig) => void;
  onProjectSelect: (projectId: string) => void;
  projects: DevflowProjectView[];
  templates: PipelineTemplateItem[];
  registrationStates: RegistrationStateItem[];
}

const ConfigPanel: React.FC<ConfigPanelProps> = ({
  config,
  onConfigChange,
  onProjectSelect,
  projects,
  templates,
  registrationStates,
}) => {
  const updateField = <K extends keyof RunConfig>(key: K, value: RunConfig[K]) => {
    onConfigChange({ ...config, [key]: value });
  };

  const configStatus = (): { label: string; ok: boolean } => {
    if (!config.projectName.trim() && !config.projectId) {
      return { label: '请先选择或创建 Project', ok: false };
    }
    if (!config.targetRepo.trim()) {
      return { label: '缺少目标仓库', ok: false };
    }
    if (!config.apiKey.trim()) {
      return { label: '可保存为本地测试配置', ok: true };
    }
    return { label: '配置完成', ok: true };
  };

  const assemblyStatus = (): { label: string; ok: boolean } => {
    const missing = registrationStates.filter(
      (state) => state.status === 'missing' || state.status === 'error',
    );
    if (missing.length === 0) {
      return { label: '全部就绪', ok: true };
    }
    return { label: `${missing.length} 个组件异常`, ok: false };
  };

  const launchStatus = (): { label: string; ok: boolean } => {
    if (config.enableWebInject) {
      return { label: '网页注入已启用', ok: true };
    }
    return { label: '标准模式', ok: true };
  };

  const cs = configStatus();
  const as = assemblyStatus();
  const ls = launchStatus();

  return (
    <div className="shrink-0 border-t border-border bg-card/80 backdrop-blur-sm">
      <div className="mx-auto max-w-3xl px-4">
        <Accordion type="multiple" defaultValue={[]}>
          <AccordionItem value="model-config">
            <AccordionTrigger className="text-sm">
              <div className="flex items-center gap-2">
                <Settings className="size-4 text-muted-foreground" />
                <span>Project 与模型配置</span>
                <Badge
                  variant={cs.ok ? 'outline' : 'destructive'}
                  className="text-xs"
                >
                  {cs.label}
                </Badge>
              </div>
            </AccordionTrigger>
            <AccordionContent>
              <div className="grid grid-cols-2 gap-3 pt-2">
                <div className="col-span-2">
                  <label className="mb-1 block text-xs text-muted-foreground">
                    已有 Project
                  </label>
                  <select
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.projectId}
                    onChange={(e) => onProjectSelect(e.target.value)}
                  >
                    <option value="">创建新 Project</option>
                    {projects.map((project) => (
                      <option
                        key={project.project_id}
                        value={project.project_id}
                      >
                        {project.name}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="col-span-2">
                  <label className="mb-1 block text-xs text-muted-foreground">
                    Project 名称
                  </label>
                  <input
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.projectName}
                    onChange={(e) => updateField('projectName', e.target.value)}
                    placeholder="例如：Doujia Workspace Upgrade"
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    Model Provider
                  </label>
                  <input
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.modelProvider}
                    onChange={(e) => updateField('modelProvider', e.target.value)}
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    Model Name
                  </label>
                  <input
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.modelName}
                    onChange={(e) => updateField('modelName', e.target.value)}
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    API Key
                  </label>
                  <input
                    type="password"
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.apiKey}
                    onChange={(e) => updateField('apiKey', e.target.value)}
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    Base URL
                  </label>
                  <input
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.baseUrl}
                    onChange={(e) => updateField('baseUrl', e.target.value)}
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    API Style
                  </label>
                  <select
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.apiStyle}
                    onChange={(e) =>
                      updateField('apiStyle', e.target.value as RunConfig['apiStyle'])
                    }
                  >
                    <option value="chat_completions">Chat Completions</option>
                    <option value="responses">Responses</option>
                  </select>
                </div>
              </div>
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="run-assembly">
            <AccordionTrigger className="text-sm">
              <div className="flex items-center gap-2">
                <Package className="size-4 text-muted-foreground" />
                <span>运行装配</span>
                <Badge
                  variant={as.ok ? 'outline' : 'destructive'}
                  className="text-xs"
                >
                  {as.label}
                </Badge>
              </div>
            </AccordionTrigger>
            <AccordionContent>
              <div className="space-y-2 pt-2">
                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    Pipeline 模板
                  </label>
                  <select
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.templateId}
                    onChange={(e) => updateField('templateId', e.target.value)}
                  >
                    {templates.map((template) => (
                      <option key={template.id} value={template.id}>
                        {template.name}
                      </option>
                    ))}
                  </select>
                </div>

                <div>
                  <label className="mb-1 block text-xs text-muted-foreground">
                    目标仓库
                  </label>
                  <input
                    className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
                    value={config.targetRepo}
                    onChange={(e) => updateField('targetRepo', e.target.value)}
                    placeholder="local:self 或 https://github.com/org/repo"
                  />
                </div>

                {registrationStates.length > 0 && (
                  <div className="mt-2">
                    <p className="mb-1 text-xs font-medium text-muted-foreground">
                      组件注册状态
                    </p>
                    <div className="flex flex-wrap gap-2">
                      {registrationStates.map((state) => (
                        <Badge
                          key={state.id}
                          variant={
                            state.status === 'registered'
                              ? 'outline'
                              : state.status === 'missing'
                                ? 'destructive'
                                : 'secondary'
                          }
                        >
                          {state.status === 'registered' && (
                            <CheckCircle2 className="mr-1 size-3" />
                          )}
                          {state.status !== 'registered' && (
                            <AlertTriangle className="mr-1 size-3" />
                          )}
                          {state.name}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="launch-options">
            <AccordionTrigger className="text-sm">
              <div className="flex items-center gap-2">
                <Rocket className="size-4 text-muted-foreground" />
                <span>启动选项</span>
                <Badge variant="outline" className="text-xs">
                  {ls.label}
                </Badge>
              </div>
            </AccordionTrigger>
            <AccordionContent>
              <div className="space-y-3 pt-2">
                <label className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    className="rounded border-border"
                    checked={config.enableWebInject}
                    onChange={(e) => updateField('enableWebInject', e.target.checked)}
                  />
                  <span className="text-sm text-foreground">
                    启用网页注入模式
                  </span>
                </label>

                <label className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    className="rounded border-border"
                    checked={config.enableObservability}
                    onChange={(e) =>
                      updateField('enableObservability', e.target.checked)
                    }
                  />
                  <span className="text-sm text-foreground">启用可观测性</span>
                </label>
              </div>
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </div>
    </div>
  );
};

export default ConfigPanel;
