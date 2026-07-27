<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { KeyRound, Copy, Check, Trash2, Plus, TriangleAlert } from 'lucide-vue-next'
import { api, type ApiTokenInfo } from '@/lib/api'
import { useUiStore } from '@/stores/ui'
import AccessDialog from './AccessDialog.vue'
import AccessSwitch from './AccessSwitch.vue'

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

function selectAll(e: FocusEvent) {
  ;(e.target as HTMLInputElement).select()
}

function fmtDate(s: string | null): string {
  if (!s) return t('tokens.never')
  return new Date(s).toLocaleString('fr-FR', { dateStyle: 'short', timeStyle: 'short' })
}

onMounted(load)
</script>

<template>
  <AccessDialog :title="t('tokens.title')" :subtitle="t('tokens.subtitle')" @close="emit('close')">
    <template #icon><KeyRound :size="19" /></template>

    <!-- One-time token (shown once) -->
    <div v-if="created" class="acc-section">
      <div class="acc-reveal">
        <div class="acc-reveal-head"><KeyRound :size="15" />{{ t('tokens.newToken') }}</div>
        <div class="acc-secret">
          <input class="input mono" :value="created" readonly @focus="selectAll" />
          <button class="icon-btn" :title="copied ? t('common.copied') : t('common.copy')" @click="copy(created!)">
            <component :is="copied ? Check : Copy" :size="15" />
          </button>
        </div>
        <div class="acc-warn"><TriangleAlert :size="13" />{{ t('tokens.tokenOnce') }}</div>
      </div>
    </div>

    <!-- Existing tokens -->
    <div class="acc-section">
      <p class="acc-label">{{ t('tokens.secActive') }}</p>
      <div v-if="loading" class="acc-empty">{{ t('common.loading') }}</div>
      <div v-else-if="!tokens.length" class="acc-empty">{{ t('tokens.empty') }}</div>
      <div v-else class="acc-list">
        <div v-for="tk in tokens" :key="tk.id" class="acc-cred">
          <span class="acc-dot"><KeyRound :size="15" /></span>
          <div class="acc-cred-body">
            <div class="acc-cred-top">
              <span class="acc-cred-name">{{ tk.label }}</span>
              <span v-if="tk.read_only" class="acc-pill">{{ t('tokens.readOnly') }}</span>
            </div>
            <div class="acc-cred-sub">
              <span class="mono">{{ tk.prefix }}…</span>
              <span class="sep">·</span>
              <span>{{ t('tokens.lastUsed', { date: fmtDate(tk.last_used_at) }) }}</span>
              <template v-if="tk.expires_at">
                <span class="sep">·</span>
                <span>{{ t('tokens.expires', { date: fmtDate(tk.expires_at) }) }}</span>
              </template>
            </div>
          </div>
          <button class="acc-revoke" :title="t('tokens.revoke')" @click="revoke(tk)"><Trash2 :size="16" /></button>
        </div>
      </div>
    </div>

    <!-- Create -->
    <div class="acc-section">
      <p class="acc-label">{{ t('tokens.secNew') }}</p>
      <div class="acc-create">
        <div class="acc-field">
          <label for="t-name">{{ t('tokens.nameLabel') }}</label>
          <input id="t-name" v-model="label" class="input" type="text" :placeholder="t('tokens.labelPlaceholder')" @keyup.enter="create" />
        </div>
        <div class="acc-opt-row">
          <div class="acc-field short">
            <label for="t-exp">{{ t('tokens.expiryLabel') }}</label>
            <input id="t-exp" v-model="expiresDays" class="input" type="number" min="1" :placeholder="t('tokens.expiresDays')" />
          </div>
          <AccessSwitch v-model="readOnly" :label="t('tokens.readOnly')" style="padding-bottom: 9px" />
        </div>
        <div class="acc-create-foot">
          <span class="acc-hint">{{ t('tokens.noExpiry') }}</span>
          <button class="btn btn-primary" :disabled="creating" @click="create">
            <Plus :size="15" />{{ creating ? t('tokens.creating') : t('common.create') }}
          </button>
        </div>
      </div>
    </div>
  </AccessDialog>
</template>
