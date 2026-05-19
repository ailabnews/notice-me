import { createApp } from 'vue'
import { createPinia } from 'pinia'
import TDesign from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import 'highlight.js/styles/github-dark.min.css'
import './style.css'
import App from './App.vue'

const app = createApp(App)

// Single reusable error banner — replaces itself instead of stacking.
let _errEl: HTMLDivElement | null = null

app.config.errorHandler = (err, _instance, info) => {
  console.error('[notify-me] Vue error:', err, info)

  // Remove previous banner
  if (_errEl) { _errEl.remove(); _errEl = null }

  let msg = ''
  if (err instanceof Error) {
    msg = err.message || err.name
  } else {
    msg = String(err)
  }
  // Truncate long messages
  if (msg.length > 120) msg = msg.slice(0, 117) + '...'

  const d = document.createElement('div')
  d.style.cssText =
    'position:fixed;bottom:12px;left:50%;transform:translateX(-50%);' +
    'background:#fdecee;color:#d5293b;padding:6px 28px 6px 14px;border-radius:6px;' +
    'font-size:12px;line-height:1.4;z-index:99999;white-space:nowrap;max-width:90vw;overflow:hidden;text-overflow:ellipsis'
  d.textContent = msg
  d.title = String(err instanceof Error ? err.stack : err)

  const close = document.createElement('span')
  close.textContent = '×'
  close.style.cssText = 'position:absolute;right:8px;top:50%;transform:translateY(-50%);cursor:pointer;font-size:14px;line-height:1'
  close.onclick = () => { d.remove(); _errEl = null }
  d.style.position = 'relative'
  d.style.display = 'inline-block'
  d.appendChild(close)

  document.body.appendChild(d)
  _errEl = d
}

app.use(createPinia()).use(TDesign).mount('#app')
