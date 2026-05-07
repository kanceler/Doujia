import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import type {
  RunItem,
  MessageItem,
  PipelineHistoryNode,
  PipelineHistoryEdge,
  RefHistory,
} from '@shared/api.interface';
import { getRunById, cloneRun } from '@client/src/api/run';
import { getMessagesByRunId } from '@client/src/api/message';
import { getPipelineHistory } from '@client/src/api/pipeline-node';
import { exportRun } from '@client/src/api/web-inject';
import { Card, CardContent, CardHeader, CardTitle } from '@client/src/components/ui/card';
import { Button } from '@client/src/components/ui/button';
import { Badge } from '@client/src/components/ui/badge';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@client/src/components/ui/tabs';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from '@client/src/components/ui/dialog';
import { Input } from '@client/src/components/ui/input';
import { ScrollArea } from '@client/src/components/ui/scroll-area';
import { Streamdown } from '@client/src/components/ui/streamdown';
import OverviewCard from './OverviewCard';
import PipelineReview from './PipelineReview';
import MessageFlow from './MessageFlow';
import {
  Copy,
  Download,
  Loader2,
  GitBranch,
  Package,
  Clock,
  ChevronRight,
} from 'lucide-react';
import { logger } from '@lark-apaas/client-toolkit/logger';
import { UniversalLink } from '@lark-apaas/client-toolkit/components/UniversalLink';

const RunDetailPage: React.FC = () => {
  const { runId } = useParams<{ runId: string }>();
  const navigate = useNavigate();

  const [run, setRun] = useState<RunItem | null>(null);
  const [messages, setMessages] = useState<MessageItem[]>([]);
  const [pipelineNodes, setPipelineNodes] = useState<PipelineHistoryNode[]>([]);
  const [pipelineEdges, setPipelineEdges] = useState<PipelineHistoryEdge[]>([]);
  const [refHistory, setRefHistory] = useState<RefHistory[]>([]);
  const [activeTab, setActiveTab] = useState('messages');
  const [loading, setLoading] = useState(true);
  const [cloneOpen, setCloneOpen] = useState(false);
  const [cloneName, setCloneName] = useState('');
  const [cloning, setCloning] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [exportUrl, setExportUrl] = useState<string | null>(null);

  useEffect(() => {
    if (!runId) return;
    const fetchData = async () => {
      setLoading(true);
      try {
        const [runData, msgData, historyData] = await Promise.all([
          getRunById(runId),
          getMessagesByRunId(runId, undefined, 100),
          getPipelineHistory(runId),
        ]);
        setRun(runData);
        setMessages(msgData.items || []);
        setPipelineNodes(historyData.nodes || []);
        setPipelineEdges(historyData.edges || []);
        setRefHistory(historyData.refHistory || []);
        setCloneName(`${runData.name} - Copy`);
      } catch (err) {
        logger.error('Failed to fetch run detail:', String(err));
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, [runId]);

  const handleClone = async () => {
    if (!runId || !cloneName.trim()) return;
    setCloning(true);
    try {
      const result = await cloneRun(runId, { newName: cloneName.trim() });
      setCloneOpen(false);
      navigate(`/run/${result.newRunId}/workspace`);
    } catch (err) {
      logger.error('Failed to clone run:', String(err));
    } finally {
      setCloning(false);
    }
  };

  const handleExport = async () => {
    if (!runId) return;
    setExporting(true);
    try {
      const result = await exportRun(runId);
      setExportUrl(result.downloadUrl);
    } catch (err) {
      logger.error('Failed to export run:', String(err));
    } finally {
      setExporting(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
        <span className="ml-2 text-sm text-muted-foreground">加载中...</span>
      </div>
    );
  }

  if (!run) {
    return (
      <div className="text-center py-16">
        <p className="text-muted-foreground">未找到该 Run</p>
        <Button variant="outline" className="mt-4" onClick={() => navigate('/')}>
          返回首页
        </Button>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6 max-w-6xl mx-auto">
      {/* Overview */}
      <OverviewCard run={run} />

      {/* Pipeline Review */}
      <PipelineReview nodes={pipelineNodes} edges={pipelineEdges} />

      {/* Detail Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
        <TabsList className="w-full sm:w-auto">
          <TabsTrigger value="messages">消息流</TabsTrigger>
          <TabsTrigger value="ref-history">Ref 历史</TabsTrigger>
          <TabsTrigger value="delivery">交付物</TabsTrigger>
        </TabsList>

        <TabsContent value="messages" className="mt-4">
          <MessageFlow messages={messages} />
        </TabsContent>

        <TabsContent value="ref-history" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-lg">Ref 变更历史</CardTitle>
            </CardHeader>
            <CardContent>
              {refHistory.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground text-sm">
                  暂无 Ref 变更记录
                </div>
              ) : (
                <ScrollArea className="h-[400px]">
                  <div className="space-y-0">
                    {refHistory.map((item, idx) => {
                      const nodeName =
                        pipelineNodes.find((n) => n.id === item.nodeId)?.name || item.nodeId;
                      return (
                        <div key={idx} className="flex gap-3">
                          <div className="flex flex-col items-center">
                            <div className="size-8 rounded-full bg-primary/10 flex items-center justify-center shrink-0">
                              <GitBranch className="size-4 text-primary" />
                            </div>
                            {idx < refHistory.length - 1 && (
                              <div className="w-px h-full min-h-[40px] bg-border" />
                            )}
                          </div>
                          <div className="pb-6 flex-1">
                            <div className="flex items-center gap-2">
                              <span className="font-mono text-sm font-medium">
                                {item.ref.slice(0, 7)}
                              </span>
                              <Badge variant="outline" className="text-xs">
                                {nodeName}
                              </Badge>
                            </div>
                            <div className="flex items-center gap-1 mt-1 text-xs text-muted-foreground">
                              <Clock className="size-3" />
                              {new Date(item.timestamp).toLocaleString('zh-CN')}
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </ScrollArea>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="delivery" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-lg">交付结果</CardTitle>
            </CardHeader>
            <CardContent>
              {run.demandSummary ? (
                <div className="prose prose-sm max-w-none">
                  <Streamdown>{run.demandSummary}</Streamdown>
                </div>
              ) : (
                <div className="text-center py-8 text-muted-foreground text-sm">
                  暂无交付物信息
                </div>
              )}
              {run.ref && (
                <div className="mt-4 p-3 bg-muted rounded-lg">
                  <div className="flex items-center gap-2 text-sm">
                    <Package className="size-4 text-muted-foreground" />
                    <span className="font-medium">最终 Ref</span>
                  </div>
                  <code className="block mt-1 font-mono text-xs text-primary">
                    {run.ref}
                  </code>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Action Buttons */}
      <div className="flex items-center gap-3 pt-2">
        <Button variant="outline" onClick={() => setCloneOpen(true)}>
          <Copy className="size-4" />
          从当前 Run 创建新 Run
        </Button>
        <Button variant="outline" onClick={handleExport} disabled={exporting}>
          {exporting ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Download className="size-4" />
          )}
          导出 Run
        </Button>
        {exportUrl && (
          <UniversalLink
            to={exportUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1 text-sm text-primary hover:underline ml-2"
          >
            下载导出文件
            <ChevronRight className="size-3" />
          </UniversalLink>
        )}
      </div>

      {/* Clone Dialog */}
      <Dialog open={cloneOpen} onOpenChange={setCloneOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建新 Run</DialogTitle>
            <DialogDescription>
              基于当前 Run 的配置创建一个新的 Run
            </DialogDescription>
          </DialogHeader>
          <div className="py-4">
            <label className="text-sm font-medium mb-1.5 block">Run 名称</label>
            <Input
              value={cloneName}
              onChange={(e) => setCloneName(e.target.value)}
              placeholder="输入新 Run 名称"
              onKeyDown={(e) => e.key === 'Enter' && handleClone()}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCloneOpen(false)}>
              取消
            </Button>
            <Button onClick={handleClone} disabled={cloning || !cloneName.trim()}>
              {cloning ? (
                <>
                  <Loader2 className="size-4 animate-spin" />
                  创建中...
                </>
              ) : (
                '创建'
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default RunDetailPage;
