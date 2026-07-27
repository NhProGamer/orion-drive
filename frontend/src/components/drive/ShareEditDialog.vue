<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pencil } from 'lucide-vue-next'
import { api, type ShareInfo, type SharePermission } from '@/lib/api'
import { useUiStore } from '@/stores/ui'
import AccessDialog from './AccessDialog.vue'
import AccessSwitch from './AccessSwitch.vue'

const { t } = useI18n()

const props = defineProps<{ share: ShareInfo }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const ui = useUiStore()

// Permission: read/write apply to files and folders; deposit is folder-only.
const permission = ref<SharePermission>(props.share.permission)
const permOptions = [
  { value: 'read' as SharePermission, label: t('shareDialog.permRead') },
  { value: 'write' as SharePermission, label: t('shareDialog.permWrite') },
  ...(props.share.is_dir ? [{ value: 'deposit' as SharePermission, label: t('shareDialog.permDeposit') }] : []),
]

// Password: keep existing unless the user changes the toggle / types a new one.
const hasPassword = ref(props.share.has_password)
const password = ref('')

// Expiry prefilled with remaining whole days (empty = never).
function remainingDays(iso: string | null): number | '' {
  if (!iso) return ''
  const ms = new Date(iso).getTime() - Date.now()
  return ms > 0 ? Math.ceil(ms / 86400000) : ''
}
const expiresDays = ref<number | ''>(remainingDays(props.share.expires))

// Max downloads prefilled with the total limit (empty = unlimited).
const total = props.share.remain_downloads === null ? '' : props.share.downloads + props.share.remain_downloads
const maxDownloads = ref<number | ''>(total)

const loading = ref(false)

async function save() {
  loading.value = true
  const input: {
    permission?: SharePermission
    password?: string
    expires_days?: number
    max_downloads?: number
  } = {
    expires_days: expiresDays.value === '' ? 0 : Number(expiresDays.value),
    max_downloads: maxDownloads.value === '' ? 0 : Number(maxDownloads.value),
  }
  input.permission = permission.value
  // Password intent: unchecked -> remove (""); checked+typed -> set; checked+empty -> keep (omit).
  if (!hasPassword.value) input.password = ''
  else if (password.value) input.password = password.value

  try {
    await api.updateShare(props.share.token, input)
    ui.toast(t('shareEdit.updated'), 'check')
    emit('saved')
    emit('close')
  } catch (e: any) {
    ui.toast(t('shareEdit.updateFailed') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AccessDialog :title="t('shareEdit.title', { name: share.name })" @close="emit('close')">
    <template #icon><Pencil :size="19" /></template>

    <div class="acc-form">
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
      </div>

      <div class="acc-field">
        <AccessSwitch v-model="hasPassword" :label="t('shareEdit.passwordProtect')" />
        <input
          v-if="hasPassword"
          v-model="password"
          class="input"
          type="text"
          :placeholder="share.has_password ? t('shareEdit.passwordKeepHint') : t('shareEdit.passwordNew')"
        />
      </div>

      <div class="acc-row">
        <div class="acc-field">
          <label>{{ t('shareEdit.expiresDays') }}</label>
          <input v-model="expiresDays" class="input" type="number" min="1" :placeholder="t('shareEdit.never')" />
        </div>
        <div class="acc-field">
          <label>{{ t('shareEdit.maxDownloads') }}</label>
          <input v-model="maxDownloads" class="input" type="number" min="1" :placeholder="t('shareEdit.unlimited')" />
        </div>
      </div>
    </div>

    <template #footer>
      <button class="btn btn-ghost" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" :disabled="loading" @click="save">
        {{ loading ? t('shareEdit.saving') : t('common.save') }}
      </button>
    </template>
  </AccessDialog>
</template>
