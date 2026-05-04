<template>
  <div class="history">
    <div class="filter-bar">
      <button @click="store.load()" :disabled="store.loading">
        {{ store.loading ? '加载中...' : '刷新' }}
      </button>
      <button @click="confirmClear" class="danger-btn">清空全部</button>
    </div>
    <div class="content-area">
      <div class="table-wrapper">
        <table class="records">
          <thead>
            <tr>
              <th>时间</th><th>端点</th><th>标题</th><th>来源</th>
              <th>状态</th><th>耗时</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in store.records"
              :key="r.id"
              :class="{ selected: store.selectedId === r.id }"
              @click="store.selectRecord(r.id)"
            >
              <td>{{ formatTs(r.created_at) }}</td>
              <td>{{ r.endpoint }}</td>
              <td :title="r.title">{{ r.title }}</td>
              <td class="muted">{{ r.source_ip || '-' }}</td>
              <td><span class="badge" :data-status="badgeStatus(r.status)">{{ statusText(r.status) }}</span></td>
              <td>{{ formatDuration(r.duration_ms) }}</td>
              <td><button class="icon-btn" @click.stop="store.deleteRecord(r.id)" title="删除">&times;</button></td>
            </tr>
            <tr v-if="store.records.length === 0 && !store.loading">
              <td colspan="7" class="empty">暂无记录</td>
            </tr>
          </tbody>
        </table>
        <div v-if="store.loading" class="loading-overlay">
          <span class="spinner"></span>
        </div>
      </div>
      <div v-if="store.detail" class="detail-panel">
        <div class="detail-header">
          <span>记录详情</span>
          <button class="icon-btn" @click="store.selectRecord(null)">&times;</button>
        </div>
        <div class="detail-body">
          <div class="detail-row"><label>状态</label><span class="badge" :data-status="badgeStatus(store.detail.status)">{{ statusText(store.detail.status) }}</span></div>
          <div class="detail-row"><label>端点</label><span>{{ store.detail.endpoint }}</span></div>
          <div class="detail-row"><label>标题</label><span>{{ store.detail.title }}</span></div>
          <div class="detail-row"><label>消息</label><pre class="detail-message">{{ store.detail.message }}</pre></div>
          <div class="detail-row"><label>来源 IP</label><span>{{ store.detail.source_ip || '-' }}</span></div>
          <div class="detail-row"><label>来源头</label><span>{{ store.detail.source_header || '-' }}</span></div>
          <div class="detail-row"><label>创建时间</label><span>{{ formatTs(store.detail.created_at) }}</span></div>
          <div class="detail-row"><label>完成时间</label><span>{{ store.detail.resolved_at ? formatTs(store.detail.resolved_at) : '-' }}</span></div>
          <div class="detail-row"><label>耗时</label><span>{{ formatDuration(store.detail.duration_ms) }}</span></div>
        </div>
        <div class="detail-actions">
          <button @click="store.deleteRecord(store.detail!.id)" class="danger-btn">删除此记录</button>
        </div>
      </div>
    </div>
    <div class="pager">
      <button @click="store.prev()" :disabled="store.page === 0">上一页</button>
      <span>{{ store.page + 1 }} / {{ Math.max(1, Math.ceil(store.total / store.pageSize)) }}</span>
      <button @click="store.next()" :disabled="(store.page + 1) * store.pageSize >= store.total">下一页</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useHistory } from '../stores/history'

const store = useHistory()

const systemStatus: Record<string, string> = {
  pending: '等待中',
  timeout: '超时',
  cancelled: '已取消',
}

function statusText(s: string) { return systemStatus[s] || s }

function badgeStatus(s: string): string {
  if (systemStatus[s]) return s
  // User button text — classify by common patterns for coloring
  return 'action'
}

function formatTs(ms: number) {
  return new Date(ms).toLocaleString()
}

function formatDuration(ms: number | null): string {
  if (!ms) return '-'
  if (ms < 1000) return ms + 'ms'
  if (ms < 60_000) return (ms / 1000).toFixed(1) + 's'
  const min = Math.floor(ms / 60_000)
  const sec = Math.floor((ms % 60_000) / 1000)
  return min + 'm' + (sec > 0 ? sec + 's' : '')
}

function confirmClear() {
  if (window.confirm('确定清空所有通知记录？此操作不可撤销。')) {
    store.clearAll()
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') store.selectRecord(null)
}

onMounted(() => {
  store.load()
  store.startAutoRefresh(5000)
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  store.stopAutoRefresh()
  window.removeEventListener('keydown', onKeydown)
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
  align-items: center;
}
.content-area {
  display: flex;
  overflow: hidden;
  position: relative;
  min-height: 0;
}
.table-wrapper {
  flex: 1;
  overflow: auto;
  position: relative;
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
  white-space: nowrap;
}
.records tbody tr {
  cursor: pointer;
  transition: background .1s;
}
.records tbody tr:hover {
  background: #f9fafb;
}
.records tbody tr.selected {
  background: #eff6ff;
}
.empty {
  text-align: center;
  color: #9ca3af;
  padding: 24px 0;
}
.muted {
  color: #9ca3af;
  font-size: 12px;
}
.badge {
  padding: 2px 6px;
  border-radius: 4px;
  background: #e5e7eb;
  font-size: 12px;
}
.badge[data-status='pending'] {
  background: #fef3c7;
  color: #92400e;
}
.badge[data-status='timeout'] {
  background: #fef3c7;
  color: #92400e;
}
.badge[data-status='cancelled'] {
  background: #e5e7eb;
  color: #4b5563;
}
.badge[data-status='action'] {
  background: #dcfce7;
  color: #166534;
}
.icon-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: #9ca3af;
  font-size: 16px;
  padding: 2px 4px;
  line-height: 1;
}
.icon-btn:hover {
  color: #374151;
}
.danger-btn {
  color: #b91c1c;
  border: 1px solid #fecaca;
  background: #fff;
  padding: 5px 10px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}
.danger-btn:hover {
  background: #fef2f2;
}

/* Detail side panel */
.detail-panel {
  width: 280px;
  min-width: 280px;
  border-left: 1px solid #e5e7eb;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  background: #fafafa;
}
.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  border-bottom: 1px solid #e5e7eb;
  font-weight: 500;
  font-size: 13px;
}
.detail-body {
  flex: 1;
  padding: 8px 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  overflow-y: auto;
}
.detail-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 13px;
}
.detail-row label {
  color: #6b7280;
  font-size: 12px;
}
.detail-message {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 13px;
  background: #fff;
  padding: 6px 8px;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  max-height: 200px;
  overflow-y: auto;
}
.detail-actions {
  padding: 8px 12px;
  border-top: 1px solid #e5e7eb;
}

/* Loading overlay */
.loading-overlay {
  position: absolute;
  inset: 0;
  background: rgba(255, 255, 255, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
}
.spinner {
  width: 20px;
  height: 20px;
  border: 2px solid #d1d5db;
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin .6s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

.pager {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: flex-end;
}
.pager button,
.filter-bar button:not(.danger-btn) {
  padding: 5px 10px;
  border-radius: 4px;
  border: 1px solid #d1d5db;
  background: #fff;
  cursor: pointer;
  font-size: 13px;
}
.pager button:disabled,
.filter-bar button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
