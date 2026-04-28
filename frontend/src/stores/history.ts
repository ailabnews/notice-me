import { defineStore } from 'pinia'

export interface Record {
  id: number
  endpoint: string
  title: string
  message: string
  source_ip: string
  status: string
  created_at: number
  resolved_at: number
  duration_ms: number
}

export const useHistory = defineStore('history', {
  state: () => ({
    records: [] as Record[],
    total: 0,
    pageSize: 20,
    page: 0,
    statusFilter: '',
    loading: false,
  }),
  actions: {
    async load() {
      this.loading = true
      try {
        const res = await window.go.main.App.History(
          this.statusFilter,
          this.pageSize,
          this.page * this.pageSize,
        )
        this.records = res.records ?? []
        this.total = res.total ?? 0
      } finally {
        this.loading = false
      }
    },
    setFilter(s: string) {
      this.statusFilter = s
      this.page = 0
      this.load()
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
