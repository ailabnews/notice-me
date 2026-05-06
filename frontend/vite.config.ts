import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [
    vue(),
    // Strip crossorigin attributes — Wails serves assets via a custom protocol
    // handler (not HTTP), so CORS headers are absent.  The crossorigin attribute
    // causes WebView2 to block script execution silently (white screen).
    {
      name: 'strip-crossorigin',
      transformIndexHtml(html) {
        return html.replace(/ crossorigin/g, '')
      },
    },
  ],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        popup: resolve(__dirname, 'popup.html'),
        diff: resolve(__dirname, 'diff.html'),
      },
    },
  },
})
