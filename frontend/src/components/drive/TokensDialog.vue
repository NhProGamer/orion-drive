<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { KeyRound, Copy, Check, Trash2, Plus, TriangleAlert } from 'lucide-vue-next'
import { api, type ApiTokenInfo } from '@/lib/api'
import { useUiStore } from '@/stores/ui'

const emit = defineEmits<{ close: [] }>()
const ui = useUiStore()
const { t } = useI18n()

const tokens = ref<ApiTokenInfo[]>([])
const loading = ref(true)

const label = ref('')
const readOnly = ref(false)
const expiresDays = ref<number | ''>('')
const creating = ref(false)
// The plaintext token just created (shown once).
const created = ref<string | null>(null)
const copied = ref(false)

async function load() {
  loading.value = true
  try {
    tokens.value = (await api.apiTokens()).tokens
  } catch (e: any) {
    ui.toast(t('tokens.loadError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    loading.value = false
  }
}

async function create() {
  creating.value = true
  try {
    const res = await api.createApiToken({
      label: label.value || undefined,
      read_only: readOnly.value,
      expires_days: expiresDays.value ? Number(expiresDays.value) : undefined,
    })
    created.value = res.token
    label.value = ''
    readOnly.value = false
    expiresDays.value = ''
    await load()
  } catch (e: any) {
    ui.toast(t('tokens.createError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    creating.value = false
  }
}

async function revoke(tk: ApiTokenInfo) {
  if (!confirm(t('tokens.revokeConfirm', { label: tk.label }))) return
  try {
    await api.deleteApiToken(tk.id)
    await load()
  } catch (e: any) {
    ui.toast(t('tokens.revokeError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {}
}

function fmtDate(s: string | null): string {
  if (!s) return t('tokens.never')
  return new Date(s).toLocaleString('fr-FR', { dateStyle: 'short', timeStyle: 'short' })
}

onMounted(load)
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog" style="max-width: 560px; width: 92vw">
      <h2><KeyRound :size="18" style="vertical-align: -3px; margin-right: 6px" />{{ t('tokens.title') }}</h2>
      <p style="color: var(--fg-2); font-size: 12.5px; margin: -4px 0 4px">{{ t('tokens.intro') }}</p>

      <!-- Just-created token (shown once) -->
      <div v-if="created" class="dav-created">
        <div class="dav-created-head"><KeyRound :size="15" />{{ t('tokens.newToken') }}</div>
        <div class="dav-cred-row">
          <input class="input mono" :value="created" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button class="icon-btn" :title="copied ? t('common.copied') : t('common.copy')" @click="copy(created!)">
            <component :is="copied ? Check : Copy" :size="15" />
          </button>
        </div>
        <div class="dav-warn"><TriangleAlert :size="13" />{{ t('tokens.tokenOnce') }}</div>
      </div>

      <!-- Existing tokens -->
      <div class="tweak-label" style="letter-spacing: 0.06em; margin-top: 4px">{{ t('tokens.existing') }}</div>
      <div class="dav-list">
        <div v-if="loading" class="dav-empty">{{ t('common.loading') }}</div>
        <div v-else-if="!tokens.length" class="dav-empty">{{ t('tokens.empty') }}</div>
        <div v-for="tk in tokens" :key="tk.id" class="dav-row">
          <div class="dav-meta">
            <span class="dav-name">
              {{ tk.label }}
              <span v-if="tk.read_only" class="dav-badge">{{ t('tokens.readOnly') }}</span>
            </span>
            <span class="mono dav-user">{{ tk.prefix }}…</span>
            <span class="dav-used">
              {{ t('tokens.lastUsed', { date: fmtDate(tk.last_used_at) }) }}
              <template v-if="tk.expires_at"> · {{ t('tokens.expires', { date: fmtDate(tk.expires_at) }) }}</template>
            </span>
          </div>
          <button class="icon-btn" :title="t('tokens.revoke')" @click="revoke(tk)"><Trash2 :size="15" /></button>
        </div>
      </div>

      <!-- Create form -->
      <div class="dav-create">
        <input v-model="label" class="input" type="text" :placeholder="t('tokens.labelPlaceholder')" @keyup.enter="create" />
        <input v-model="expiresDays" class="input" type="number" min="1" style="max-width: 110px" :placeholder="t('tokens.expiresDays')" />
        <label class="dav-ro"><input v-model="readOnly" type="checkbox" />{{ t('tokens.readOnly') }}</label>
        <button class="btn btn-primary" :disabled="creating" @click="create">
          <Plus :size="15" />{{ creating ? t('tokens.creating') : t('common.create') }}
        </button>
      </div>

      <div class="dialog-actions">
        <button class="btn btn-primary" @click="emit('close')">{{ t('tokens.done') }}</button>
      </div>
    </div>
  </div>
</template>
