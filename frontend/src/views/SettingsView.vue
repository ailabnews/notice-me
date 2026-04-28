<template>
  <div class="settings">
    <p class="hint">修改 host / port 后需重启程序生效。其他项保存即生效。</p>
    <textarea v-model="local" spellcheck="false"></textarea>
    <p v-if="store.error" class="err">{{ store.error }}</p>
    <div class="actions">
      <button @click="reset" :disabled="store.saving">重置为已保存</button>
      <button @click="save" :disabled="!store.dirty || store.saving">
        {{ store.saving ? '保存中…' : '保存' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useSettings } from '../stores/settings'

const store = useSettings()
const local = ref('')

onMounted(async () => {
  if (window.go?.main?.App?.GetConfig) {
    await store.load()
    local.value = store.raw
  }
})

watch(local, (v) => store.edit(v))

async function reset() {
  await store.load()
  local.value = store.raw
}

async function save() {
  await store.save()
  if (!store.error) local.value = store.raw
}
</script>

<style scoped>
.settings {
  display: grid;
  grid-template-rows: auto 1fr auto auto;
  gap: 8px;
  height: 100%;
}
.hint {
  color: #6b7280;
  font-size: 13px;
  margin: 0;
}
textarea {
  width: 100%;
  height: 100%;
  font-family: ui-monospace, monospace;
  font-size: 12px;
  padding: 8px;
  box-sizing: border-box;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  resize: none;
}
.err {
  color: #b91c1c;
  font-size: 13px;
  margin: 0;
  padding: 6px 8px;
  background: #fef2f2;
  border: 1px solid #fecaca;
  border-radius: 4px;
}
.actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
.actions button {
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid #d1d5db;
  cursor: pointer;
  background: white;
}
.actions button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
