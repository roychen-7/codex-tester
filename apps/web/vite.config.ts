import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '^/api(?:/|$)': {
        target: process.env.API_TARGET ?? `http://127.0.0.1:${process.env.PORT ?? 3000}`,
        rewrite: path => path.replace(/^\/api(?=\/|$)/, ''),
      },
    },
  },
  preview: {
    host: '127.0.0.1',
    port: 4173,
    strictPort: true,
  },
});
