<template>
  <div class="main">
    <header class="banner" v-if="banner">{{ banner }}</header>
    <nav class="tabs">
      <button :class="{ active: tab === 'home' }" @click="tab = 'home'">首页</button>
      <button :class="{ active: tab === 'history' }" @click="tab = 'history'">通知历史</button>
      <button :class="{ active: tab === 'audit' }" @click="tab = 'audit'">审核</button>
      <button :class="{ active: tab === 'settings' }" @click="tab = 'settings'">设置</button>
    </nav>
    <section class="content">
      <component :is="active" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import HomeView from './views/HomeView.vue'
import HistoryView from './views/HistoryView.vue'
import AuditView from './views/AuditView.vue'
import SettingsView from './views/SettingsView.vue'

const tab = ref<'home' | 'history' | 'audit' | 'settings'>('home')
const banner = ref('')
const active = computed(() => {
  switch (tab.value) {
    case 'home': return HomeView
    case 'history': return HistoryView
    case 'audit': return AuditView
    default: return SettingsView
  }
})
</script>
