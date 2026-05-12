import { defineStore } from 'pinia'
import { Call } from '@wailsio/runtime'

export interface ToolUsage { tool: string; count: number }
export interface DecisionStats { approved: number; denied: number; timeout: number; cancelled: number; other: number }
export interface SessionInfo { session_id: string; last_seen: number; count: number }
export interface HookStatus {
  configured: boolean; mode: string; settings_path: string; hooks_found: string[]
  installed: boolean; binary_path: string; claude_version: string; error?: string
}

export const useHome = defineStore('home', {
  state: () => ({
    toggles: { sound_enabled: true, blink_enabled: true, stop_hook_enabled: true } as { sound_enabled: boolean; blink_enabled: boolean; stop_hook_enabled: boolean },
    hookStatus: null as HookStatus | null,
    stats: {
      tool_usage: [] as ToolUsage[],
      decisions: { approved: 0, denied: 0, timeout: 0, cancelled: 0, other: 0 } as DecisionStats,
      avg_response_ms: 0,
      sessions: [] as SessionInfo[],
      total: 0,
    },
    activeSessionId: null as string | null,
    timeline: [] as any[],
    transcript: [] as any[],
    loading: false,
    lastError: '' as string,
    _timer: null as ReturnType<typeof setInterval> | null,
  }),
  actions: {
    async loadToggles() {
      const raw: string = await Call.ByName('main.App.GetQuickToggles')
      this.toggles = JSON.parse(raw)
    },
    setSound(enabled: boolean) {
      this.toggles.sound_enabled = enabled
      Call.ByName('main.App.SetSoundEnabled', enabled).catch((e: any) => {
        console.error('[notify-me] SetSoundEnabled failed:', e)
        this.lastError = 'SetSoundEnabled 失败: ' + String(e)
      })
    },
    setBlink(enabled: boolean) {
      this.toggles.blink_enabled = enabled
      Call.ByName('main.App.SetBlinkEnabled', enabled).catch((e: any) => {
        console.error('[notify-me] SetBlinkEnabled failed:', e)
        this.lastError = 'SetBlinkEnabled 失败: ' + String(e)
      })
    },
    setStopHook(enabled: boolean) {
      this.toggles.stop_hook_enabled = enabled
      Call.ByName('main.App.SetStopHookEnabled', enabled).then(async () => {
        if (this.hookStatus?.configured) {
          await Call.ByName('main.App.ConfigureHooks', this.hookStatus.mode)
          await this.loadHookStatus()
        }
      }).catch((e: any) => {
        console.error('[notify-me] SetStopHookEnabled failed:', e)
        this.lastError = 'SetStopHookEnabled 失败: ' + String(e)
      })
    },
    async loadHookStatus() {
      const raw: string = await Call.ByName('main.App.GetHookStatus')
      this.hookStatus = JSON.parse(raw)
    },
    async configureHooks(mode: string) {
      this.lastError = ''
      try {
        const result = await Call.ByName('main.App.ConfigureHooks', mode)
        // Wails v3 may return error as a string value instead of throwing
        if (result && typeof result === 'object' && (result as any).error) {
          throw new Error((result as any).error)
        }
        await this.loadHookStatus()
      } catch (e: any) {
        const msg = e?.message ?? String(e)
        this.lastError = msg
        window.alert(msg)
      }
    },
    async removeHooks() {
      this.lastError = ''
      try {
        await Call.ByName('main.App.RemoveHooks')
        await this.loadHookStatus()
      } catch (e: any) {
        const msg = e?.message ?? String(e)
        this.lastError = msg
        window.alert(msg)
      }
    },
    async loadStats() {
      const raw: string = await Call.ByName('main.App.DashboardStats')
      const parsed = JSON.parse(raw)
      this.stats = {
        tool_usage: parsed?.tool_usage ?? [],
        decisions: parsed?.decisions ?? { approved: 0, denied: 0, timeout: 0, cancelled: 0, other: 0 },
        avg_response_ms: parsed?.avg_response_ms ?? 0,
        sessions: parsed?.sessions ?? [],
        total: parsed?.total ?? 0,
      }
    },
    async loadTimeline(sessionId: string) {
      this.activeSessionId = sessionId
      const raw: string = await Call.ByName('main.App.SessionTimeline', sessionId, 50)
      this.timeline = JSON.parse(raw)
      this.transcript = []
    },
    async loadTranscript(path: string) {
      const raw: string = await Call.ByName('main.App.TranscriptMessages', path)
      const parsed = JSON.parse(raw)
      if (parsed && parsed.error) {
        throw new Error(parsed.error)
      }
      this.transcript = Array.isArray(parsed) ? parsed : []
    },
    startAutoRefresh(ms = 5000) {
      this.stopAutoRefresh()
      this._timer = setInterval(() => { this.loadStats() }, ms)
    },
    stopAutoRefresh() {
      if (this._timer) { clearInterval(this._timer); this._timer = null }
    },
  },
})
