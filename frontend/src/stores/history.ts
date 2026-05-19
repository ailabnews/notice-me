import { defineStore } from 'pinia'

export interface Record {
  id: number
  endpoint: string
  title: string
  message: string
  source_ip: string
  source_header: string
  status: string
  created_at: number
  resolved_at: number
  duration_ms: number
}

const base = `http://127.0.0.1:1886/api`

export const useHistory = defineStore('history', {
  state: () => ({
    records: [] as Record[],
    total: 0,
    pageSize: 20,
    page: 0,
    statusFilter: '',
    search: '',
    loading: false,
    selectedId: null as number | null,
    detail: null as Record | null,
    _timer: null as ReturnType<typeof setInterval> | null,
  }),
  actions: {
    async load() {
      this.loading = true
      try {
        const params = new URLSearchParams({
          status: this.statusFilter,
          search: this.search,
          limit: String(this.pageSize),
          offset: String(this.page * this.pageSize),
        })
        const res = await fetch(`${base}/_history?${params}`)
        const data = await res.json()
        this.records = data.records ?? []
        this.total = data.total ?? 0
      } catch {
        // Server not ready — auto-refresh will retry
      } finally {
        this.loading = false
      }
    },
    setFilter(s: string) {
      this.statusFilter = s
      this.page = 0
      this.load()
    },
    setSearch(s: string) {
      this.search = s
      this.page = 0
      this.load()
    },
    selectRecord(id: number | null) {
      this.selectedId = id
      if (id == null) {
        this.detail = null
        return
      }
      this.detail = this.records.find(r => r.id === id) ?? null
    },
    async deleteRecord(id: number) {
      try {
        await fetch(`${base}/_history/delete?id=${id}`, { method: 'POST' })
      } catch { /* ignore */ }
      if (this.selectedId === id) {
        this.detail = null
        this.selectedId = null
      }
      this.load()
    },
    async clearAll() {
      try {
        await fetch(`${base}/_history/clear`, { method: 'POST' })
      } catch { /* ignore */ }
      this.detail = null
      this.selectedId = null
      this.page = 0
      this.load()
    },
    async resolveRecord(id: number, decision: string) {
      try {
        await fetch(`${base}/_resolve?id=${id}&decision=${decision}`, { method: 'POST' })
      } catch { /* ignore */ }
      this.load()
    },
    startAutoRefresh(ms = 5000) {
      this.stopAutoRefresh()
      this._timer = setInterval(() => this.load(), ms)
    },
    stopAutoRefresh() {
      if (this._timer) {
        clearInterval(this._timer)
        this._timer = null
      }
    },
    next() {
      if ((this.page + 1) * this.pageSize < this.total) {
        this.page++
        this.load()
      }
    },
    prev() {
      if (this.page > 0) {
        this.page--
        this.load()
      }
    },
  },
})
