import React from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { ErrorBoundary } from 'react-error-boundary';

import RoutesComponent from './app.tsx';
import './index.css';
import { createPortal } from 'react-dom';
import { Toaster } from '@client/src/components/ui/sonner';

const CLIENT_BASE_PATH = import.meta.env.VITE_CLIENT_BASE_PATH || '/client';

const ErrorFallback = ({
  error,
  resetErrorBoundary,
}: {
  error: Error;
  resetErrorBoundary: () => void;
}) => (
  <div className="flex min-h-screen items-center justify-center bg-background px-6 text-foreground">
    <div className="max-w-md rounded-lg border border-border bg-card p-6 shadow-sm">
      <h1 className="text-lg font-semibold">应用加载失败</h1>
      <p className="mt-2 text-sm text-muted-foreground">{error.message}</p>
      <button
        type="button"
        className="mt-4 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground"
        onClick={resetErrorBoundary}
      >
        重试
      </button>
    </div>
  </div>
);

const MainApp = () => {
  return (
    <BrowserRouter basename={CLIENT_BASE_PATH}>
      <ErrorBoundary
        fallbackRender={({ error, resetErrorBoundary }) => (
          <ErrorFallback
            error={error as Error}
            resetErrorBoundary={resetErrorBoundary}
          />
        )}
      >
        <RoutesComponent />
        {createPortal(<Toaster />, document.body)}
      </ErrorBoundary>
    </BrowserRouter>
  );
};

createRoot(document.getElementById('root')!).render(<MainApp />);
