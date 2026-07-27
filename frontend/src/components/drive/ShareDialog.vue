<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Link2, Copy, Check, ExternalLink } from 'lucide-vue-next'
import type { FileNode, SharePermission } from '@/lib/api'
import { api } from '@/lib/api'
import { useUiStore } from '@/stores/ui'
import AccessDialog from './AccessDialog.vue'

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

// Read and write apply to both files (write = Office editing) and folders;
// deposit (blind drop box) is folder-only.
const permOptions = [
  { value: 'read' as SharePermission, label: t('shareDialog.permRead'), hint: t('shareDialog.permReadHint') },
  { value: 'write' as SharePermission, label: t('shareDialog.permWrite'), hint: isFolder ? t('shareDialog.permWriteHint') : t('shareDialog.permWriteFileHint') },
  ...(isFolder
    ? [{ value: 'deposit' as SharePermission, label: t('shareDialog.permDeposit'), hint: t('shareDialog.permDepositHint') }]
    : []),
]

async function create() {
  loading.value = true
  try {
    result.value = await api.createShare({
      file_id: props.node.id,
      permission: permission.value,
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

function selectAll(e: FocusEvent) {
  ;(e.target as HTMLInputElement).select()
}
</script>

<template>
  <AccessDialog :title="t('shareDialog.title', { name: node.name })" @close="emit('close')">
    <template #icon><Link2 :size="19" /></template>

    <!-- Configure -->
    <div v-if="!result" class="acc-form">
      <div class="acc-field">
        <label>{{ t('shareDialog.permLabel') }}</label>
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
        <p class="acc-hint">{{ permOptions.find((o) => o.value === permission)?.hint }}</p>
      </div>

      <div class="acc-field">
        <label>{{ t('shareDialog.passwordLabel') }}</label>
        <input v-model="password" class="input" type="text" :placeholder="t('shareDialog.passwordPlaceholder')" />
      </div>

      <div class="acc-row">
        <div class="acc-field">
          <label>{{ t('shareDialog.expiresLabel') }}</label>
          <input v-model="expiresDays" class="input" type="number" min="1" :placeholder="t('shareDialog.expiresPlaceholder')" />
        </div>
        <div class="acc-field">
          <label>{{ t('shareDialog.maxDownloadsLabel') }}</label>
          <input v-model="maxDownloads" class="input" type="number" min="1" :placeholder="t('shareDialog.maxDownloadsPlaceholder')" />
        </div>
      </div>
    </div>

    <!-- Created -->
    <div v-else class="acc-form">
      <div class="acc-field">
        <label>{{ t('shareDialog.created') }}</label>
        <div class="acc-secret">
          <input class="input mono" :value="result.url" readonly @focus="selectAll" />
          <button class="icon-btn" :title="copied ? t('common.copied') : t('common.copy')" @click="copy">
            <component :is="copied ? Check : Copy" :size="15" />
          </button>
        </div>
      </div>
    </div>

    <template #footer>
      <template v-if="!result">
        <button class="btn btn-ghost" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="loading" @click="create">
          {{ loading ? t('shareDialog.creating') : t('shareDialog.createLink') }}
        </button>
      </template>
      <template v-else>
        <a class="btn btn-secondary" :href="result.url" target="_blank" rel="noopener">
          <ExternalLink :size="15" />{{ t('common.open') }}
        </a>
        <button class="btn btn-primary" @click="emit('close')">{{ t('shareDialog.done') }}</button>
      </template>
    </template>
  </AccessDialog>
</template>
