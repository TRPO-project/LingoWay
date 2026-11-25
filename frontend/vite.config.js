import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Браузер отправляет запросы на /api. Адрес бэкенда нужен только прокси Vite.
const proxy = { '/api': process.env.API_PROXY_TARGET || 'http://127.0.0.1:8080' };

export default defineConfig({
  plugins: [react()],
  server: { host: '127.0.0.1', port: 5173, strictPort: true, proxy },
  preview: { host: '127.0.0.1', port: 5173, strictPort: true, proxy },
});
