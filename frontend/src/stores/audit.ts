import { defineStore } from 'pinia'
import { Call } from '@wailsio/runtime'

export interface PolicyRule {
  id: number
  type: 'global' | 'session'
  session_id: string
  tool_name: string
  pattern: string
  enabled: boolean
  priority: number
  source: 'manual' | 'popup'
  created_at: number
  updated_at: number
}

export const useAudit = defineStore('audit', {
  state: () => ({
    rules: [] as PolicyRule[],
    loading: false,
    error: '',
  }),
  getters: {
    globalRules: (s) => s.rules.filter(r => r.type === 'global'),
    sessionRules: (s) => s.rules.filter(r => r.type === 'session'),
  },
  actions: {
    async load() {
      this.loading = true
      this.error = ''
      try {
        const raw = await Call.ByName('main.App.GetPolicyRules')
        this.rules = JSON.parse((raw as string) || '[]')
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      } finally {
        this.loading = false
      }
    },
    async add(rule: Omit<PolicyRule, 'id' | 'created_at' | 'updated_at'>) {
      this.error = ''
      try {
        await Call.ByName('main.App.AddPolicyRule', JSON.stringify(rule))
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
    async update(rule: PolicyRule) {
      this.error = ''
      try {
        await Call.ByName('main.App.UpdatePolicyRule', JSON.stringify(rule))
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
    async remove(id: number) {
      this.error = ''
      try {
        await Call.ByName('main.App.DeletePolicyRule', id)
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
    async cleanup() {
      this.error = ''
      try {
        await Call.ByName('main.App.CleanupSessionRules')
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
  },
})
