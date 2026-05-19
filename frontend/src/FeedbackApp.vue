<template>
  <div class="feedback-popup">
    <h3 class="feedback-title">问题反馈</h3>
    <div class="feedback-field">
      <label>标题 <span class="required">*</span></label>
      <input v-model="title" placeholder="简要描述问题" maxlength="200" />
    </div>
    <div class="feedback-field">
      <label>问题描述 <span class="required">*</span></label>
      <textarea v-model="body" placeholder="详细描述遇到的问题、复现步骤等" rows="6" maxlength="5000"></textarea>
    </div>
    <div class="feedback-field">
      <label>联系邮箱（可选）</label>
      <input v-model="email" placeholder="方便我们联系您" />
    </div>
    <div class="feedback-actions">
      <button class="btn-cancel" @click="close">取消</button>
      <button class="btn-submit" @click="submit" :disabled="loading">{{ loading ? '提交中...' : '提交' }}</button>
    </div>
    <!-- Toast -->
    <transition name="toast-fade">
      <div v-if="toast.visible" :class="['toast', toast.type]">{{ toast.text }}</div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { Call } from '@wailsio/runtime'

const title = ref('')
const body = ref('')
const email = ref('')
const loading = ref(false)

const toast = reactive({ visible: false, text: '', type: 'error' as 'success' | 'error' })
let toastTimer: ReturnType<typeof setTimeout> | null = null

function showToast(text: string, type: 'success' | 'error' = 'error') {
  if (toastTimer) clearTimeout(toastTimer)
  toast.text = text
  toast.type = type
  toast.visible = true
  toastTimer = setTimeout(() => { toast.visible = false }, 3000)
}

onMounted(() => {
  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') close()
  })
})

function extractMsg(e: any): string {
  let raw = e?.message ?? String(e)
  // Strip Wails internal prefix like "调用后端失败: main.App.XXX: "
  const colonIdx = raw.indexOf(': runtime error')
  if (colonIdx > 0) return '提交失败，请稍后重试'
  const failIdx = raw.indexOf(': ')
  if (failIdx > 0 && failIdx < 40) raw = raw.slice(failIdx + 2)
  return raw || '提交失败，请稍后重试'
}

async function submit() {
  if (!title.value.trim()) { showToast('请填写标题'); return }
  if (!body.value.trim()) { showToast('请填写问题描述'); return }

  loading.value = true
  try {
    await Call.ByName('main.App.SubmitFeedback', title.value.trim(), body.value.trim(), email.value.trim())
    showToast('提交成功，感谢反馈！', 'success')
    setTimeout(close, 2000)
  } catch (e: any) {
    showToast(extractMsg(e))
  } finally {
    loading.value = false
  }
}

function close() {
  Call.ByName('main.App.CloseFeedback').catch(() => {})
}
</script>
