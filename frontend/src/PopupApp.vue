<template>
  <div class="popup">
    <pre class="popup-message">{{ message }}</pre>
    <div class="popup-actions">
      <button v-if="cancelText" class="cancel" @click="resolve(cancelText)">{{ cancelText }}</button>
      <button class="ok" @click="resolve(okText)">
        {{ okText }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'

const id = ref<number>(0)
const title = ref(''),
  message = ref(''),
  okText = ref('确定'),
  cancelText = ref('')
const mode = ref<'two-button' | 'single-button'>('two-button')

let resolveBase = ''

onMounted(() => {
  const p = new URLSearchParams(window.location.search)
  id.value = Number(p.get('id')) || 0
  title.value = p.get('title') || ''
  message.value = p.get('message') || ''
  okText.value = p.get('ok_text') || '确定'
  cancelText.value = p.get('cancel_text') || ''
  mode.value = (p.get('mode') as any) || 'two-button'
  const port = p.get('port') || '1886'
  const prefix = (p.get('prefix') || '/api').replace(/\/+$/, '')
  resolveBase = `http://127.0.0.1:${port}${prefix}/_resolve`

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
</script>
