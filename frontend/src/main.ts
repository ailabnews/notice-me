import { createApp } from 'vue'
import { createPinia } from 'pinia'
import TDesign from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import 'highlight.js/styles/github-dark.min.css'
import './style.css'
import App from './App.vue'

const app = createApp(App)
app.config.errorHandler = (err, instance, info) => {
  console.error('[notify-me] Vue error:', err, info)
  const d = document.createElement('pre')
  d.style.cssText = 'position:fixed;bottom:0;left:0;right:0;background:#fee;color:#c00;padding:8px;font-size:12px;z-index:99999;white-space:pre-wrap;margin:0'
  let msg = 'VUE ERROR:\n'
  if (err instanceof Error) {
    msg += `name: ${err.name}\nmessage: ${err.message}\nstack:\n${err.stack}\n`
  } else {
    msg += `value: ${JSON.stringify(err)}\ntype: ${typeof err}\n`
  }
  msg += `\ninfo: ${info}`
  d.textContent = msg
  document.body.appendChild(d)
}
app.use(createPinia()).use(TDesign).mount('#app')
