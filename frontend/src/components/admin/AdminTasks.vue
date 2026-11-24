<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Trash2, Upload, HardDrive, Play, Square, Cog } from 'lucide-vue-next'
import { api, type AdminTask, type AdminTaskRun } from '@/lib/api'
import { fmtEta, fmtDateTime } from '@/lib/format'
import { currentLocale } from '@/i18n'
import { useUiStore } from '@/stores/ui'

const { t, te } = useI18n()
const ui = useUiStore()

const tasks = ref<AdminTask[]>([])
const now = ref(Date.now())
const open = ref<Record<string, boolean>>({})
const busy = ref<Record<string, boolean>>({})

const scheduled = computed(() => tasks.value.filter((tk) => tk.kind === 'scheduled'))
const once = computed(() => tasks.value.filter((tk) => tk.kind === 'once'))

const ICONS: Record<string, unknown> = {
  'trash-purge': Trash2,
  'upload-cleanup': Upload,
  'policy-deletion': HardDrive,
  'policy-delete': HardDrive,
}

// Polled rather than pushed: one admin page open at a time, and a second is
// as fresh as the progress needs to be.
let timer: ReturnType<typeof setTimeout> | undefined
let alive = true
async function refresh() {
  try {
    tasks.value = await api.adminTasks()
    now.value = Date.now()
  } catch {
    /* keep the last state; the next poll retries */
  }
  if (alive) timer = setTimeout(refresh, 1000)
}
onMounted(refresh)
onUnmounted(() => {
  alive = false
  clearTimeout(timer)
})

async function act(tk: AdminTask, action: 'run' | 'stop') {
  busy.value[tk.id] = true
  try {
    await (action === 'run' ? api.adminRunTask(tk.id) : api.adminStopTask(tk.id))
    tasks.value = await api.adminTasks()
  } catch (e: any) {
    ui.toast(e?.message || t('adminTasks.actionFailed'), 'x')
  } finally {
    busy.value[tk.id] = false
  }
}

function key(tk: AdminTask, part: string): string {
  return `adminTasks.types.${tk.type}.${part}`
}
function title(tk: AdminTask): string {
  return te(key(tk, 'title')) ? t(key(tk, 'title'), tk.params ?? {}) : tk.type
}
function desc(tk: AdminTask): string {
  if (tk.kind === 'once' && tk.params?.by) return t('adminTasks.startedBy', { by: tk.params.by })
  return te(key(tk, 'desc')) ? t(key(tk, 'desc')) : ''
}
function result(tk: AdminTask, run: AdminTaskRun): string {
  if (run.status === 'failed') return run.error || ''
  if (run.status === 'stopped') return t('adminTasks.stoppedByHand')
  return t(key(tk, 'result'), { n: run.count })
}

const rtf = computed(() => new Intl.RelativeTimeFormat(currentLocale(), { numeric: 'auto' }))
/** "2 hours ago" / "in 26 minutes", from the poll clock so it ticks. */
function relative(iso: string): string {
  const secs = Math.round((new Date(iso).getTime() - now.value) / 1000)
  const abs = Math.abs(secs)
  if (abs < 60) return rtf.value.format(secs, 'second')
  if (abs < 3600) return rtf.value.format(Math.round(secs / 60), 'minute')
  if (abs < 86400) return rtf.value.format(Math.round(secs / 3600), 'hour')
  return rtf.value.format(Math.round(secs / 86400), 'day')
}
function every(tk: AdminTask): string {
  const h = (tk.interval_seconds ?? 0) / 3600
  return h >= 1 ? t('adminTasks.everyHours', { n: h }, h) : t('adminTasks.everyMinutes', { n: Math.round(h * 60) })
}
function duration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : fmtEta(ms / 1000)
}
function pct(tk: AdminTask): number {
  return tk.total ? Math.min(100, Math.round(((tk.done ?? 0) / tk.total) * 100)) : -1
}
const BADGE: Record<string, string> = { running: 'run', done: 'ok', failed: 'danger', stopped: 'warn', idle: '' }
</script>

<template>
  <section class="admin-tasks">
    <template v-for="group in [{ id: 'scheduled', list: scheduled }, { id: 'once', list: once }]" :key="group.id">
      <div v-if="group.list.length" class="tasks-head">
        <h2 class="admin-subtitle">{{ t(`adminTasks.${group.id}`) }}</h2>
        <span class="stat-label">{{ t(`adminTasks.${group.id}Hint`) }}</span>
      </div>
      <div class="tasks-list">
        <div v-for="tk in group.list" :key="tk.id" class="task-card">
          <div class="task-ico"><component :is="ICONS[tk.type] ?? Cog" :size="17" /></div>
          <div class="task-main">
            <div class="task-name">
              {{ title(tk) }}
              <span class="badge task-badge" :class="tk.stopping ? 'warn' : BADGE[tk.status]">
                {{ tk.stopping ? t('adminTasks.status.stopping') : t(`adminTasks.status.${tk.status}`) }}
              </span>
              <span v-if="tk.params?.offline" class="badge task-badge warn">{{ t('adminTasks.metadataOnly') }}</span>
            </div>
            <div class="task-desc">{{ desc(tk) }}</div>
          </div>
          <div class="task-actions">
            <button
              v-if="tk.kind === 'scheduled'"
              class="btn btn-secondary"
              :disabled="tk.status === 'running' || busy[tk.id]"
              @click="act(tk, 'run')"
            >
              <Play :size="14" />{{ t('adminTasks.run') }}
            </button>
            <button v-if="tk.status === 'running'" class="btn btn-secondary task-stop" :disabled="busy[tk.id] || tk.stopping" @click="act(tk, 'stop')">
              <Square :size="14" />{{ t('adminTasks.stop') }}
            </button>
          </div>

          <div v-if="tk.status === 'running'" class="task-progress">
            <div class="upload-track">
              <div v-if="pct(tk) >= 0" class="upload-fill" :style="{ width: pct(tk) + '%' }"></div>
              <div v-else class="upload-fill indeterminate"></div>
            </div>
            <div class="task-progress-label">
              <span v-if="tk.total">{{ t('adminTasks.progress', { done: tk.done ?? 0, total: tk.total, pct: pct(tk) }) }}</span>
              <span v-else>{{ t('adminTasks.working') }}</span>
              <span v-if="tk.eta_seconds">{{ t('tasks.remaining', { time: fmtEta(tk.eta_seconds) }) }}</span>
            </div>
          </div>
          <div v-else-if="tk.history[0]?.status === 'failed'" class="task-error">{{ tk.history[0].error }}</div>

          <div class="task-meta">
            <span v-if="tk.kind === 'scheduled'">{{ t('adminTasks.scheduledEvery') }} <b>{{ every(tk) }}</b></span>
            <span v-if="tk.status === 'running' && tk.started_at">{{ t('adminTasks.startedAt') }} <b>{{ relative(tk.started_at) }}</b></span>
            <span v-if="tk.history[0] && tk.status !== 'running'" :title="fmtDateTime(tk.history[0].started_at)">
              {{ t('adminTasks.lastRun') }} <b>{{ relative(tk.history[0].started_at) }}</b> · {{ duration(tk.history[0].duration_ms) }}
            </span>
            <span v-if="tk.history[0] && tk.status !== 'running' && tk.history[0].status === 'done'"><b>{{ result(tk, tk.history[0]) }}</b></span>
            <span v-if="!tk.history.length && tk.status !== 'running'">{{ t('adminTasks.neverRun') }}</span>
            <span v-if="tk.kind === 'scheduled' && tk.next_run">{{ t('adminTasks.nextRun') }} <b>{{ relative(tk.next_run) }}</b></span>
            <button v-if="tk.kind === 'scheduled' && tk.history.length" class="task-toggle" @click="open[tk.id] = !open[tk.id]">
              {{ open[tk.id] ? t('adminTasks.hideHistory') : t('adminTasks.history') }}
            </button>
          </div>

          <table v-if="open[tk.id]" class="task-history">
            <tr v-for="run in tk.history" :key="run.started_at">
              <td>{{ fmtDateTime(run.started_at) }}</td>
              <td><span class="badge task-badge" :class="BADGE[run.status]">{{ t(`adminTasks.status.${run.status}`) }}</span></td>
              <td>{{ result(tk, run) }} · {{ duration(run.duration_ms) }}</td>
            </tr>
          </table>
        </div>
      </div>
    </template>
  </section>
</template>
