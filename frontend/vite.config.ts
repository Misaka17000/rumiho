import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import { version } from './package.json'

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(version),
    __COMMIT_ID__: JSON.stringify('rumiho'),
    __FONT__: JSON.stringify('none'),
  },
  base: '/',
  plugins: [vue(), vueJsx()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    host: '127.0.0.1',
    proxy: {
      '/api': { target: 'http://127.0.0.1:18080' },
      '/mihomo': { target: 'http://127.0.0.1:18080', ws: true },
    },
  },
})
