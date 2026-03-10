import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { viteStaticCopy } from 'vite-plugin-static-copy';

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080';
const wsProxyTarget = apiProxyTarget.replace(/^http/, 'ws');

// Vite configuration for the React-admin workspace. Provides React fast refresh and local proxying to the Go API.
// Swagger UI assets are copied from swagger-ui-dist into dist/swagger/ during build.
export default defineConfig({
  plugins: [
    react(),
    viteStaticCopy({
      targets: [
        {
          src: 'node_modules/swagger-ui-dist/swagger-ui-bundle.js',
          dest: 'swagger',
        },
        {
          src: 'node_modules/swagger-ui-dist/swagger-ui-standalone-preset.js',
          dest: 'swagger',
        },
        {
          src: 'node_modules/swagger-ui-dist/swagger-ui.css',
          dest: 'swagger',
        },
      ],
    }),
  ],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
      },
      '/ws': {
        target: wsProxyTarget,
        changeOrigin: true,
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
});
