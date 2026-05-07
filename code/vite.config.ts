import fs from 'node:fs/promises';
import path from 'node:path';
import react from '@vitejs/plugin-react';
import tsconfigPaths from 'vite-tsconfig-paths';
import { defineConfig, type Plugin } from 'vite';

const devflowApiBaseUrl =
  process.env.VITE_DEVFLOW_API_BASE_URL || 'http://127.0.0.1:18080';

const clientProcessEnv = {
  NODE_ENV: process.env.NODE_ENV || 'development',
  CLIENT_BASE_PATH: '/client',
  FORCE_FRAMEWORK_DOMAIN_MAIN: undefined,
  __RUNTIME_INJECTED__: 'true',
  BUILD_TOOL: 'vite',
  runtimeMode: 'local',
  APP_FLAGS: {},
  __PAGE_ROUTE_DEFINITIONS__: '[]',
  __API_ROUTE_DEFINITIONS__: '[]',
};

function clientSpaFallbackPlugin(): Plugin {
  return {
    name: 'client-spa-fallback',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        if (req.method !== 'GET' && req.method !== 'HEAD') {
          next();
          return;
        }

        const requestUrl = new URL(req.url || '/', 'http://localhost');
        const pathname = requestUrl.pathname;

        if (pathname === '/' || pathname === '/client') {
          res.statusCode = 302;
          res.setHeader('Location', '/client/');
          res.end();
          return;
        }

        const acceptsHtml = (req.headers.accept || '').includes('text/html');
        const isClientRoute =
          pathname === '/client/' ||
          (pathname.startsWith('/client/') &&
            !pathname.startsWith('/client/src/') &&
            !path.extname(pathname));

        if (!acceptsHtml || !isClientRoute) {
          next();
          return;
        }

        try {
          const indexPath = path.resolve(__dirname, 'client/index.html');
          const html = await fs.readFile(indexPath, 'utf8');
          const transformed = await server.transformIndexHtml(
            req.url || '/client/',
            html,
          );
          res.statusCode = 200;
          res.setHeader('Content-Type', 'text/html; charset=utf-8');
          res.end(transformed);
        } catch (error) {
          next(error);
        }
      });
    },
  };
}

export default defineConfig({
  appType: 'custom',
  plugins: [clientSpaFallbackPlugin(), react(), tsconfigPaths()],
  define: {
    'process.env': JSON.stringify(clientProcessEnv),
    global: 'globalThis',
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'client/src'),
    },
  },
  server: {
    watch: {
      ignored: ['**/dist/**'],
    },
    proxy: {
      '/api': {
        target: devflowApiBaseUrl,
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: path.resolve(__dirname, 'dist/client'),
    emptyOutDir: true,
    sourcemap: process.env.NODE_ENV === 'production' ? 'hidden' : true,
    rollupOptions: {
      input: path.resolve(__dirname, 'client/index.html'),
    },
  },
  optimizeDeps: {
    esbuildOptions: {
      define: {
        'process.env': JSON.stringify(clientProcessEnv),
        global: 'globalThis',
      },
    },
  },
  publicDir: path.resolve(__dirname, 'client/public'),
});
