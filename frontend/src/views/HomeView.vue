<template>
  <div class="home">
    <!-- Quick Settings Card -->
    <div class="home-card">
      <h3 class="card-title">快捷设置</h3>
      <div class="toggles-row">
        <div class="toggle-item">
          <span>提示音</span>
          <t-switch v-model="soundEnabled" size="large" />
        </div>
        <div class="toggle-item">
          <span>图标闪动</span>
          <t-switch v-model="blinkEnabled" size="large" />
        </div>
      </div>
      <div class="toggle-error" v-if="store.lastError">{{ store.lastError }}</div>
    </div>

    <!-- Hook Config Card -->
    <div class="home-card">
      <h3 class="card-title">Claude Code Hook</h3>
      <div v-if="store.hookStatus">

        <!-- Environment Detection -->
        <div class="env-info">
          <div class="env-row" style="margin-bottom: 6px;">
            <span :class="['env-dot', store.hookStatus.installed ? 'ok' : 'off']"></span>
            <span class="env-label">{{ store.hookStatus.installed ? 'Claude Code 已安装' : '未检测到 Claude Code' }}</span>
            <span v-if="store.hookStatus.claude_version" class="env-version">{{ store.hookStatus.claude_version }}</span>
          </div>
          <template v-if="store.hookStatus.installed">
            <div class="env-path">{{ store.hookStatus.binary_path }}</div>
          </template>
          <div class="env-path" style="margin-top: 4px;">{{ store.hookStatus.settings_path }}</div>
        </div>

        <!-- Hook Configuration (only when Claude Code is installed) -->
        <template v-if="store.hookStatus.installed">
          <!-- Configured state -->
          <div v-if="store.hookStatus.configured" class="hook-configured">
            <div class="hook-indicator">
              <span class="hook-dot ok"></span>
              <span>已配置</span>
              <span class="hook-mode">({{ hookModeLabel }})</span>
            </div>
            <div class="hook-tags" v-if="store.hookStatus.hooks_found?.length">
              <span v-for="evt in store.hookStatus.hooks_found" :key="evt"
                :class="['hook-tag', isBlocking(evt) ? 'blocking' : 'non-blocking']">
                {{ eventName(evt) }}
              </span>
            </div>
          </div>

          <!-- Not configured state -->
          <div v-else class="hook-unconfigured">
            <div class="hook-indicator">
              <span class="hook-dot off"></span>
              <span>未配置</span>
            </div>
          </div>

          <!-- Stop Hook Toggle (shared) -->
          <div class="hook-toggle-row">
            <div>
              <div class="hook-toggle-label">完成通知</div>
              <div class="hook-toggle-desc">Claude Code 任务完成时弹窗提醒</div>
            </div>
            <t-switch v-model="stopHookEnabled" size="large" />
          </div>

          <!-- Actions -->
          <div class="hook-actions" v-if="store.hookStatus.configured">
            <t-button theme="danger" variant="outline" @click="doRemoveHooks">移除配置</t-button>
          </div>
          <div class="hook-actions" v-else>
            <t-button theme="primary" @click="doConfigureHooks('stdio')">一键配置 (本机)</t-button>
            <t-button theme="default" variant="outline" @click="showHttpDialog = true">HTTP 模式</t-button>
          </div>
        </template>

        <!-- Not installed hint -->
        <div v-else class="hook-not-installed">
          请先安装 Claude Code CLI，然后刷新此页面。
        </div>
      </div>
      <div v-else class="hook-loading">检测中...</div>
    </div>

    <!-- HTTP config dialog -->
    <t-dialog v-model:visible="showHttpDialog" header="HTTP 模式配置" :footer="false" width="640px" placement="center">
      <p class="http-hint">请将以下配置复制到 Claude Code 的配置文件中：</p>
      <p class="http-path">{{ store.hookStatus?.settings_path }}</p>
      <div class="http-code-wrap">
        <pre class="http-code">{{ httpHooksConfig }}</pre>
        <button class="http-copy-btn" @click="copyHttpConfig">复制</button>
      </div>
    </t-dialog>

    <!-- Transcript dialog -->
    <t-dialog v-model:visible="showTranscriptDialog" header="完整对话记录" :footer="false" width="720px" placement="center" class="transcript-dialog">
      <div class="transcript-scroll" v-if="store.transcript.length > 0">
        <div v-for="(msg, i) in store.transcript" :key="i" :class="['chat-msg', msg.type]">
          <div class="chat-avatar">{{ msg.type === 'user' ? '👤' : '🤖' }}</div>
          <div class="chat-body">
            <div class="chat-meta">
              <span class="chat-role">{{ msg.type === 'user' ? '用户' : 'Claude' }}</span>
              <span class="chat-time" v-if="msg.timestamp">{{ formatTime(msg.timestamp) }}</span>
            </div>
            <div class="chat-content" v-html="renderMsgContent(msg)"></div>
          </div>
        </div>
      </div>
      <div class="transcript-empty" v-else>加载中...</div>
    </t-dialog>

    <!-- Dashboard Stats -->
    <div class="home-card">
      <h3 class="card-title">会话仪表盘</h3>
      <div class="stats-grid">
        <div class="stat-card">
          <div class="stat-value">{{ store.stats.total }}</div>
          <div class="stat-label">总通知</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ approvalRate }}%</div>
          <div class="stat-label">审批通过率</div>
          <div class="stat-bar">
            <div class="stat-bar-fill approved" :style="{ width: approvedPct + '%' }"></div>
            <div class="stat-bar-fill denied" :style="{ width: deniedPct + '%' }"></div>
          </div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ avgResponse }}</div>
          <div class="stat-label">平均响应时间</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ store.stats.sessions.length }}</div>
          <div class="stat-label">活跃会话</div>
        </div>
      </div>

      <!-- Tool Usage Chart -->
      <div class="tool-chart" v-if="store.stats.tool_usage.length > 0">
        <h4 class="section-title">工具使用频率</h4>
        <div class="bar-chart">
          <div v-for="item in store.stats.tool_usage" :key="item.tool" class="bar-row">
            <span class="bar-label">{{ item.tool }}</span>
            <div class="bar-track">
              <div class="bar-fill" :style="{ width: barWidth(item.count) + '%' }"></div>
            </div>
            <span class="bar-value">{{ item.count }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Sessions & Timeline -->
    <div class="home-card" v-if="store.stats.sessions.length > 0">
      <h3 class="card-title">会话管理</h3>
      <div class="session-layout">
        <div class="session-list">
          <div v-for="s in store.stats.sessions" :key="s.session_id"
            :class="['session-item', { active: store.activeSessionId === s.session_id }]"
            @click="store.loadTimeline(s.session_id)">
            <div class="session-id">{{ shortId(s.session_id) }}</div>
            <div class="session-meta">
              <span class="session-badge">{{ s.count }}</span>
              <span class="session-time">{{ timeAgo(s.last_seen) }}</span>
            </div>
          </div>
        </div>
        <div class="session-detail" v-if="store.timeline.length > 0">
          <h4 class="section-title">工具调用时间线</h4>
          <div class="timeline">
            <div v-for="item in store.timeline" :key="item.id" class="timeline-item">
              <div class="timeline-dot"></div>
              <div class="timeline-content">
                <div class="timeline-header">
                  <span class="tool-badge">{{ item.tool_name || item.hook_event }}</span>
                  <span :class="['decision-badge', item.status]">{{ item.status }}</span>
                  <span class="timeline-time" v-if="item.duration_ms">{{ formatMs(item.duration_ms) }}</span>
                </div>
                <div class="timeline-summary" v-if="item.tool_input_summary">{{ truncate(item.tool_input_summary, 120) }}</div>
                <div class="timeline-ts">{{ formatTime(item.created_at) }}</div>
              </div>
            </div>
          </div>
          <!-- Transcript replay button -->
          <t-button theme="default" variant="outline" class="transcript-btn" v-if="firstTranscriptPath"
            @click="doLoadTranscript" :loading="transcriptLoading">
            查看完整对话
          </t-button>
        </div>
      </div>
    </div>

    <div class="home-empty" v-if="!store.stats.total && !store.loading">
      暂无通知数据，启动 Claude Code 并配置 Hook 后开始记录。
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useHome } from '../stores/home'
import { MessagePlugin } from 'tdesign-vue-next'
import { renderMd } from '../markdown'

const store = useHome()
const showHttpDialog = ref(false)
const showTranscriptDialog = ref(false)
const transcriptLoading = ref(false)

onMounted(async () => {
  await Promise.allSettled([
    store.loadToggles().catch((e: any) => console.error('[notify-me] loadToggles:', e)),
    store.loadHookStatus().catch((e: any) => console.error('[notify-me] loadHookStatus:', e)),
    store.loadStats().catch((e: any) => console.error('[notify-me] loadStats:', e)),
  ])
  store.startAutoRefresh(5000)
})
onUnmounted(() => { store.stopAutoRefresh() })

async function doConfigureHooks(mode: string) {
  await store.configureHooks(mode)
  if (!store.lastError) {
    MessagePlugin.success('Hook 配置成功')
  }
}

async function doRemoveHooks() {
  await store.removeHooks()
  if (!store.lastError) {
    MessagePlugin.success('Hook 配置已移除')
  }
}

async function doLoadTranscript() {
  const path = firstTranscriptPath.value
  if (!path) {
    MessagePlugin.warning('没有可用的对话记录')
    return
  }
  transcriptLoading.value = true
  try {
    await store.loadTranscript(path)
    showTranscriptDialog.value = true
  } catch (e: any) {
    MessagePlugin.error('加载对话失败: ' + (e?.message || String(e)))
  } finally {
    transcriptLoading.value = false
  }
}

const soundEnabled = computed({
  get: () => store.toggles.sound_enabled,
  set: (v: boolean) => store.setSound(v),
})
const blinkEnabled = computed({
  get: () => store.toggles.blink_enabled,
  set: (v: boolean) => store.setBlink(v),
})
const stopHookEnabled = computed({
  get: () => store.toggles.stop_hook_enabled,
  set: (v: boolean) => store.setStopHook(v),
})

const d = computed(() => store.stats.decisions)
const total = computed(() => d.value.approved + d.value.denied + d.value.timeout + d.value.cancelled + d.value.other)
const approvalRate = computed(() => total.value > 0 ? Math.round(d.value.approved / total.value * 100) : 0)
const approvedPct = computed(() => total.value > 0 ? d.value.approved / total.value * 100 : 0)
const deniedPct = computed(() => total.value > 0 ? d.value.denied / total.value * 100 : 0)
const avgResponse = computed(() => {
  const ms = store.stats.avg_response_ms
  if (!ms) return '-'
  return ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : Math.round(ms) + 'ms'
})

const hookEventNames: Record<string, string> = {
  PreToolUse: '工具调用',
  PermissionRequest: '权限请求',
  Notification: '空闲通知',
  Stop: '任务完成',
  StopFailure: '任务异常',
}
const hookModeLabels: Record<string, string> = { stdio: '本机', http: 'HTTP' }
const hookModeLabel = computed(() => hookModeLabels[store.hookStatus?.mode ?? ''] || store.hookStatus?.mode || '')

const httpHooksConfig = computed(() => {
  return `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:1886/api/claude/hook", "timeout": 200 }]
      },
      {
        "matcher": "Edit|Write",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:1886/api/claude/hook", "timeout": 200 }]
      }
    ],
    "PermissionRequest": [
      {
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:1886/api/claude/hook", "timeout": 200 }]
      }
    ],
    "Notification": [
      {
        "matcher": "idle_prompt",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:1886/api/claude/hook", "timeout": 10 }]
      }
    ],
    "Stop": [
      {
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:1886/api/claude/hook", "timeout": 10 }]
      }
    ],
    "StopFailure": [
      {
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:1886/api/claude/hook", "timeout": 10 }]
      }
    ]
  }
}`
})

function copyHttpConfig() {
  navigator.clipboard.writeText(httpHooksConfig.value).then(() => {
    MessagePlugin.success('已复制到剪贴板')
  }).catch(() => {
    MessagePlugin.error('复制失败，请手动选择复制')
  })
}

const blockingEvents = new Set(['PreToolUse', 'PermissionRequest'])

function eventName(evt: string) { return hookEventNames[evt] || evt }
function isBlocking(evt: string) { return blockingEvents.has(evt) }

function barWidth(count: number) {
  const max = store.stats.tool_usage[0]?.count || 1
  return Math.max(5, count / max * 100)
}

function shortId(id: string) { return id.length > 12 ? id.slice(0, 8) + '…' + id.slice(-4) : id }
function truncate(s: string, max: number) { return s.length > max ? s.slice(0, max - 3) + '…' : s }
function timeAgo(ms: number) {
  const sec = Math.floor((Date.now() - ms) / 1000)
  if (sec < 60) return sec + '秒前'
  if (sec < 3600) return Math.floor(sec / 60) + '分前'
  if (sec < 86400) return Math.floor(sec / 3600) + '小时前'
  return Math.floor(sec / 86400) + '天前'
}
function formatMs(ms: number) { return ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : ms + 'ms' }
function formatTime(ms: number) { return new Date(ms).toLocaleTimeString() }

function renderMsgContent(msg: any): string {
  if (msg.tool_name && !msg.content) {
    return `<span class="tool-tag">🔧 ${msg.tool_name}</span>`
  }
  if (!msg.content) return ''
  // Truncate long content for display
  const maxLen = 2000
  const content = msg.content.length > maxLen ? msg.content.slice(0, maxLen - 3) + '...' : msg.content
  return renderMd(content)
}

const firstTranscriptPath = computed(() => {
  return store.timeline.find((r: any) => r.transcript_path)?.transcript_path || ''
})
</script>
