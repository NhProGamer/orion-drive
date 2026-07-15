<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft, LayoutDashboard, Users, Shield, HardDrive, Trash2, Plus, Ban, Check } from 'lucide-vue-next'
import {
  api,
  type AdminStats,
  type AdminUser,
  type AdminGroup,
  type AdminPolicy,
} from '@/lib/api'
import { fmtSize } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const router = useRouter()
const auth = useAuthStore()
const ui = useUiStore()

type Tab = 'dashboard' | 'users' | 'groups' | 'policies'
const tab = ref<Tab>('dashboard')
const ready = ref(false)

const stats = ref<AdminStats | null>(null)
const users = ref<AdminUser[]>([])
const groups = ref<AdminGroup[]>([])
const policies = ref<AdminPolicy[]>([])

async function loadAll() {
  ;[stats.value, users.value, groups.value, policies.value] = await Promise.all([
    api.adminStats(),
    api.adminUsers(),
    api.adminGroups(),
    api.adminPolicies(),
  ])
}

onMounted(async () => {
  await auth.load()
  if (!auth.ready) return
  if (!auth.isAuthenticated || !auth.isAdmin) {
    router.replace('/')
    return
  }
  try {
    await loadAll()
  } catch (e: any) {
    ui.toast('Échec du chargement admin' + (e?.message ? ` : ${e.message}` : ''), 'x')
  }
  ready.value = true
})

/* Users */
async function setUserGroup(u: AdminUser, groupId: number) {
  await api.adminUpdateUser(u.id, { group_id: groupId })
  u.group_id = groupId
  ui.toast('Utilisateur mis à jour', 'check')
}
async function toggleBan(u: AdminUser) {
  const status = u.status === 0 ? 1 : 0
  await api.adminUpdateUser(u.id, { status })
  u.status = status
}

/* Groups */
const gForm = ref<Partial<AdminGroup> | null>(null)
function newGroup() {
  gForm.value = { name: '', max_storage: 0, speed_limit: 0, storage_policy_id: policies.value[0]?.id ?? 1, can_share: true, can_admin: false, sso_groups: '' }
}
function editGroup(g: AdminGroup) {
  gForm.value = { ...g }
}
async function saveGroup() {
  const g = gForm.value!
  const body = {
    name: g.name,
    max_storage: Number(g.max_storage) || 0,
    speed_limit: Number(g.speed_limit) || 0,
    storage_policy_id: Number(g.storage_policy_id) || 0,
    can_share: !!g.can_share,
    can_admin: !!g.can_admin,
    sso_groups: g.sso_groups || '',
  }
  if (g.id) await api.adminUpdateGroup(g.id, body)
  else await api.adminCreateGroup(body)
  gForm.value = null
  groups.value = await api.adminGroups()
  ui.toast('Groupe enregistré', 'check')
}
async function deleteGroup(g: AdminGroup) {
  if (!confirm(`Supprimer le groupe « ${g.name} » ?`)) return
  try {
    await api.adminDeleteGroup(g.id)
    groups.value = await api.adminGroups()
  } catch (e: any) {
    ui.toast(e?.message || 'Suppression impossible', 'x')
  }
}

/* Policies */
const pForm = ref<any | null>(null)
function newPolicy() {
  pForm.value = { name: '', type: 'local', server: '', bucket_name: '', base_path: '', access_key: '', secret_key: '', region: 'us-east-1', path_style: true, encrypt: false }
}
async function savePolicy() {
  const p = pForm.value
  const body: any = { name: p.name, type: p.type, server: p.server, bucket_name: p.bucket_name, base_path: p.base_path, access_key: p.access_key, secret_key: p.secret_key }
  if (p.type === 's3') body.settings = { region: p.region, path_style: !!p.path_style }
  else if (p.type === 'local') body.settings = { encrypt: !!p.encrypt }
  await api.adminCreatePolicy(body)
  pForm.value = null
  policies.value = await api.adminPolicies()
  ui.toast('Policy créée', 'check')
}
async function deletePolicy(p: AdminPolicy) {
  if (!confirm(`Supprimer la policy « ${p.name} » ?`)) return
  try {
    await api.adminDeletePolicy(p.id)
    policies.value = await api.adminPolicies()
  } catch (e: any) {
    ui.toast(e?.message || 'Suppression impossible', 'x')
  }
}

const TABS: { id: Tab; label: string; icon: any }[] = [
  { id: 'dashboard', label: 'Tableau de bord', icon: LayoutDashboard },
  { id: 'users', label: 'Utilisateurs', icon: Users },
  { id: 'groups', label: 'Groupes', icon: Shield },
  { id: 'policies', label: 'Stockage', icon: HardDrive },
]
</script>

<template>
  <div v-if="ready" class="admin">
    <header class="admin-top">
      <button class="btn btn-ghost" @click="router.push('/')"><ArrowLeft :size="16" />Retour au Drive</button>
      <h1>Administration</h1>
    </header>

    <nav class="admin-tabs">
      <button v-for="t in TABS" :key="t.id" class="admin-tab" :class="{ active: tab === t.id }" @click="tab = t.id">
        <component :is="t.icon" :size="16" />{{ t.label }}
      </button>
    </nav>

    <!-- Dashboard -->
    <section v-if="tab === 'dashboard' && stats" class="admin-cards">
      <div class="stat-card"><span class="stat-num">{{ stats.users }}</span><span class="stat-label">Utilisateurs</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.files }}</span><span class="stat-label">Fichiers</span></div>
      <div class="stat-card"><span class="stat-num">{{ fmtSize(stats.storage_used) }}</span><span class="stat-label">Stockage utilisé</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.shares }}</span><span class="stat-label">Partages</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.groups }}</span><span class="stat-label">Groupes</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.policies }}</span><span class="stat-label">Policies</span></div>
    </section>

    <!-- Users -->
    <section v-else-if="tab === 'users'" class="admin-table">
      <table>
        <thead><tr><th>Email</th><th>Groupe</th><th>Stockage</th><th>Statut</th><th></th></tr></thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>{{ u.email }} <span v-if="u.admin" class="badge">admin</span></td>
            <td>
              <select class="input" :value="u.group_id" @change="setUserGroup(u, Number(($event.target as HTMLSelectElement).value))">
                <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }}</option>
              </select>
            </td>
            <td class="mono">{{ fmtSize(u.storage_used) }}</td>
            <td><span class="badge" :class="{ danger: u.status !== 0 }">{{ u.status === 0 ? 'Actif' : 'Banni' }}</span></td>
            <td class="row-actions">
              <button class="icon-btn" :title="u.status === 0 ? 'Bannir' : 'Réactiver'" @click="toggleBan(u)">
                <component :is="u.status === 0 ? Ban : Check" :size="16" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Groups -->
    <section v-else-if="tab === 'groups'" class="admin-table">
      <div class="admin-actions"><button class="btn btn-primary" @click="newGroup"><Plus :size="15" />Nouveau groupe</button></div>
      <table>
        <thead><tr><th>Nom</th><th>Quota</th><th>Débit max</th><th>Partage</th><th>Admin</th><th>Groupes SSO</th><th>Membres</th><th></th></tr></thead>
        <tbody>
          <tr v-for="g in groups" :key="g.id">
            <td>{{ g.name }}</td>
            <td class="mono">{{ g.max_storage ? fmtSize(g.max_storage) : '∞' }}</td>
            <td class="mono">{{ g.speed_limit ? fmtSize(g.speed_limit) + '/s' : '∞' }}</td>
            <td>{{ g.can_share ? 'oui' : 'non' }}</td>
            <td>{{ g.can_admin ? 'oui' : 'non' }}</td>
            <td class="mono">{{ g.sso_groups || '—' }}</td>
            <td class="mono">{{ g.user_count }}</td>
            <td class="row-actions">
              <button class="icon-btn" title="Modifier" @click="editGroup(g)"><Shield :size="15" /></button>
              <button v-if="g.id !== 1" class="icon-btn danger" title="Supprimer" @click="deleteGroup(g)"><Trash2 :size="15" /></button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Policies -->
    <section v-else-if="tab === 'policies'" class="admin-table">
      <div class="admin-actions"><button class="btn btn-primary" @click="newPolicy"><Plus :size="15" />Nouvelle policy</button></div>
      <table>
        <thead><tr><th>Nom</th><th>Type</th><th>Serveur / Bucket</th><th>Base path</th><th></th></tr></thead>
        <tbody>
          <tr v-for="p in policies" :key="p.id">
            <td>{{ p.name }}</td>
            <td><span class="badge">{{ p.type }}</span></td>
            <td class="mono">{{ p.server }}{{ p.bucket_name ? ' · ' + p.bucket_name : '' }}</td>
            <td class="mono">{{ p.base_path || '—' }}</td>
            <td class="row-actions"><button class="icon-btn danger" title="Supprimer" @click="deletePolicy(p)"><Trash2 :size="15" /></button></td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>

  <!-- Group editor -->
  <div v-if="gForm" class="overlay" @click.self="gForm = null">
    <div class="dialog">
      <h2>{{ gForm.id ? 'Modifier le groupe' : 'Nouveau groupe' }}</h2>
      <div class="form-grid">
        <label>Nom<input v-model="gForm.name" class="input" /></label>
        <label>Policy de stockage
          <select v-model="gForm.storage_policy_id" class="input">
            <option v-for="p in policies" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </label>
        <label>Quota (octets, 0 = illimité)<input v-model="gForm.max_storage" class="input" type="number" min="0" /></label>
        <label>Débit max download (o/s, 0 = illimité)<input v-model="gForm.speed_limit" class="input" type="number" min="0" /></label>
        <label>Groupes SSO (séparés par des virgules)
          <input v-model="gForm.sso_groups" class="input" placeholder="engineering, ops" />
        </label>
        <label class="chk"><input v-model="gForm.can_share" type="checkbox" />Autoriser le partage</label>
        <label class="chk"><input v-model="gForm.can_admin" type="checkbox" />Accès administrateur</label>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-ghost" @click="gForm = null">Annuler</button>
        <button class="btn btn-primary" @click="saveGroup">Enregistrer</button>
      </div>
    </div>
  </div>

  <!-- Policy creator -->
  <div v-if="pForm" class="overlay" @click.self="pForm = null">
    <div class="dialog">
      <h2>Nouvelle policy de stockage</h2>
      <div class="form-grid">
        <label>Nom<input v-model="pForm.name" class="input" /></label>
        <label>Type
          <select v-model="pForm.type" class="input">
            <option value="local">Local</option>
            <option value="s3">S3 / compatible</option>
            <option value="remote">Nœud distant (slave)</option>
          </select>
        </label>
        <template v-if="pForm.type === 'local'">
          <label>Répertoire (base path)<input v-model="pForm.base_path" class="input" placeholder="data/storage" /></label>
          <label class="chk"><input v-model="pForm.encrypt" type="checkbox" />Chiffrer au repos (nécessite EncryptionKey)</label>
        </template>
        <template v-else-if="pForm.type === 's3'">
          <label>Endpoint<input v-model="pForm.server" class="input" placeholder="https://s3.amazonaws.com" /></label>
          <label>Bucket<input v-model="pForm.bucket_name" class="input" /></label>
          <label>Region<input v-model="pForm.region" class="input" /></label>
          <label>Access key<input v-model="pForm.access_key" class="input" /></label>
          <label>Secret key<input v-model="pForm.secret_key" class="input" type="password" /></label>
          <label>Base path (préfixe)<input v-model="pForm.base_path" class="input" /></label>
          <label class="chk"><input v-model="pForm.path_style" type="checkbox" />Path-style (MinIO, etc.)</label>
        </template>
        <template v-else>
          <label>URL du nœud<input v-model="pForm.server" class="input" placeholder="http://slave:5212" /></label>
          <label>Secret partagé<input v-model="pForm.secret_key" class="input" type="password" /></label>
          <label>Base path<input v-model="pForm.base_path" class="input" /></label>
        </template>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-ghost" @click="pForm = null">Annuler</button>
        <button class="btn btn-primary" @click="savePolicy">Créer</button>
      </div>
    </div>
  </div>
</template>
