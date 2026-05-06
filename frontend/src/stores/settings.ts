import { defineStore } from 'pinia'
import { Call } from '@wailsio/runtime'

export const useSettings = defineStore('settings', {
  state: () => ({ raw: '', dirty: false, saving: false, error: '' }),
  actions: {
    async load() {
      this.raw = await Call.ByName('main.App.GetConfig')
      this.dirty = false
    },
    edit(next: string) {
      this.raw = next
      this.dirty = true
    },
    async save() {
      this.saving = true
      this.error = ''
      try {
        await Call.ByName('main.App.SetConfig', this.raw)
        this.dirty = false
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      } finally {
        this.saving = false
      }
    },
  },
})
