<template>
  <div class="audit">
    <div class="toggle-error" v-if="store.error">{{ store.error }}</div>

    <!-- Global Rules Card -->
    <div class="audit-card">
      <div class="audit-header">
        <h3 class="card-title" style="margin:0">策略引擎模版</h3>
        <button class="btn btn-primary" @click="openAddDialog">+ 新增规则</button>
      </div>
      <table class="audit-table" v-if="store.globalRules.length > 0">
        <thead>
          <tr>
            <th>启用</th>
            <th>工具名</th>
            <th>命令模式</th>
            <th>优先级</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="rule in store.globalRules" :key="rule.id">
            <td><t-switch v-model="rule.enabled" @change="toggleRule(rule)" size="small" /></td>
            <td><code>{{ rule.tool_name || '*' }}</code></td>
            <td><code>{{ rule.pattern || '*' }}</code></td>
            <td>{{ rule.priority }}</td>
            <td>
              <button class="btn btn-secondary" @click="openEditDialog(rule)">编辑</button>
              <button class="btn btn-danger" style="margin-left:6px" @click="doRemove(rule.id)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="audit-empty" v-else>暂无全局策略规则</div>
    </div>

    <!-- Session Rules Card -->
    <div class="audit-card">
      <div class="audit-header">
        <h3 class="card-title" style="margin:0">会话免审批列表</h3>
        <button class="btn btn-secondary" @click="doCleanup" :disabled="store.sessionRules.length === 0">清理已结束会话</button>
      </div>
      <table class="audit-table" v-if="store.sessionRules.length > 0">
        <thead>
          <tr>
            <th>会话ID</th>
            <th>工具名</th>
            <th>命令/路径模式</th>
            <th>来源</th>
            <th>添加时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="rule in store.sessionRules" :key="rule.id">
            <td><code :title="rule.session_id">{{ shortId(rule.session_id) }}</code></td>
            <td><code>{{ rule.tool_name || '*' }}</code></td>
            <td><code>{{ rule.pattern || '*' }}</code></td>
            <td>{{ rule.source }}</td>
            <td>{{ formatTime(rule.created_at) }}</td>
            <td>
              <button class="btn btn-danger" @click="doRemove(rule.id)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="audit-empty" v-else>暂无会话免审批规则</div>
    </div>

    <!-- Rule Editor Dialog -->
    <div class="rule-dialog-overlay" v-if="showDialog" @click.self="showDialog = false">
      <div class="rule-dialog">
        <h4>{{ editingRule ? '编辑规则' : '新增规则' }}</h4>
        <label>工具名</label>
        <input v-model="form.tool_name" placeholder="e.g. Bash 或 * 表示所有" />
        <label>命令模式</label>
        <input v-model="form.pattern" placeholder="正则表达式，e.g. rm\\s+-rf" />
        <label>优先级</label>
        <input v-model.number="form.priority" type="number" min="0" />
        <div class="rule-dialog-actions">
          <button class="btn btn-secondary" @click="showDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveRule">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, reactive } from 'vue'
import { useAudit, type PolicyRule } from '../stores/audit'

const store = useAudit()
const showDialog = ref(false)
const editingRule = ref<PolicyRule | null>(null)
const form = reactive({ tool_name: '', pattern: '', priority: 0 })

onMounted(() => { store.load() })

function shortId(id: string) {
  return id.length > 12 ? id.slice(0, 8) + '...' + id.slice(-4) : id
}

function formatTime(ms: number) {
  return new Date(ms).toLocaleString()
}

function openAddDialog() {
  editingRule.value = null
  form.tool_name = ''
  form.pattern = ''
  form.priority = 0
  showDialog.value = true
}

function openEditDialog(rule: PolicyRule) {
  editingRule.value = rule
  form.tool_name = rule.tool_name
  form.pattern = rule.pattern
  form.priority = rule.priority
  showDialog.value = true
}

async function saveRule() {
  if (editingRule.value) {
    await store.update({
      ...editingRule.value,
      tool_name: form.tool_name,
      pattern: form.pattern,
      priority: form.priority,
    })
  } else {
    await store.add({
      type: 'global',
      session_id: '',
      tool_name: form.tool_name,
      pattern: form.pattern,
      enabled: true,
      priority: form.priority,
      source: 'manual',
    })
  }
  if (!store.error) {
    showDialog.value = false
  }
}

async function toggleRule(rule: PolicyRule) {
  await store.update(rule)
}

async function doRemove(id: number) {
  await store.remove(id)
}

async function doCleanup() {
  await store.cleanup()
}
</script>
