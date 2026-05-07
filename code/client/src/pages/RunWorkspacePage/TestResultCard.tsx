import type { MessageItem } from '@shared/api.interface';
import { CheckCircle, XCircle } from 'lucide-react';

interface TestResultCardProps {
  message: MessageItem;
}

export const TestResultCard: React.FC<TestResultCardProps> = ({ message }) => {
  const testResults: Array<{ name: string; passed: boolean }> = message.metadata?.testResults || [];
  const passedCount = testResults.filter((test) => test.passed).length;
  const totalCount = testResults.length;
  const allPassed = totalCount > 0 && passedCount === totalCount;

  return (
    <div className="space-y-3 rounded-[22px] border border-slate-200/80 bg-slate-50/80 p-4">
      <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
        {allPassed ? (
          <CheckCircle className="size-4 text-green-600" />
        ) : (
          <XCircle className="size-4 text-red-500" />
        )}
        <span>
          测试结果: {passedCount}/{totalCount} 通过
        </span>
      </div>

      {testResults.length > 0 && (
        <div className="space-y-2">
          {testResults.map((test, idx) => (
            <div
              key={idx}
              className="flex items-center gap-2 rounded-[16px] bg-white/80 px-3 py-2 text-xs text-slate-600"
            >
              {test.passed ? (
                <CheckCircle className="size-3 shrink-0 text-green-600" />
              ) : (
                <XCircle className="size-3 shrink-0 text-red-500" />
              )}
              <span className="truncate font-mono">{test.name}</span>
            </div>
          ))}
        </div>
      )}

      {testResults.length === 0 && (
        <p className="text-sm leading-6 text-slate-500">{message.content}</p>
      )}
    </div>
  );
};
