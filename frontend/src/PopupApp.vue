<template>
  <div class="popup">
    <div class="popup-content">
      <div class="popup-message" v-html="renderedMessage"></div>
      <button v-if="hasDiff" class="diff-link" @click="openDiff" title="查看 Diff">Diff</button>
    </div>
    <div class="popup-actions">
      <button v-if="cancelText" class="cancel" @click="resolve(cancelText)">{{ cancelText }}</button>
      <button v-if="hasSessionAuth" class="session-auth" @click="sessionAuth">会话授权</button>
      <button class="ok" @click="resolve(okText)">
        {{ okText }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { renderMd } from './markdown'

const id = ref<number>(0)
const title = ref('')
const message = ref(''),
  okText = ref('确定'),
  cancelText = ref('')
const mode = ref<'two-button' | 'single-button'>('two-button')
const hasDiff = ref(false)
const hasSessionAuth = ref(false)

const renderedMessage = computed(() => renderMd(message.value))

let resolveBase = ''
let serverBase = ''

onMounted(() => {
  const p = new URLSearchParams(window.location.search)
  id.value = Number(p.get('id')) || 0
  title.value = p.get('title') || ''
  message.value = p.get('message') || ''
  okText.value = p.get('ok_text') || '确定'
  cancelText.value = p.get('cancel_text') || ''
  mode.value = (p.get('mode') as any) || 'two-button'
  hasDiff.value = p.get('has_diff') === 'true'
  // temporarily disabled: hasSessionAuth.value = p.get('has_session_auth') === 'true'
  const port = p.get('port') || '1886'
  const prefix = (p.get('prefix') || '/api').replace(/\/+$/, '')
  serverBase = `http://127.0.0.1:${port}${prefix}`
  resolveBase = `${serverBase}/_resolve`

  // Set window title from notification title.
  if (title.value) {
    document.title = title.value
  }

  window.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') resolve(okText.value)
    if (e.key === 'Escape') resolve(cancelText.value || '取消')
  })
})

async function resolve(decision: string) {
  if (!resolveBase || !id.value) return
  try {
    await fetch(`${resolveBase}?id=${id.value}&decision=${decision}`, { method: 'POST' })
  } catch {
    // Go side will close the popup; ignore network errors.
  }
}

async function sessionAuth() {
  if (!resolveBase || !id.value) return
  try {
    await fetch(`${resolveBase}?id=${id.value}&decision=auto_session`, { method: 'POST' })
  } catch {
    // Go side will close the popup
  }
}

async function openDiff() {
  if (!serverBase || !id.value) return
  try {
    await fetch(`${serverBase}/_open-diff?id=${id.value}`, { method: 'POST' })
  } catch {
    // ignore
  }
}
</script>
