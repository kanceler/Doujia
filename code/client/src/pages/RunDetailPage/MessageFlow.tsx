import type { MessageItem, MessageType } from '@shared/api.interface';
import { Card, CardContent, CardHeader, CardTitle } from '@client/src/components/ui/card';
import { Badge } from '@client/src/components/ui/badge';
import { ScrollArea } from '@client/src/components/ui/scroll-area';
import { Streamdown } from '@client/src/components/ui/streamdown';
import {
  User,
  Bot,
  Settings,
  CheckCircle2,
  AlertTriangle,
  FileDiff,
  MessageSquare,
} from 'lucide-react';

const ROLE_ICON: Record<string, React.ReactNode> = {
  user: <User className="size-4" />,
  assistant: <Bot className="size-4" />,
  system: <Settings className="size-4" />,
};

const ROLE_BG: Record<string, string> = {
  user: 'bg-primary/10 text-primary',
  assistant: 'bg-accent text-accent-foreground',
  system: 'bg-muted text-muted-foreground',
};

const TYPE_CONFIG: Record<MessageType, { label: string; icon: React.ReactNode }> = {
  text: { label: 'Text', icon: <MessageSquare className="size-3" /> },
  approval: { label: 'Approval', icon: <CheckCircle2 className="size-3" /> },
  diff: { label: 'Diff', icon: <FileDiff className="size-3" /> },
  'test-result': { label: 'Test Result', icon: <AlertTriangle className="size-3" /> },
  'mr-summary': { label: 'MR Summary', icon: <FileDiff className="size-3" /> },
  'requirement-summary': { label: 'Requirement Summary', icon: <MessageSquare className="size-3" /> },
  'project-generated': { label: 'Project Generated', icon: <CheckCircle2 className="size-3" /> },
};

interface MessageFlowProps {
  messages: MessageItem[];
}

const MessageFlow: React.FC<MessageFlowProps> = ({ messages }) => {
  if (messages.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Message Flow</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="py-8 text-center text-sm text-muted-foreground">
            No messages yet
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">Message Flow</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <ScrollArea className="h-[500px]">
          <div className="space-y-4 p-6">
            {messages.map((msg) => (
              <div key={msg.id} className="flex gap-3">
                <div className={`shrink-0 rounded-full p-2 ${ROLE_BG[msg.role] || ROLE_BG.system}`}>
                  {ROLE_ICON[msg.role] || ROLE_ICON.system}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="mb-1 flex items-center gap-2">
                    <span className="text-sm font-medium capitalize">
                      {msg.role === 'assistant' ? 'AI' : msg.role}
                    </span>
                    {msg.type !== 'text' && (
                      <Badge variant="outline" className="text-xs">
                        {TYPE_CONFIG[msg.type]?.icon}
                        {TYPE_CONFIG[msg.type]?.label}
                      </Badge>
                    )}
                    <span className="ml-auto text-xs text-muted-foreground">
                      {new Date(msg.createdAt).toLocaleTimeString('zh-CN')}
                    </span>
                  </div>
                  <div className="text-sm leading-relaxed">
                    {msg.type === 'text' ||
                    msg.type === 'mr-summary' ||
                    msg.type === 'requirement-summary' ||
                    msg.type === 'project-generated' ? (
                      <Streamdown>{msg.content}</Streamdown>
                    ) : msg.type === 'diff' ? (
                      <pre className="overflow-x-auto whitespace-pre-wrap rounded-lg bg-muted p-3 text-xs font-mono">
                        {msg.content}
                      </pre>
                    ) : msg.type === 'test-result' ? (
                      <div className="rounded-lg bg-muted p-3">
                        <pre className="whitespace-pre-wrap text-xs font-mono">{msg.content}</pre>
                        {msg.metadata?.testResults && msg.metadata.testResults.length > 0 && (
                          <div className="mt-2 space-y-1">
                            {msg.metadata.testResults.map(
                              (test: { name: string; passed: boolean }, idx: number) => (
                                <div key={idx} className="flex items-center gap-2 text-xs">
                                  {test.passed ? (
                                    <CheckCircle2 className="size-3 text-status-success" />
                                  ) : (
                                    <AlertTriangle className="size-3 text-status-failed" />
                                  )}
                                  <span>{test.name}</span>
                                </div>
                              ),
                            )}
                          </div>
                        )}
                      </div>
                    ) : msg.type === 'approval' ? (
                      <div className="rounded-lg bg-muted p-3">
                        <p className="text-xs">{msg.content}</p>
                        {msg.metadata?.approvalStatus && (
                          <Badge variant="outline" className="mt-2 text-xs">
                            Status: {msg.metadata.approvalStatus}
                          </Badge>
                        )}
                      </div>
                    ) : (
                      <p className="text-sm">{msg.content}</p>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  );
};

export default MessageFlow;
