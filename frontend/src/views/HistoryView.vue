<template>
  <div class="history">
    <div class="filter-bar">
      <select v-model="store.statusFilter" @change="store.setFilter(store.statusFilter)">
        <option value="">全部</option>
        <option value="approved">已批准</option>
        <option value="denied">已拒绝</option>
        <option value="acknowledged">已知晓</option>
        <option value="timeout">超时</option>
        <option value="cancelled">已取消</option>
      </select>
      <button @click="store.load()" :disabled="store.loading">刷新</button>
    </div>
    <table class="records">
      <thead>
        <tr><th>时间</th><th>端点</th><th>标题</th><th>状态</th><th>耗时</th></tr>
      </thead>
      <tbody>
        <tr v-for="r in store.records" :key="r.id">
          <td>{{ formatTs(r.created_at) }}</td>
          <td>{{ r.endpoint }}</td>
          <td :title="r.message">{{ r.title }}</td>
          <td><span class="badge" :data-status="r.status">{{ r.status }}</span></td>
          <td>{{ r.duration_ms ? r.duration_ms + 'ms' : '-' }}</td>
        </tr>
        <tr v-if="store.records.length === 0 && !store.loading">
          <td colspan="5" class="empty">暂无记录</td>
        </tr>
      </tbody>
    </table>
    <div class="pager">
      <button @click="store.prev()" :disabled="store.page === 0">上一页</button>
      <span>{{ store.page + 1 }} / {{ Math.max(1, Math.ceil(store.total / store.pageSize)) }}</span>
      <button @click="store.next()" :disabled="(store.page + 1) * store.pageSize >= store.total">下一页</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useHistory } from '../stores/history'
const store = useHistory()
function formatTs(ms: number) {
  return new Date(ms).toLocaleString()
}
onMounted(() => {
  // Tolerate the binding not being available in pure-frontend dev (browser without Wails runtime).
  if (window.go?.main?.App?.History) {
    store.load()
  }
})
</script>

<style scoped>
.history {
  display: grid;
  grid-template-rows: auto 1fr auto;
  gap: 8px;
  height: 100%;
}
.filter-bar {
  display: flex;
  gap: 8px;
}
.records {
  width: 100%;
  border-collapse: collapse;
}
.records th,
.records td {
  padding: 6px 8px;
  text-align: left;
  border-bottom: 1px solid #f0f0f0;
  font-size: 13px;
}
.empty {
  text-align: center;
  color: #9ca3af;
  padding: 24px 0;
}
.badge {
  padding: 2px 6px;
  border-radius: 4px;
  background: #e5e7eb;
  font-size: 12px;
}
.badge[data-status='approved'] {
  background: #dcfce7;
  color: #166534;
}
.badge[data-status='denied'] {
  background: #fee2e2;
  color: #991b1b;
}
.badge[data-status='timeout'] {
  background: #fef3c7;
  color: #92400e;
}
.badge[data-status='cancelled'] {
  background: #e5e7eb;
  color: #4b5563;
}
.badge[data-status='acknowledged'] {
  background: #dbeafe;
  color: #1e40af;
}
.pager {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: flex-end;
}
</style>
