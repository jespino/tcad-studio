import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { studioProxy } from '@jespino/agentstudio/vite';

// In development, run `tcad studio -addr 127.0.0.1:34118` and `npm run dev`
// (or just `make dev`): Vite proxies the API and the viewer to it.
const backend = process.env.TCAD_BACKEND ?? 'http://127.0.0.1:34118';

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 5173,
    proxy: studioProxy({ backend, routes: ['/preview'] }),
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500,
    // The build is committed (go build needs no Node): stable names keep
    // its diffs small.
    rollupOptions: {
      output: { entryFileNames: 'assets/[name].js', chunkFileNames: 'assets/[name].js', assetFileNames: 'assets/[name][extname]' },
    },
  },
});
