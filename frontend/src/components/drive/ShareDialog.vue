<script setup lang="ts">
import { ref } from 'vue'
import { Link2, Copy, Check, ExternalLink } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
import { api } from '@/lib/api'
import { useUiStore } from '@/stores/ui'

const props = defineProps<{ node: FileNode }>()
const emit = defineEmits<{ close: [] }>()

const ui = useUiStore()
const password = ref('')
const expiresDays = ref<number | ''>('')
const maxDownloads = ref<number | ''>('')
const loading = ref(false)
const result = ref<{ url: string } | null>(null)
const copied = ref(false)

async function create() {
  loading.value = true
  try {
    result.value = await api.createShare({
      file_id: props.node.id,
      password: password.value || undefined,
      expires_days: expiresDays.value ? Number(expiresDays.value) : undefined,
      max_downloads: maxDownloads.value ? Number(maxDownloads.value) : undefined,
    })
  } catch (e: any) {
    ui.toast('Échec de la création du lien' + (e?.message ? ` : ${e.message}` : ''), 'x')
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
      <h2><Link2 :size="18" style="vertical-align: -3px; margin-right: 6px" />Partager « {{ node.name }} »</h2>

      <template v-if="!result">
        <div style="display: flex; flex-direction: column; gap: 12px">
          <label style="display: flex; flex-direction: column; gap: 4px">
            <span class="tweak-label" style="letter-spacing: 0.06em">Mot de passe (optionnel)</span>
            <input v-model="password" class="input" type="text" placeholder="Aucun" />
          </label>
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
          <button class="btn btn-primary" :disabled="loading" @click="create">
            {{ loading ? 'Création…' : 'Créer le lien' }}
          </button>
        </div>
      </template>

      <template v-else>
        <p>Lien de partage créé. Toute personne disposant de ce lien peut accéder au fichier.</p>
        <div style="display: flex; gap: 8px; align-items: center">
          <input class="input" :value="result.url" readonly @focus="($event.target as HTMLInputElement).select()" />
          <button class="icon-btn" :title="copied ? 'Copié' : 'Copier'" @click="copy">
            <component :is="copied ? Check : Copy" :size="16" />
          </button>
        </div>
        <div class="dialog-actions">
          <a class="btn btn-secondary" :href="result.url" target="_blank" rel="noopener">
            <ExternalLink :size="15" />Ouvrir
          </a>
          <button class="btn btn-primary" @click="emit('close')">Terminé</button>
        </div>
      </template>
    </div>
  </div>
</template>
