import { useEffect, useRef } from 'react';
import type { MessageItem as MessageItemType } from '@shared/api.interface';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Streamdown } from '@/components/ui/streamdown';
import { cn } from '@/lib/utils';
import { Bot, MessageSquare, Sparkles } from 'lucide-react';
import { ApprovalCard } from './ApprovalCard';
import { DiffCard } from './DiffCard';
import { MrSummaryCard } from './MrSummaryCard';
import { ProjectGeneratedCard } from './ProjectGeneratedCard';
import { RequirementSummaryCard } from './RequirementSummaryCard';
import { TestResultCard } from './TestResultCard';

interface MessageFlowProps {
  messages: MessageItemType[];
  onActionComplete: () => void;
  onProjectGeneratedClick: () => void;
  messagesEndRef: React.RefObject<HTMLDivElement | null>;
}

const roleLabelMap: Record<string, string> = {
  user: '你',
  assistant: 'Doujia',
  system: '系统',
};

const messageTypeLabelMap: Partial<Record<MessageItemType['type'], string>> = {
  approval: '待确认',
  diff: '代码变更',
  'mr-summary': '交付摘要',
  'project-generated': '项目生成',
  'requirement-summary': '需求确认',
  'test-result': '测试结果',
};

export const MessageFlow: React.FC<MessageFlowProps> = ({
  messages,
  onActionComplete,
  onProjectGeneratedClick,
  messagesEndRef,
}) => {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const viewport = scrollRef.current?.querySelector('[data-radix-scroll-area-viewport]');
    if (viewport) {
      viewport.scrollTop = viewport.scrollHeight;
    }
  }, [messages.length]);

  if (messages.length === 0) {
    return (
      <div className="flex h-full items-center justify-center px-6 py-10">
        <div className="w-full max-w-xl rounded-[28px] border border-white/80 bg-white/80 p-8 shadow-[0_24px_80px_rgba(15,23,42,0.08)] backdrop-blur">
          <Empty>
            <EmptyHeader>
              <EmptyMedia
                variant="icon"
                className="bg-[radial-gradient(circle_at_top,#ffffff_0%,rgba(191,219,254,0.75)_45%,rgba(96,165,250,0.18)_100%)] text-primary shadow-[0_10px_30px_rgba(59,130,246,0.18)]"
              >
                <MessageSquare className="size-6" />
              </EmptyMedia>
              <EmptyTitle>等待你和 Doujia 开始对话</EmptyTitle>
              <EmptyDescription>
                这里会保留你真正需要参与的消息、确认和需求补充，让整个协作更像自然的 AI 对话，而不是系统日志。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </div>
      </div>
    );
  }

  return (
    <ScrollArea ref={scrollRef} className="h-full">
      <div className="mx-auto flex w-full max-w-[1100px] flex-col gap-5 px-4 pb-8 pt-6 sm:px-6 sm:pt-8">
        {messages.map((msg: MessageItemType, index) => (
          <MessageItem
            key={`${msg.id}-${index}`}
            message={msg}
            onActionComplete={onActionComplete}
            onProjectGeneratedClick={onProjectGeneratedClick}
          />
        ))}
        <div ref={messagesEndRef} />
      </div>
    </ScrollArea>
  );
};

interface MessageItemProps {
  message: MessageItemType;
  onActionComplete: () => void;
  onProjectGeneratedClick: () => void;
}

const MessageItem: React.FC<MessageItemProps> = ({
  message,
  onActionComplete,
  onProjectGeneratedClick,
}) => {
  const isUser = message.role === 'user';
  const bubbleTypeLabel = message.type !== 'text' ? messageTypeLabelMap[message.type] : null;

  return (
    <div className={cn('flex w-full gap-3 sm:gap-4', isUser ? 'justify-end pl-10 sm:pl-16' : 'pr-4 sm:pr-10')}>
      {!isUser && <RoleAvatar role={message.role} />}

      <div className={cn('flex min-w-0 max-w-full flex-col', isUser ? 'items-end sm:max-w-[42rem]' : 'items-start sm:max-w-[44rem]')}>
        <div
          className={cn(
            'mb-1.5 flex flex-wrap items-center gap-2 px-1 text-[11px] font-medium tracking-[0.02em]',
            isUser ? 'text-slate-400' : 'text-slate-500',
          )}
        >
          <span className={cn(message.role === 'assistant' && 'text-primary')}>
            {roleLabelMap[message.role] || message.role}
          </span>
          {bubbleTypeLabel && (
            <span className="inline-flex items-center rounded-full border border-slate-200/80 bg-white/85 px-2 py-0.5 text-[10px] text-slate-500 shadow-[0_4px_14px_rgba(15,23,42,0.04)]">
              {bubbleTypeLabel}
            </span>
          )}
        </div>

        <div
          className={cn(
            'w-fit min-w-[11rem] max-w-full overflow-hidden rounded-[26px] border px-4 py-3.5 sm:min-w-[14rem] sm:px-5 sm:py-4',
            isUser
              ? 'border-primary/15 bg-[linear-gradient(135deg,#4f8cff_0%,#2563eb_100%)] text-white shadow-[0_18px_35px_rgba(37,99,235,0.24)]'
              : message.role === 'assistant'
                ? 'border-white/90 bg-white/92 text-slate-900 shadow-[0_18px_48px_rgba(15,23,42,0.06)]'
                : 'border-slate-200/70 bg-slate-100/90 text-slate-700 shadow-[0_12px_30px_rgba(15,23,42,0.04)]',
          )}
        >
          <MessageContent
            message={message}
            onActionComplete={onActionComplete}
            onProjectGeneratedClick={onProjectGeneratedClick}
            isUser={isUser}
          />
        </div>

        <div className="mt-2 px-2 text-[11px] text-slate-400">
          {formatTime(message.createdAt)}
        </div>
      </div>
    </div>
  );
};

const RoleAvatar: React.FC<{ role: MessageItemType['role'] }> = ({ role }) => {
  if (role === 'assistant') {
    return (
        <Avatar className="mt-1 size-10 ring-1 ring-white/80 shadow-[0_14px_34px_rgba(59,130,246,0.14)]">
        <AvatarFallback className="bg-[radial-gradient(circle_at_top,#ffffff_0%,rgba(224,231,255,0.95)_32%,rgba(191,219,254,0.92)_65%,rgba(96,165,250,0.2)_100%)] text-primary">
          <Sparkles className="size-[18px]" />
        </AvatarFallback>
      </Avatar>
    );
  }

  return (
    <Avatar className="mt-1 size-10 ring-1 ring-white/80 shadow-[0_12px_24px_rgba(15,23,42,0.06)]">
      <AvatarFallback className="bg-slate-200/80 text-slate-500">
        <Bot className="size-4" />
      </AvatarFallback>
    </Avatar>
  );
};

interface MessageContentProps {
  message: MessageItemType;
  onActionComplete: () => void;
  onProjectGeneratedClick: () => void;
  isUser: boolean;
}

const MessageContent: React.FC<MessageContentProps> = ({
  message,
  onActionComplete,
  onProjectGeneratedClick,
  isUser,
}) => {
  switch (message.type) {
    case 'text':
      if (!isUser && message.content.trim().length === 0) {
        return <TypingIndicator />;
      }
      return (
        <div className="text-[15px] leading-7">
          <Streamdown
            className={cn(
              'max-w-none break-words text-[15px] leading-7 prose-p:my-0 prose-p:leading-7 prose-p:[overflow-wrap:anywhere] prose-ul:my-2 prose-ol:my-2 prose-li:my-1 prose-pre:my-3 prose-pre:overflow-x-auto prose-pre:rounded-2xl prose-pre:px-4 prose-pre:py-3 prose-code:rounded prose-code:px-1 prose-code:py-0.5 prose-hr:my-4',
              isUser
                ? 'prose-headings:text-white prose-p:text-white/95 prose-strong:text-white prose-li:text-white/90 prose-a:text-white prose-code:bg-white/15 prose-code:text-white prose-pre:bg-white/10 prose-pre:text-white'
                : 'prose-headings:text-slate-950 prose-p:text-slate-700 prose-strong:text-slate-900 prose-li:text-slate-700 prose-a:text-primary prose-code:bg-slate-100 prose-code:text-slate-700 prose-pre:bg-slate-950 prose-pre:text-slate-100',
            )}
          >
            {message.content}
          </Streamdown>
        </div>
      );
    case 'approval':
      return <ApprovalCard message={message} onActionComplete={onActionComplete} />;
    case 'diff':
      return <DiffCard message={message} />;
    case 'test-result':
      return <TestResultCard message={message} />;
    case 'mr-summary':
      return <MrSummaryCard message={message} />;
    case 'requirement-summary':
      return <RequirementSummaryCard message={message} onActionComplete={onActionComplete} />;
    case 'project-generated':
      return <ProjectGeneratedCard message={message} onOpen={onProjectGeneratedClick} />;
    default:
      return <p className="text-sm leading-6">{message.content}</p>;
  }
};

const TypingIndicator: React.FC = () => (
  <div className="flex items-center gap-3 py-1 text-slate-500">
    <div className="flex items-center gap-1.5">
      {[0, 1, 2].map((dot) => (
        <span
          key={dot}
          className="size-2 rounded-full bg-primary/55 animate-[bounce_1.2s_infinite]"
          style={{ animationDelay: `${dot * 0.15}s` }}
        />
      ))}
    </div>
    <span className="text-sm font-medium">正在思考...</span>
  </div>
);

function formatTime(isoString: string): string {
  const date = new Date(isoString);
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  return `${hours}:${minutes}`;
}
