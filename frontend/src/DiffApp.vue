<template>
  <div class="diff-viewer">
    <div class="diff-header">
      <div class="diff-file">
        <span class="diff-tool">{{ payload?.tool_name }}</span>
        <code>{{ payload?.file_path }}</code>
      </div>
      <div class="diff-toolbar">
        <button :class="['mode-btn', { active: mode === 'line-by-line' }]" @click="mode = 'line-by-line'">Unified</button>
        <button :class="['mode-btn', { active: mode === 'side-by-side' }]" @click="mode = 'side-by-side'">Split</button>
      </div>
    </div>

    <div class="diff-body" ref="diffContainer" v-if="hasDiff"></div>
    <div class="diff-body diff-empty" v-else-if="loaded">无差异</div>
    <div class="diff-body diff-loading" v-else>加载中...</div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { html as renderDiff2Html } from 'diff2html'
import 'diff2html/bundles/css/diff2html.min.css'
import * as Diff from 'diff'

interface DiffPayload {
  tool_name: string
  file_path: string
  old_string: string
  new_string: string
}

const payload = ref<DiffPayload | null>(null)
const loaded = ref(false)
const hasDiff = ref(false)
const mode = ref<'line-by-line' | 'side-by-side'>('side-by-side')
const diffContainer = ref<HTMLElement | null>(null)

const unifiedDiff = computed(() => {
  if (!payload.value) return ''
  const patch = Diff.createPatch(
    payload.value.file_path || 'file',
    payload.value.old_string || '',
    payload.value.new_string || '',
    'old',
    'new'
  )
  return patch
})

const diffHtml = computed(() => {
  if (!unifiedDiff.value || unifiedDiff.value === '--- \n+++ \n') return ''
  return renderDiff2Html(unifiedDiff.value, {
    drawFileList: false,
    matching: 'lines',
    outputFormat: mode.value,
  })
})

function renderDiff() {
  if (!diffContainer.value) return
  diffContainer.value.innerHTML = diffHtml.value
}

watch(diffHtml, () => nextTick(renderDiff))
watch(mode, () => nextTick(renderDiff))

onMounted(async () => {
  const p = new URLSearchParams(window.location.search)
  const notifId = Number(p.get('id')) || 0
  const port = p.get('port') || '1886'
  const prefix = (p.get('prefix') || '/api').replace(/\/+$/, '')
  const base = `http://127.0.0.1:${port}${prefix}`

  if (notifId) {
    try {
      const resp = await fetch(`${base}/_diff?id=${notifId}`)
      if (resp.ok) {
        payload.value = await resp.json()
      }
    } catch {
      // ignore
    }
    loaded.value = true
  }

  if (payload.value?.file_path) {
    document.title = `Diff: ${payload.value.file_path.split('/').pop()}`
  }

  // Check if there is actual diff content
  hasDiff.value = !!unifiedDiff.value && unifiedDiff.value !== '--- \n+++ \n'
  nextTick(renderDiff)
})
</script>
