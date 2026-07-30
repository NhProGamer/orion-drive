<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Server, Cloud, Terminal, HardDrive, Copy, Check, Trash2, Plus, KeyRound, TriangleAlert } from 'lucide-vue-next'
import { api, type WebdavAccount } from '@/lib/api'
import { fmtDateTime } from '@/lib/format'
import { useUiStore } from '@/stores/ui'
import AccessDialog from './AccessDialog.vue'
import AccessSwitch from './AccessSwitch.vue'

const emit = defineEmits<{ close: [] }>()
const ui = useUiStore()
const { t } = useI18n()

const accounts = ref<WebdavAccount[]>([])
const url = ref('')
const sftp = ref<{ enabled: boolean; host: string; port: number } | null>(null)
const loading = ref(true)

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
    sftp.value = res.sftp
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

function selectAll(e: FocusEvent) {
  ;(e.target as HTMLInputElement).select()
}

function fmtDate(s: string | null): string {
  return s ? fmtDateTime(s) : t('webdav.never')
}

onMounted(load)
</script>

<template>
  <AccessDialog :title="t('webdav.title')" :subtitle="t('webdav.subtitle')" @close="emit('close')">
    <template #icon><Server :size="19" /></template>

    <!-- Connection endpoints -->
    <div class="acc-section">
      <p class="acc-label">{{ t('webdav.secConnection') }}</p>
      <div class="acc-endpoints">
        <div class="acc-endpoint">
          <span class="acc-ep-ico"><Cloud :size="16" /></span>
          <div class="acc-ep-body">
            <div class="acc-ep-k">WebDAV</div>
            <div class="acc-ep-v">{{ url }}</div>
          </div>
          <button class="icon-btn" :title="copied === 'url' ? t('common.copied') : t('common.copy')" @click="copy(url, 'url')">
            <component :is="copied === 'url' ? Check : Copy" :size="15" />
          </button>
        </div>
        <div v-if="sftp?.enabled" class="acc-endpoint">
          <span class="acc-ep-ico"><Terminal :size="16" /></span>
          <div class="acc-ep-body">
            <div class="acc-ep-k">SFTP</div>
            <div class="acc-ep-v">sftp://{{ sftp.host }}:{{ sftp.port }}</div>
          </div>
          <button
            class="icon-btn"
            :title="copied === 'sftp' ? t('common.copied') : t('common.copy')"
            @click="copy(`sftp://${sftp.host}:${sftp.port}`, 'sftp')"
          >
            <component :is="copied === 'sftp' ? Check : Copy" :size="15" />
          </button>
        </div>
      </div>
    </div>

    <!-- One-time credential (shown once) -->
    <div v-if="created" class="acc-section">
      <div class="acc-reveal">
        <div class="acc-reveal-head"><KeyRound :size="15" />{{ t('webdav.newCredential') }}</div>
        <div class="acc-secret">
          <input class="input mono" :value="created.username" readonly @focus="selectAll" />
          <button class="icon-btn" :title="copied === 'u' ? t('common.copied') : t('common.copy')" @click="copy(created.username, 'u')">
            <component :is="copied === 'u' ? Check : Copy" :size="15" />
          </button>
        </div>
        <div class="acc-secret">
          <input class="input mono" :value="created.password" readonly @focus="selectAll" />
          <button class="icon-btn" :title="copied === 'p' ? t('common.copied') : t('common.copy')" @click="copy(created.password, 'p')">
            <component :is="copied === 'p' ? Check : Copy" :size="15" />
          </button>
        </div>
        <div class="acc-warn"><TriangleAlert :size="13" />{{ t('webdav.passwordOnce') }}</div>
      </div>
    </div>

    <!-- Existing credentials -->
    <div class="acc-section">
      <p class="acc-label">{{ t('webdav.secActive') }}</p>
      <div v-if="loading" class="acc-empty">{{ t('common.loading') }}</div>
      <div v-else-if="!accounts.length" class="acc-empty">{{ t('webdav.empty') }}</div>
      <div v-else class="acc-list">
        <div v-for="a in accounts" :key="a.id" class="acc-cred">
          <span class="acc-dot"><HardDrive :size="16" /></span>
          <div class="acc-cred-body">
            <div class="acc-cred-top">
              <span class="acc-cred-name">{{ a.label }}</span>
              <span v-if="a.read_only" class="acc-pill">{{ t('webdav.readOnly') }}</span>
            </div>
            <div class="acc-cred-sub">
              <span class="mono">{{ a.username }}</span>
              <span class="sep">·</span>
              <span>{{ t('webdav.lastUsed', { date: fmtDate(a.last_used_at) }) }}</span>
            </div>
          </div>
          <button class="acc-revoke" :title="t('webdav.revoke')" @click="revoke(a)"><Trash2 :size="16" /></button>
        </div>
      </div>
    </div>

    <!-- Create -->
    <div class="acc-section">
      <p class="acc-label">{{ t('webdav.secNew') }}</p>
      <div class="acc-create">
        <div class="acc-field">
          <label for="w-name">{{ t('webdav.nameLabel') }}</label>
          <input id="w-name" v-model="label" class="input" type="text" :placeholder="t('webdav.labelPlaceholder')" @keyup.enter="create" />
        </div>
        <div class="acc-create-foot">
          <AccessSwitch v-model="readOnly" :label="t('webdav.readOnly')" />
          <button class="btn btn-primary" :disabled="creating" @click="create">
            <Plus :size="15" />{{ creating ? t('webdav.creating') : t('common.create') }}
          </button>
        </div>
      </div>
    </div>
  </AccessDialog>
</template>
