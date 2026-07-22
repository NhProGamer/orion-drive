<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Server, Copy, Check, Trash2, Plus, KeyRound, TriangleAlert } from 'lucide-vue-next'
import { api, type WebdavAccount } from '@/lib/api'
import { useUiStore } from '@/stores/ui'

const emit = defineEmits<{ close: [] }>()
const ui = useUiStore()
const { t } = useI18n()

const accounts = ref<WebdavAccount[]>([])
const url = ref('')
const loading = ref(true)

// Creation form.
const label = ref('')
const readOnly = ref(false)
const creating = ref(false)
// The one-time credential just created (username + clear password).
const created = ref<{ username: string; password: string } | null>(null)

const copied = ref('')

async function load() {
  loading.value = true
  try {
    const res = await api.webdavAccounts()
    accounts.value = res.accounts
    url.value = res.url
  } catch (e: any) {
    ui.toast(t('webdav.loadError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    loading.value = false
  }
}

async function create() {
  creating.value = true
  try {
    const res = await api.createWebdavAccount({ label: label.value || undefined, read_only: readOnly.value })
    created.value = { username: res.account.username, password: res.password }
    label.value = ''
    readOnly.value = false
    await load()
  } catch (e: any) {
    ui.toast(t('webdav.createError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    creating.value = false
  }
}

async function revoke(a: WebdavAccount) {
  if (!confirm(t('webdav.revokeConfirm', { label: a.label, username: a.username }))) return
  try {
    await api.deleteWebdavAccount(a.id)
    if (created.value?.username === a.username) created.value = null
    await load()
  } catch (e: any) {
    ui.toast(t('webdav.revokeError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  }
}

async function copy(text: string, key: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = key
    setTimeout(() => (copied.value = ''), 1500)
  } catch {}
}

function fmtDate(s: string | null): string {
  if (!s) return t('webdav.never')
  return new Date(s).toLocaleString('fr-FR', { dateStyle: 'short', timeStyle: 'short' })
}

onMounted(load)
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog" style="max-width: 560px; width: 92vw">
      <h2><Server :size="18" />{{ t('webdav.title') }}</h2>
      <p style="color: var(--fg-2); font-size: 12.5px; margin: -4px 0 4px">
        {{ t('webdav.intro') }}
      </p>

      <!-- Connection URL -->
      <label style="display: flex; flex-direction: column; gap: 4px">
        <span class="tweak-label" style="letter-spacing: 0.06em">{{ t('webdav.serverAddress') }}</span>
        <div style="display: flex; gap: 8px; align-items: center">
          <input class="input mono" :value="url" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button class="icon-btn" :title="copied === 'url' ? t('common.copied') : t('common.copy')" @click="copy(url, 'url')">
            <component :is="copied === 'url' ? Check : Copy" :size="16" />
          </button>
        </div>
      </label>

      <!-- Just-created credential (shown once) -->
      <div v-if="created" class="dav-created">
        <div class="dav-created-head"><KeyRound :size="15" />{{ t('webdav.newCredential') }}</div>
        <div class="dav-cred-row">
          <span class="tweak-label">{{ t('webdav.user') }}</span>
          <input class="input mono" :value="created.username" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button class="icon-btn" :title="copied === 'u' ? t('common.copied') : t('common.copy')" @click="copy(created.username, 'u')">
            <component :is="copied === 'u' ? Check : Copy" :size="15" />
          </button>
        </div>
        <div class="dav-cred-row">
          <span class="tweak-label">{{ t('webdav.password') }}</span>
          <input class="input mono" :value="created.password" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button class="icon-btn" :title="copied === 'p' ? t('common.copied') : t('common.copy')" @click="copy(created.password, 'p')">
            <component :is="copied === 'p' ? Check : Copy" :size="15" />
          </button>
        </div>
        <div class="dav-warn"><TriangleAlert :size="13" />{{ t('webdav.passwordOnce') }}</div>
      </div>

      <!-- Existing accounts -->
      <div class="tweak-label" style="letter-spacing: 0.06em; margin-top: 4px">{{ t('webdav.existingCredentials') }}</div>
      <div class="dav-list">
        <div v-if="loading" class="dav-empty">{{ t('common.loading') }}</div>
        <div v-else-if="!accounts.length" class="dav-empty">{{ t('webdav.empty') }}</div>
        <div v-for="a in accounts" :key="a.id" class="dav-row">
          <div class="dav-meta">
            <span class="dav-name">{{ a.label }} <span v-if="a.read_only" class="dav-badge">{{ t('webdav.readOnly') }}</span></span>
            <span class="mono dav-user">{{ a.username }}</span>
            <span class="dav-used">{{ t('webdav.lastUsed', { date: fmtDate(a.last_used_at) }) }}</span>
          </div>
          <button class="icon-btn" :title="t('webdav.revoke')" @click="revoke(a)"><Trash2 :size="15" /></button>
        </div>
      </div>

      <!-- Create form -->
      <div class="dav-create">
        <input v-model="label" class="input" type="text" :placeholder="t('webdav.labelPlaceholder')" @keyup.enter="create" />
        <label class="dav-ro"><input v-model="readOnly" type="checkbox" />{{ t('webdav.readOnly') }}</label>
        <button class="btn btn-primary" :disabled="creating" @click="create">
          <Plus :size="15" />{{ creating ? t('webdav.creating') : t('common.create') }}
        </button>
      </div>

      <div class="dialog-actions">
        <button class="btn btn-primary" @click="emit('close')">{{ t('webdav.done') }}</button>
      </div>
    </div>
  </div>
</template>
