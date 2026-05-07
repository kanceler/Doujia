import { Link } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { Button } from '@client/src/components/ui/button';
import { Card, CardContent } from '@client/src/components/ui/card';
import { Badge } from '@client/src/components/ui/badge';
import { getDevflowDemoRuns } from '@/api/devflow-client';
import type { DemoRunItem } from '@shared/api.interface';
import type { DevflowRunView, GoRunStatus } from '@shared/devflow-api';
import {
  ArrowRight,
  Zap,
  GitBranch,
  Shield,
  Eye,
  FileCode,
  BrainCircuit,
  CheckCircle2,
  Play,
  MousePointerClick,
  Activity,
  ChevronRight,
  LayoutTemplate,
  Sprout,
  Bean,
} from 'lucide-react';

const HERO_BG = 'https://miaoda.feishu.cn/aily/api/v1/files/static/53d42c1347fa4fa498fa393ef98e27c0_ve_miaoda';

const PodLogo: React.FC<{ className?: string }> = ({ className }) => (
  <svg viewBox="0 0 24 24" fill="none" className={className}>
    <path d="M5 20C3.5 14 4.5 8 8 4.5c2-1.5 5-1.5 7.5 0C18.5 7.5 19 12 18 17c-.3 1.2-1.5 2-3 2H8c-1.5 0-2.7-.8-3-2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    <circle cx="8.5" cy="11" r="1.8" stroke="currentColor" strokeWidth="1.4" />
    <circle cx="12" cy="9" r="2" stroke="currentColor" strokeWidth="1.4" />
    <circle cx="15.5" cy="11" r="2.2" stroke="currentColor" strokeWidth="1.4" />
    <path d="M15.5 4.5c.5-1.2 1-2.5 1.5-3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    <path d="M17 1.5c1.2.8 2.2 2 2.5 3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
  </svg>
);

const CAPABILITY_ITEMS = [
  {
    icon: BrainCircuit,
    title: '需求分析',
    desc: 'AI 驱动的需求理解与澄清，自动识别模糊点并提出关键问题',
    img: 'https://miaoda.feishu.cn/aily/api/v1/files/static/2e54b0e4470e45328d0741937cafc519_ve_miaoda',
  },
  {
    icon: FileCode,
    title: '架构设计',
    desc: '自动生成架构方案，支持 Human-in-the-Loop 审批与调整',
    img: 'https://miaoda.feishu.cn/aily/api/v1/files/static/552530789f2c4ce882411f08c9b00822_ve_miaoda',
  },
  {
    icon: Zap,
    title: '代码生成',
    desc: '基于 Pipeline 的多 Agent 协作，自动生成高质量代码',
    img: 'https://miaoda.feishu.cn/aily/api/v1/files/static/a5fd4653b7fc42c68ae24e6223094963_ve_miaoda',
  },
  {
    icon: Shield,
    title: '测试验证',
    desc: '自动化测试执行与结果验证，确保代码质量达标',
    img: 'https://miaoda.feishu.cn/aily/api/v1/files/static/4ff76cbc3a804af795011befd660d97c_ve_miaoda',
  },
  {
    icon: Bean,
    title: '版本管控',
    desc: 'DoujiaGit 版本化机制，每次变更可追踪、可回溯、可恢复',
    img: 'https://miaoda.feishu.cn/aily/api/v1/files/static/b75e4754d2984eea93ca98b96b49f5aa_ve_miaoda',
  },
  {
    icon: MousePointerClick,
    title: '网页注入',
    desc: '所见即所得的页面修改，圈选元素即可热更新',
    img: 'https://miaoda.feishu.cn/aily/api/v1/files/static/d4986ad939bd4b05b28be3c2fcbfa7cf_ve_miaoda',
  },
];

const HIGHLIGHTS = [
  {
    icon: Bean,
    title: 'DoujiaGit 版本化',
    desc: '每个 Pipeline 节点产生独立 Ref，支持版本回溯、分支比较和精确恢复。告别黑盒执行，让 AI 研发过程完全可控。',
    tags: ['Ref 追踪', '版本回溯', '分支对比'],
  },
  {
    icon: FileCode,
    title: 'Pipeline JSON DSL',
    desc: '流程不是硬编码实现，而是通过声明式 JSON DSL 定义。支持动态编排、模板切换和运行时装配，灵活适配不同研发场景。',
    tags: ['声明式定义', '动态编排', '模板切换'],
  },
];

const PIPELINE_STEPS = [
  { name: '需求分析', status: 'success' as const },
  { name: '架构审批', status: 'success' as const },
  { name: '代码生成', status: 'running' as const },
  { name: '代码审查', status: 'pending' as const },
  { name: '测试验证', status: 'pending' as const },
  { name: '交付部署', status: 'pending' as const },
];

const STATUS_COLORS: Record<string, string> = {
  success: 'bg-[hsl(142_72%_35%)]',
  running: 'bg-[hsl(212_92%_48%)] animate-pulse',
  pending: 'bg-[hsl(38_92%_50%)]',
  failed: 'bg-[hsl(0_84%_60%)]',
  rejected: 'bg-[hsl(0_74%_50%)]',
};

const STATUS_BORDER: Record<string, string> = {
  success: 'border-[hsl(142_72%_35%)]',
  running: 'border-[hsl(212_92%_48%)]',
  pending: 'border-[hsl(38_92%_50%)]',
  failed: 'border-[hsl(0_84%_60%)]',
  rejected: 'border-[hsl(0_74%_50%)]',
};

const RUN_STATUS_MAP: Record<GoRunStatus, DemoRunItem['status']> = {
  created: 'pending',
  running: 'running',
  awaiting_acceptance: 'rejected',
  completed: 'success',
  failed: 'failed',
};

function toDemoRunItem(run: DevflowRunView): DemoRunItem {
  return {
    id: run.run_id,
    name: run.run_id,
    description: run.pipeline_id,
    status: RUN_STATUS_MAP[run.status] ?? 'pending',
    createdAt: run.created_at,
  };
}

const HeroSection = () => (
  <section className="relative overflow-hidden">
    <div
      className="absolute inset-0 bg-cover bg-center opacity-15"
      style={{ backgroundImage: `url(${HERO_BG})` }}
    />
    <div className="absolute inset-0 bg-gradient-to-b from-background/40 via-background/80 to-background" />
    <div className="relative max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-24 sm:py-32">
      <div className="max-w-3xl">
        <Badge variant="secondary" className="mb-4 text-xs font-medium">
          AI 驱动 · Doujia 出品
        </Badge>
        <h1 className="text-4xl font-extrabold tracking-tight text-foreground sm:text-5xl lg:text-6xl">
          让 AI 完成研发流程
          <span className="block text-primary mt-2">Doujia，从需求到交付一站自动化</span>
        </h1>
        <p className="mt-6 text-lg text-muted-foreground leading-7 max-w-2xl">
          Doujia（豆荚）是一个基于 Pipeline 编排的 AI 研发自动化平台，支持需求澄清、代码生成、测试验证的全流程自动化，
          内置 DoujiaGit 版本管控和 Human-in-the-Loop 审批，让 AI 研发过程透明可控。
        </p>
        <div className="mt-8 flex flex-wrap gap-3">
          <Button size="lg" asChild>
            <Link to="/run/create">
              立即体验 <ArrowRight className="ml-2 size-4" />
            </Link>
          </Button>
          <Button size="lg" variant="outline" asChild>
            <Link to="/templates">
              查看 Pipeline DSL <ChevronRight className="ml-1 size-4" />
            </Link>
          </Button>
        </div>
      </div>
    </div>
  </section>
);

const CapabilitySection = () => (
  <section className="py-20 bg-card">
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div className="text-center mb-12">
        <h2 className="text-2xl font-bold tracking-tight text-foreground">全研发链路能力</h2>
        <p className="mt-3 text-muted-foreground text-base">从需求分析到网页注入，6 大核心模块覆盖 AI 研发全流程</p>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6" data-ai-section-type="card-list">
        {CAPABILITY_ITEMS.map((item) => (
          <Card key={item.title} className="group border border-border hover:shadow-sm transition-shadow duration-150">
            <CardContent className="p-6">
              <div className="flex items-start gap-4">
                <div className="shrink-0 size-10 rounded-md bg-accent flex items-center justify-center">
                  <item.icon className="size-5 text-accent-foreground" />
                </div>
                <div className="min-w-0">
                  <h3 className="text-lg font-semibold text-foreground">{item.title}</h3>
                  <p className="mt-1 text-sm text-muted-foreground leading-6">{item.desc}</p>
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  </section>
);

const HighlightSection = () => (
  <section className="py-20">
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div className="text-center mb-12">
        <h2 className="text-2xl font-bold tracking-tight text-foreground">核心亮点</h2>
        <p className="mt-3 text-muted-foreground text-base">两大技术创新，让 AI 研发过程从黑盒变为白盒</p>
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {HIGHLIGHTS.map((item) => (
          <Card key={item.title} className="border border-border overflow-hidden">
            <CardContent className="p-8">
              <div className="flex items-center gap-3 mb-4">
                <div className="size-10 rounded-md bg-primary/10 flex items-center justify-center">
                  <item.icon className="size-5 text-primary" />
                </div>
                <h3 className="text-lg font-semibold text-foreground">{item.title}</h3>
              </div>
              <p className="text-sm text-muted-foreground leading-6 mb-4">{item.desc}</p>
              <div className="flex flex-wrap gap-2">
                {item.tags.map((tag) => (
                  <Badge key={tag} variant="secondary" className="text-xs">{tag}</Badge>
                ))}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  </section>
);

const PipelineVisualization = () => (
  <section className="py-20 bg-card">
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div className="text-center mb-12">
        <h2 className="text-2xl font-bold tracking-tight text-foreground">运行可视化</h2>
        <p className="mt-3 text-muted-foreground text-base">Pipeline 执行过程实时可见，Human-in-the-Loop 审批节点保障质量</p>
      </div>
      <div className="flex flex-col items-center gap-6">
        <div className="flex items-center gap-2 overflow-x-auto pb-4 w-full justify-center">
          {PIPELINE_STEPS.map((step, idx) => (
            <div key={step.name} className="flex items-center shrink-0">
              <div className="flex flex-col items-center gap-2">
                <div
                  className={`size-12 rounded-full border-2 flex items-center justify-center ${
                    STATUS_BORDER[step.status]
                  } ${step.status === 'running' ? 'ring-2 ring-[hsl(212_92%_48%)]/20' : ''}`}
                >
                  <div className={`size-5 rounded-full ${STATUS_COLORS[step.status]}`} />
                </div>
                <span className="text-xs font-medium text-foreground whitespace-nowrap">{step.name}</span>
                {step.status === 'running' && (
                  <span className="text-[10px] text-primary font-medium">当前</span>
                )}
              </div>
              {idx < PIPELINE_STEPS.length - 1 && (
                <div className="w-8 sm:w-12 h-0.5 bg-border mx-1" />
              )}
            </div>
          ))}
        </div>
        <div className="flex items-center gap-6 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-[hsl(142_72%_35%)]" /> 成功</span>
          <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-[hsl(212_92%_48%)]" /> 执行中</span>
          <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-[hsl(38_92%_50%)]" /> 等待</span>
          <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-[hsl(0_84%_60%)]" /> 失败</span>
        </div>
      </div>
    </div>
  </section>
);

const WebInjectSection = () => (
  <section className="py-20">
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-12 items-center">
        <div>
          <Badge variant="secondary" className="mb-4 text-xs">高光功能</Badge>
          <h2 className="text-2xl font-bold tracking-tight text-foreground">网页注入模式</h2>
          <p className="mt-4 text-muted-foreground text-base leading-7">
            所见即所得的页面修改能力。圈选页面元素，输入修改指令，实时预览热更新效果。
            修改完成后自动生成 MR 摘要，一键提交合并请求。
          </p>
          <ul className="mt-6 space-y-3">
            {[
              '元素圈选高亮，悬浮显示上下文信息',
              '自然语言修改指令，AI 自动生成代码变更',
              '实时预览热更新效果，确认后提交',
              '自动生成 MR 摘要，与主工作台共享上下文',
            ].map((item) => (
              <li key={item} className="flex items-start gap-2 text-sm text-muted-foreground">
                <CheckCircle2 className="size-4 text-[hsl(142_72%_35%)] shrink-0 mt-0.5" />
                <span>{item}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="relative rounded-lg border border-border bg-card p-6 shadow-sm">
          <div className="space-y-4">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Activity className="size-3.5" /> 预览模式
            </div>
            <div className="rounded-md border border-border bg-background p-4 space-y-2">
              <div className="h-4 w-24 bg-accent rounded" />
              <div className="h-3 w-full bg-muted rounded" />
              <div className="h-3 w-3/4 bg-muted rounded" />
              <div className="mt-3 h-8 w-32 bg-primary/20 border-2 border-dashed border-primary rounded flex items-center justify-center">
                <Eye className="size-3.5 text-primary" />
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
);

const ActionSection = ({ demoRuns }: { demoRuns: DemoRunItem[] }) => (
  <section className="py-20 bg-card">
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div className="text-center mb-12">
        <h2 className="text-2xl font-bold tracking-tight text-foreground">开始使用</h2>
        <p className="mt-3 text-muted-foreground text-base">三种方式快速进入 Doujia</p>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-6" data-ai-section-type="card-menu">
        <Card className="border border-border hover:shadow-sm transition-shadow group">
          <CardContent className="p-6 text-center">
            <div className="mx-auto size-12 rounded-full bg-primary/10 flex items-center justify-center mb-4">
              <PodLogo className="size-5 text-primary" />
            </div>
            <h3 className="text-lg font-semibold text-foreground">发起 Run</h3>
            <p className="mt-2 text-sm text-muted-foreground">输入需求，启动一次完整的 AI 研发流程</p>
            <Button className="mt-4 w-full" asChild>
              <Link to="/run/create">
                开始 <ArrowRight className="ml-1 size-3.5" />
              </Link>
            </Button>
          </CardContent>
        </Card>

        <Card className="border border-border hover:shadow-sm transition-shadow group">
          <CardContent className="p-6 text-center">
            <div className="mx-auto size-12 rounded-full bg-accent flex items-center justify-center mb-4">
              <LayoutTemplate className="size-6 text-accent-foreground" />
            </div>
            <h3 className="text-lg font-semibold text-foreground">查看模板</h3>
            <p className="mt-2 text-sm text-muted-foreground">浏览 Pipeline 模板，了解流程编排方式</p>
            <Button variant="outline" className="mt-4 w-full" asChild>
              <Link to="/templates">浏览模板</Link>
            </Button>
          </CardContent>
        </Card>

        <Card className="border border-border hover:shadow-sm transition-shadow group">
          <CardContent className="p-6 text-center">
            <div className="mx-auto size-12 rounded-full bg-accent flex items-center justify-center mb-4">
              <Play className="size-6 text-accent-foreground" />
            </div>
            <h3 className="text-lg font-semibold text-foreground">Demo Run</h3>
            <p className="mt-2 text-sm text-muted-foreground">查看完整的演示案例，体验全流程</p>
            {demoRuns.length > 0 ? (
              <Button variant="outline" className="mt-4 w-full" asChild>
                <Link to={`/run/${demoRuns[0].id}/detail`}>查看 Demo</Link>
              </Button>
            ) : (
              <Button variant="outline" className="mt-4 w-full" disabled>暂无 Demo</Button>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  </section>
);

const HomePage = () => {
  const [demoRuns, setDemoRuns] = useState<DemoRunItem[]>([]);

  useEffect(() => {
    const fetchDemoRuns = async () => {
      try {
        const result = await getDevflowDemoRuns();
        setDemoRuns(result.items.map(toDemoRunItem));
      } catch {
        setDemoRuns([]);
      }
    };
    fetchDemoRuns();
  }, []);

  return (
    <div className="bg-background">
      <HeroSection />
      <CapabilitySection />
      <HighlightSection />
      <PipelineVisualization />
      <WebInjectSection />
      <ActionSection demoRuns={demoRuns} />
    </div>
  );
};

export default HomePage;
