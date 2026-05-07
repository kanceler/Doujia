import { useEffect, useState } from 'react';
import { NavLink, Link, Outlet, useLocation } from 'react-router-dom';
import {
  SidebarProvider,
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarTrigger,
} from '@/components/ui/sidebar';
import {
  Clock,
  GitBranch,
  Grid2X2,
  Menu,
  Plug,
  Plus,
  X,
} from 'lucide-react';
import { getDevflowDemoRuns, getDevflowSessionMessages } from '@/api/devflow-client';
import type { DevflowRunView, GoRunStatus } from '@shared/devflow-api';

const NAV_ITEMS = [
  { path: '/run/create', label: '新项目', icon: Plus },
  { path: '/demo', label: '导入 doujia-git', icon: GitBranch },
  { path: '/', label: '我的应用', icon: Grid2X2 },
  { path: '/templates', label: '插件', icon: Plug },
];

interface RecentRunItem {
  run: DevflowRunView;
  title: string;
}

const RUN_STATUS_LABELS: Record<GoRunStatus, string> = {
  created: '待开始',
  running: '运行中',
  awaiting_acceptance: '待确认',
  completed: '已完成',
  failed: '失败',
};

const RUN_STATUS_CLASS_NAMES: Record<GoRunStatus, string> = {
  created: 'bg-slate-100 text-slate-600',
  running: 'bg-sky-100 text-sky-700',
  awaiting_acceptance: 'bg-amber-100 text-amber-700',
  completed: 'bg-emerald-100 text-emerald-700',
  failed: 'bg-red-100 text-red-700',
};

function getRunStatusLabel(status: GoRunStatus): string {
  return RUN_STATUS_LABELS[status] ?? status;
}

function getRunStatusClassName(status: GoRunStatus): string {
  return RUN_STATUS_CLASS_NAMES[status] ?? RUN_STATUS_CLASS_NAMES.created;
}

const PodIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg viewBox="0 0 24 24" fill="none" className={className}>
    <path d="M5 20C3.5 14 4.5 8 8 4.5c2-1.5 5-1.5 7.5 0C18.5 7.5 19 12 18 17c-.3 1.2-1.5 2-3 2H8c-1.5 0-2.7-.8-3-2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    <circle cx="8.5" cy="11" r="1.8" stroke="currentColor" strokeWidth="1.4" />
    <circle cx="12" cy="9" r="2" stroke="currentColor" strokeWidth="1.4" />
    <circle cx="15.5" cy="11" r="2.2" stroke="currentColor" strokeWidth="1.4" />
    <path d="M15.5 4.5c.5-1.2 1-2.5 1.5-3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    <path d="M17 1.5c1.2.8 2.2 2 2.5 3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
  </svg>
);

const TopNav = () => {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  return (
    <>
      <header className="fixed top-0 left-0 right-0 z-50 border-b border-border bg-card">
        <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <div className="flex h-14 items-center justify-between">
            <Link to="/" className="flex shrink-0 items-center gap-2">
              <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <PodIcon className="size-5" />
              </div>
              <span className="text-sm font-semibold text-foreground">Doujia（豆荚）</span>
            </Link>

            <nav className="hidden items-center gap-1 md:flex">
              {NAV_ITEMS.map((item) => (
                <NavLink
                  key={item.path}
                  to={item.path}
                  className={({ isActive }) =>
                    `rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                      isActive
                        ? 'bg-accent text-primary'
                        : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                    }`
                  }
                >
                  {item.label}
                </NavLink>
              ))}
            </nav>

            <div className="hidden items-center md:flex">
              <UserAvatar />
            </div>

            <button
              className="p-2 text-foreground md:hidden"
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              aria-label="Toggle menu"
            >
              {mobileMenuOpen ? <X className="size-5" /> : <Menu className="size-5" />}
            </button>
          </div>
        </div>
      </header>

      {mobileMenuOpen && (
        <div className="fixed inset-0 z-40 bg-black/50 md:hidden" onClick={() => setMobileMenuOpen(false)}>
          <div
            className="absolute right-0 top-14 w-64 border-b border-border bg-card p-4 shadow-md"
            onClick={(e) => e.stopPropagation()}
          >
            <nav className="flex flex-col gap-1">
              {NAV_ITEMS.map((item) => (
                <NavLink
                  key={item.path}
                  to={item.path}
                  onClick={() => setMobileMenuOpen(false)}
                  className={({ isActive }) =>
                    `flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                      isActive
                        ? 'bg-accent text-primary'
                        : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                    }`
                  }
                >
                  <item.icon className="size-4" />
                  {item.label}
                </NavLink>
              ))}
            </nav>
          </div>
        </div>
      )}

      <main className="h-screen overflow-hidden pt-14">
        <Outlet />
      </main>
    </>
  );
};

const SidebarLayout = () => {
  const { pathname } = useLocation();
  const [recentRuns, setRecentRuns] = useState<RecentRunItem[]>([]);

  useEffect(() => {
    let cancelled = false;

    getDevflowDemoRuns()
      .then(async (result) => {
        const runs = result.items.slice(0, 8);
        const items = await Promise.all(
          runs.map(async (run) => {
            try {
              const messages = await getDevflowSessionMessages(run.run_id);
              const firstUserMessage = messages.items.find((message) => message.role === 'user');
              return {
                run,
                title: firstUserMessage?.content.trim() || run.run_id,
              };
            } catch {
              return { run, title: run.run_id };
            }
          }),
        );
        if (!cancelled) {
          setRecentRuns(items);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setRecentRuns([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="lg" asChild>
                <Link to="/">
                  <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                    <PodIcon className="size-5" />
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight group-data-[collapsible=icon]:hidden">
                    <span className="truncate font-semibold">Doujia（豆荚）</span>
                  </div>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>

        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {NAV_ITEMS.map((item) => (
                  <SidebarMenuItem key={item.path}>
                    <SidebarMenuButton asChild isActive={pathname === item.path}>
                      <Link to={item.path}>
                        <item.icon className="size-4" />
                        <span>{item.label}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>
              <Clock className="mr-2 size-3.5" />
              最近的项目
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {recentRuns.length > 0 ? (
                  recentRuns.map((item) => (
                    <SidebarMenuItem key={item.run.run_id}>
                      <SidebarMenuButton
                        asChild
                        isActive={pathname === `/run/${item.run.run_id}/workspace`}
                        className="h-auto items-start rounded-lg px-2 py-2 group-data-[collapsible=icon]:h-8 group-data-[collapsible=icon]:items-center"
                      >
                        <Link to={`/run/${item.run.run_id}/workspace`} title={item.title}>
                          <span className="flex min-w-0 flex-1 flex-col gap-1 group-data-[collapsible=icon]:hidden">
                            <span className="truncate text-sm font-medium leading-5 text-sidebar-foreground">
                              {item.title}
                            </span>
                            <span className="truncate text-xs font-normal leading-4 text-muted-foreground">
                              {item.run.run_id}
                            </span>
                          </span>
                          <span
                            className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium leading-4 group-data-[collapsible=icon]:hidden ${getRunStatusClassName(item.run.status)}`}
                          >
                            {getRunStatusLabel(item.run.status)}
                          </span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))
                ) : (
                  <SidebarMenuItem>
                    <div className="px-2 py-1.5 text-xs text-muted-foreground group-data-[collapsible=icon]:hidden">
                      暂无最近项目
                    </div>
                  </SidebarMenuItem>
                )}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>

        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <UserAvatar />
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>

      <main className="flex h-screen flex-1 flex-col overflow-hidden">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
          <SidebarTrigger />
        </header>
        <div className="min-h-0 flex-1 overflow-hidden">
          <Outlet />
        </div>
      </main>
    </SidebarProvider>
  );
};

const UserAvatar = () => (
  <div className="flex items-center gap-2">
    <div className="flex size-7 items-center justify-center rounded-full bg-muted">
      <span className="text-xs text-muted-foreground">D</span>
    </div>
  </div>
);

const Layout = () => {
  const { pathname } = useLocation();
  const isHomePage = pathname === '/';

  if (isHomePage) {
    return <TopNav />;
  }

  return <SidebarLayout />;
};

export default Layout;
