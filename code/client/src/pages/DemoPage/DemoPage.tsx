import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getDevflowDemoRuns } from '@/api/devflow-client';
import { Loader2 } from 'lucide-react';

const DemoRedirect: React.FC = () => {
  const navigate = useNavigate();
  const [error, setError] = useState(false);

  useEffect(() => {
    const redirectToDemo = async () => {
      try {
        const result = await getDevflowDemoRuns();
        if (result.items.length > 0) {
          navigate(`/run/${result.items[0].run_id}/workspace`, { replace: true });
        } else {
          setError(true);
        }
      } catch {
        setError(true);
      }
    };
    redirectToDemo();
  }, [navigate]);

  if (error) {
    return (
      <div className="flex items-center justify-center h-64">
        <p className="text-muted-foreground">暂无 Demo Run</p>
      </div>
    );
  }

  return (
    <div className="flex items-center justify-center h-64">
      <Loader2 className="size-6 animate-spin text-muted-foreground" />
      <span className="ml-2 text-sm text-muted-foreground">跳转中...</span>
    </div>
  );
};

export default DemoRedirect;
