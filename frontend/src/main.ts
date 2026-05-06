import { createApp } from 'vue'
import { createPinia } from 'pinia'
import TDesign from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import 'highlight.js/styles/github-dark.min.css'
import './style.css'
import App from './App.vue'

createApp(App).use(createPinia()).use(TDesign).mount('#app')
