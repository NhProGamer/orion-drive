<script setup lang="ts">
import { ref } from 'vue'
import { Pencil } from 'lucide-vue-next'
import { api, type ShareInfo } from '@/lib/api'
import { useUiStore } from '@/stores/ui'

const props = defineProps<{ share: ShareInfo }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const ui = useUiStore()

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
  const input: { password?: string; expires_days?: number; max_downloads?: number } = {
    expires_days: expiresDays.value === '' ? 0 : Number(expiresDays.value),
    max_downloads: maxDownloads.value === '' ? 0 : Number(maxDownloads.value),
  }
  // Password intent: unchecked -> remove (""); checked+typed -> set; checked+empty -> keep (omit).
  if (!hasPassword.value) input.password = ''
  else if (password.value) input.password = password.value

  try {
    await api.updateShare(props.share.token, input)
    ui.toast('Partage mis à jour', 'check')
    emit('saved')
    emit('close')
  } catch (e: any) {
    ui.toast('Échec de la mise à jour' + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog">
      <h2><Pencil :size="18" style="vertical-align: -3px; margin-right: 6px" />Modifier le partage « {{ share.name }} »</h2>

      <div style="display: flex; flex-direction: column; gap: 12px">
        <label style="display: flex; align-items: center; gap: 8px">
          <input v-model="hasPassword" type="checkbox" />
          <span class="tweak-label" style="letter-spacing: 0.06em">Protéger par mot de passe</span>
        </label>
        <input
          v-if="hasPassword"
          v-model="password"
          class="input"
          type="text"
          :placeholder="share.has_password ? 'Laisser vide pour conserver le mot de passe actuel' : 'Nouveau mot de passe'"
        />

        <div style="display: flex; gap: 12px">
          <label style="flex: 1; display: flex; flex-direction: column; gap: 4px">
            <span class="tweak-label" style="letter-spacing: 0.06em">Expire (jours)</span>
            <input v-model="expiresDays" class="input" type="number" min="1" placeholder="Jamais" />
          </label>
          <label style="flex: 1; display: flex; flex-direction: column; gap: 4px">
            <span class="tweak-label" style="letter-spacing: 0.06em">Max. téléchargements</span>
            <input v-model="maxDownloads" class="input" type="number" min="1" placeholder="Illimité" />
          </label>
        </div>
      </div>

      <div class="dialog-actions">
        <button class="btn btn-ghost" @click="emit('close')">Annuler</button>
        <button class="btn btn-primary" :disabled="loading" @click="save">
          {{ loading ? 'Enregistrement…' : 'Enregistrer' }}
        </button>
      </div>
    </div>
  </div>
</template>
