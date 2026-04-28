<template>
  <div class="main">
    <header class="banner" v-if="banner">{{ banner }}</header>
    <nav class="tabs">
      <button :class="{ active: tab === 'history' }" @click="tab = 'history'">通知历史</button>
      <button :class="{ active: tab === 'settings' }" @click="tab = 'settings'">设置</button>
    </nav>
    <section class="content">
      <component :is="active" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import HistoryView from './views/HistoryView.vue'
import SettingsView from './views/SettingsView.vue'

const tab = ref<'history' | 'settings'>('history')
const banner = ref('')
const active = computed(() => (tab.value === 'history' ? HistoryView : SettingsView))
</script>
