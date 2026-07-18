<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Link2, Copy, Check, ExternalLink } from 'lucide-vue-next'
import type { FileNode, SharePermission } from '@/lib/api'
import { api } from '@/lib/api'
import { useUiStore } from '@/stores/ui'

const { t } = useI18n()

const props = defineProps<{ node: FileNode }>()
const emit = defineEmits<{ close: [] }>()

const ui = useUiStore()
const isFolder = props.node.type === 'folder'
const permission = ref<SharePermission>('read')
const password = ref('')
const expiresDays = ref<number | ''>('')
const maxDownloads = ref<number | ''>('')
const loading = ref(false)
const result = ref<{ url: string } | null>(null)
const copied = ref(false)

// Write/deposit only apply to folders; a file share is always read-only.
const permOptions: { value: SharePermission; label: string; hint: string }[] = [
  { value: 'read', label: t('shareDialog.permRead'), hint: t('shareDialog.permReadHint') },
  { value: 'write', label: t('shareDialog.permWrite'), hint: t('shareDialog.permWriteHint') },
  { value: 'deposit', label: t('shareDialog.permDeposit'), hint: t('shareDialog.permDepositHint') },
]

async function create() {
  loading.value = true
  try {
    result.value = await api.createShare({
      file_id: props.node.id,
      permission: isFolder ? permission.value : 'read',
      password: password.value || undefined,
      expires_days: expiresDays.value ? Number(expiresDays.value) : undefined,
      max_downloads: maxDownloads.value ? Number(maxDownloads.value) : undefined,
    })
  } catch (e: any) {
    ui.toast(t('shareDialog.createError') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    loading.value = false
  }
}

async function copy() {
  if (!result.value) return
  try {
    await navigator.clipboard.writeText(result.value.url)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {}
}
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog">
      <h2><Link2 :size="18" style="vertical-align: -3px; margin-right: 6px" />{{ t('shareDialog.title', { name: node.name }) }}</h2>

      <template v-if="!result">
        <div style="display: flex; flex-direction: column; gap: 12px">
          <div v-if="isFolder" style="display: flex; flex-direction: column; gap: 6px">
            <span class="tweak-label" style="letter-spacing: 0.06em">{{ t('shareDialog.permLabel') }}</span>
            <div class="seg">
              <button
                v-for="o in permOptions"
                :key="o.value"
                type="button"
                class="seg-btn"
                :class="{ active: permission === o.value }"
                @click="permission = o.value"
              >
                {{ o.label }}
              </button>
            </div>
            <span style="font-size: 12px; color: var(--fg-2)">{{ permOptions.find((o) => o.value === permission)?.hint }}</span>
          </div>
          <label style="display: flex; flex-direction: column; gap: 4px">
            <span class="tweak-label" style="letter-spacing: 0.06em">{{ t('shareDialog.passwordLabel') }}</span>
            <input v-model="password" class="input" type="text" :placeholder="t('shareDialog.passwordPlaceholder')" />
          </label>
          <div style="display: flex; gap: 12px">
            <label style="flex: 1; display: flex; flex-direction: column; gap: 4px">
              <span class="tweak-label" style="letter-spacing: 0.06em">{{ t('shareDialog.expiresLabel') }}</span>
              <input v-model="expiresDays" class="input" type="number" min="1" :placeholder="t('shareDialog.expiresPlaceholder')" />
            </label>
            <label style="flex: 1; display: flex; flex-direction: column; gap: 4px">
              <span class="tweak-label" style="letter-spacing: 0.06em">{{ t('shareDialog.maxDownloadsLabel') }}</span>
              <input v-model="maxDownloads" class="input" type="number" min="1" :placeholder="t('shareDialog.maxDownloadsPlaceholder')" />
            </label>
          </div>
        </div>
        <div class="dialog-actions">
          <button class="btn btn-ghost" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="loading" @click="create">
            {{ loading ? t('shareDialog.creating') : t('shareDialog.createLink') }}
          </button>
        </div>
      </template>

      <template v-else>
        <p>{{ t('shareDialog.created') }}</p>
        <div style="display: flex; gap: 8px; align-items: center">
          <input class="input" :value="result.url" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button class="icon-btn" :title="copied ? t('common.copied') : t('common.copy')" @click="copy">
            <component :is="copied ? Check : Copy" :size="16" />
          </button>
        </div>
        <div class="dialog-actions">
          <a class="btn btn-secondary" :href="result.url" target="_blank" rel="noopener">
            <ExternalLink :size="15" />{{ t('common.open') }}
          </a>
          <button class="btn btn-primary" @click="emit('close')">{{ t('shareDialog.done') }}</button>
        </div>
      </template>
    </div>
  </div>
</template>
