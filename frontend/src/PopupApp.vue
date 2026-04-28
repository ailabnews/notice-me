<template>
  <div class="popup">
    <h1 class="popup-title">{{ title }}</h1>
    <pre class="popup-message">{{ message }}</pre>
    <div class="popup-actions">
      <button v-if="cancelText" class="cancel" @click="resolve('denied')">{{ cancelText }}</button>
      <button class="ok" @click="resolve(mode === 'single-button' ? 'acknowledged' : 'approved')">
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
  cancelText = ref('取消')
const mode = ref<'two-button' | 'single-button'>('two-button')

declare global {
  interface Window {
    runtime: any
    go: any
  }
}

onMounted(() => {
  if (window.runtime) {
    window.runtime.EventsOn('notification:show', (n: any) => {
      id.value = n.id
      title.value = n.title
      message.value = n.message
      okText.value = n.ok_text
      cancelText.value = n.cancel_text
      mode.value = n.mode
    })
  }
  window.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') resolve(mode.value === 'single-button' ? 'acknowledged' : 'approved')
    if (e.key === 'Escape') resolve('denied')
  })
})

function resolve(decision: string) {
  if (window.go?.main?.App?.Resolve) {
    window.go.main.App.Resolve(id.value, decision)
  }
}
</script>
