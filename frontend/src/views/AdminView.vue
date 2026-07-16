<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, LayoutDashboard, Users, Shield, HardDrive, Trash2, Plus, Ban, Check, Wrench } from 'lucide-vue-next'
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
const { t } = useI18n()

type Tab = 'dashboard' | 'users' | 'groups' | 'policies'
const tab = ref<Tab>('dashboard')
const ready = ref(false)

const stats = ref<AdminStats | null>(null)
const users = ref<AdminUser[]>([])
const groups = ref<AdminGroup[]>([])
const policies = ref<AdminPolicy[]>([])

const maintBusy = ref(false)
async function runMaintenance() {
  maintBusy.value = true
  try {
    const r = await api.adminRunMaintenance()
    stats.value = await api.adminStats()
    ui.toast(t('admin.maintenanceDone', { purged: r.purged_trash, cleaned: r.cleaned_uploads }), 'check')
  } catch (e: any) {
    ui.toast(t('admin.maintenanceFailed') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    maintBusy.value = false
  }
}

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
    ui.toast(t('admin.loadFailed') + (e?.message ? ` : ${e.message}` : ''), 'x')
  }
  ready.value = true
})

/* Users */
async function setUserGroup(u: AdminUser, groupId: number) {
  await api.adminUpdateUser(u.id, { group_id: groupId })
  u.group_id = groupId
  ui.toast(t('admin.userUpdated'), 'check')
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
  ui.toast(t('admin.groupSaved'), 'check')
}
async function deleteGroup(g: AdminGroup) {
  if (!confirm(t('admin.confirmDeleteGroup', { name: g.name }))) return
  try {
    await api.adminDeleteGroup(g.id)
    groups.value = await api.adminGroups()
  } catch (e: any) {
    ui.toast(e?.message || t('admin.deleteFailed'), 'x')
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
  ui.toast(t('admin.policyCreated'), 'check')
}
async function deletePolicy(p: AdminPolicy) {
  if (!confirm(t('admin.confirmDeletePolicy', { name: p.name }))) return
  try {
    await api.adminDeletePolicy(p.id)
    policies.value = await api.adminPolicies()
  } catch (e: any) {
    ui.toast(e?.message || t('admin.deleteFailed'), 'x')
  }
}

const TABS: { id: Tab; label: string; icon: any }[] = [
  { id: 'dashboard', label: t('admin.tabDashboard'), icon: LayoutDashboard },
  { id: 'users', label: t('admin.tabUsers'), icon: Users },
  { id: 'groups', label: t('admin.tabGroups'), icon: Shield },
  { id: 'policies', label: t('admin.tabStorage'), icon: HardDrive },
]
</script>

<template>
  <div v-if="ready" class="admin">
    <header class="admin-top">
      <button class="btn btn-ghost" @click="router.push('/')"><ArrowLeft :size="16" />{{ t('admin.backToDrive') }}</button>
      <h1>{{ t('admin.title') }}</h1>
    </header>

    <nav class="admin-tabs">
      <button v-for="t in TABS" :key="t.id" class="admin-tab" :class="{ active: tab === t.id }" @click="tab = t.id">
        <component :is="t.icon" :size="16" />{{ t.label }}
      </button>
    </nav>

    <!-- Dashboard -->
    <template v-if="tab === 'dashboard' && stats">
    <section class="admin-cards">
      <div class="stat-card"><span class="stat-num">{{ stats.users }}</span><span class="stat-label">{{ t('admin.tabUsers') }}</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.files }}</span><span class="stat-label">{{ t('admin.statFiles') }}</span></div>
      <div class="stat-card"><span class="stat-num">{{ fmtSize(stats.storage_used) }}</span><span class="stat-label">{{ t('admin.statStorageUsed') }}</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.shares }}</span><span class="stat-label">{{ t('admin.statShares') }}</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.groups }}</span><span class="stat-label">{{ t('admin.tabGroups') }}</span></div>
      <div class="stat-card"><span class="stat-num">{{ stats.policies }}</span><span class="stat-label">{{ t('admin.statPolicies') }}</span></div>
    </section>
    <div class="admin-actions" style="margin-top: 20px">
      <button class="btn btn-secondary" :disabled="maintBusy" @click="runMaintenance">
        <Wrench :size="15" />{{ maintBusy ? t('admin.maintenanceRunning') : t('admin.runMaintenance') }}
      </button>
      <span class="stat-label" style="margin-left: 10px">{{ t('admin.maintenanceHint') }}</span>
    </div>
    </template>

    <!-- Users -->
    <section v-else-if="tab === 'users'" class="admin-table">
      <table>
        <thead><tr><th>{{ t('admin.colEmail') }}</th><th>{{ t('admin.colGroup') }}</th><th>{{ t('admin.colStorage') }}</th><th>{{ t('admin.colStatus') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>{{ u.email }} <span v-if="u.admin" class="badge">admin</span></td>
            <td>
              <select class="input" :value="u.group_id" @change="setUserGroup(u, Number(($event.target as HTMLSelectElement).value))">
                <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }}</option>
              </select>
            </td>
            <td class="mono">{{ fmtSize(u.storage_used) }}</td>
            <td><span class="badge" :class="{ danger: u.status !== 0 }">{{ u.status === 0 ? t('admin.statusActive') : t('admin.statusBanned') }}</span></td>
            <td class="row-actions">
              <button class="icon-btn" :title="u.status === 0 ? t('admin.ban') : t('admin.reactivate')" @click="toggleBan(u)">
                <component :is="u.status === 0 ? Ban : Check" :size="16" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Groups -->
    <section v-else-if="tab === 'groups'">
      <div class="admin-actions"><button class="btn btn-primary" @click="newGroup"><Plus :size="15" />{{ t('admin.newGroup') }}</button></div>
      <div class="admin-table">
      <table>
        <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('admin.colQuota') }}</th><th>{{ t('admin.colMaxSpeed') }}</th><th>{{ t('admin.colSharing') }}</th><th>{{ t('admin.colAdmin') }}</th><th>{{ t('admin.colSsoGroups') }}</th><th>{{ t('admin.colMembers') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="g in groups" :key="g.id">
            <td>{{ g.name }}</td>
            <td class="mono">{{ g.max_storage ? fmtSize(g.max_storage) : '∞' }}</td>
            <td class="mono">{{ g.speed_limit ? fmtSize(g.speed_limit) + '/s' : '∞' }}</td>
            <td>{{ g.can_share ? t('admin.yes') : t('admin.no') }}</td>
            <td>{{ g.can_admin ? t('admin.yes') : t('admin.no') }}</td>
            <td class="mono">{{ g.sso_groups || '—' }}</td>
            <td class="mono">{{ g.user_count }}</td>
            <td class="row-actions">
              <button class="icon-btn" :title="t('common.edit')" @click="editGroup(g)"><Shield :size="15" /></button>
              <button v-if="g.id !== 1" class="icon-btn danger" :title="t('common.delete')" @click="deleteGroup(g)"><Trash2 :size="15" /></button>
            </td>
          </tr>
        </tbody>
      </table>
      </div>
    </section>

    <!-- Policies -->
    <section v-else-if="tab === 'policies'">
      <div class="admin-actions"><button class="btn btn-primary" @click="newPolicy"><Plus :size="15" />{{ t('admin.newPolicy') }}</button></div>
      <div class="admin-table">
      <table>
        <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('admin.colType') }}</th><th>{{ t('admin.colServerBucket') }}</th><th>{{ t('admin.colBasePath') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="p in policies" :key="p.id">
            <td>{{ p.name }}</td>
            <td><span class="badge">{{ p.type }}</span></td>
            <td class="mono">{{ p.server }}{{ p.bucket_name ? ' · ' + p.bucket_name : '' }}</td>
            <td class="mono">{{ p.base_path || '—' }}</td>
            <td class="row-actions"><button class="icon-btn danger" :title="t('common.delete')" @click="deletePolicy(p)"><Trash2 :size="15" /></button></td>
          </tr>
        </tbody>
      </table>
      </div>
    </section>
  </div>

  <!-- Group editor -->
  <div v-if="gForm" class="overlay" @click.self="gForm = null">
    <div class="dialog">
      <h2>{{ gForm.id ? t('admin.editGroup') : t('admin.newGroup') }}</h2>
      <div class="form-grid">
        <label>{{ t('common.name') }}<input v-model="gForm.name" class="input" /></label>
        <label>{{ t('admin.storagePolicy') }}
          <select v-model="gForm.storage_policy_id" class="input">
            <option v-for="p in policies" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </label>
        <label>{{ t('admin.quotaBytes') }}<input v-model="gForm.max_storage" class="input" type="number" min="0" /></label>
        <label>{{ t('admin.maxDownloadSpeed') }}<input v-model="gForm.speed_limit" class="input" type="number" min="0" /></label>
        <label>{{ t('admin.ssoGroupsField') }}
          <input v-model="gForm.sso_groups" class="input" placeholder="engineering, ops" />
        </label>
        <label class="chk"><input v-model="gForm.can_share" type="checkbox" />{{ t('admin.allowSharing') }}</label>
        <label class="chk"><input v-model="gForm.can_admin" type="checkbox" />{{ t('admin.adminAccess') }}</label>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-ghost" @click="gForm = null">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" @click="saveGroup">{{ t('common.save') }}</button>
      </div>
    </div>
  </div>

  <!-- Policy creator -->
  <div v-if="pForm" class="overlay" @click.self="pForm = null">
    <div class="dialog">
      <h2>{{ t('admin.newPolicyTitle') }}</h2>
      <div class="form-grid">
        <label>{{ t('common.name') }}<input v-model="pForm.name" class="input" /></label>
        <label>{{ t('admin.colType') }}
          <select v-model="pForm.type" class="input">
            <option value="local">{{ t('admin.typeLocal') }}</option>
            <option value="s3">{{ t('admin.typeS3') }}</option>
            <option value="remote">{{ t('admin.typeRemote') }}</option>
          </select>
        </label>
        <template v-if="pForm.type === 'local'">
          <label>{{ t('admin.directoryBasePath') }}<input v-model="pForm.base_path" class="input" placeholder="data/storage" /></label>
          <label class="chk"><input v-model="pForm.encrypt" type="checkbox" />{{ t('admin.encryptAtRest') }}</label>
        </template>
        <template v-else-if="pForm.type === 's3'">
          <label>{{ t('admin.endpoint') }}<input v-model="pForm.server" class="input" placeholder="https://s3.amazonaws.com" /></label>
          <label>{{ t('admin.bucket') }}<input v-model="pForm.bucket_name" class="input" /></label>
          <label>{{ t('admin.region') }}<input v-model="pForm.region" class="input" /></label>
          <label>{{ t('admin.accessKey') }}<input v-model="pForm.access_key" class="input" /></label>
          <label>{{ t('admin.secretKey') }}<input v-model="pForm.secret_key" class="input" type="password" /></label>
          <label>{{ t('admin.basePathPrefix') }}<input v-model="pForm.base_path" class="input" /></label>
          <label class="chk"><input v-model="pForm.path_style" type="checkbox" />{{ t('admin.pathStyle') }}</label>
        </template>
        <template v-else>
          <label>{{ t('admin.nodeUrl') }}<input v-model="pForm.server" class="input" placeholder="http://slave:5212" /></label>
          <label>{{ t('admin.sharedSecret') }}<input v-model="pForm.secret_key" class="input" type="password" /></label>
          <label>{{ t('admin.colBasePath') }}<input v-model="pForm.base_path" class="input" /></label>
        </template>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-ghost" @click="pForm = null">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" @click="savePolicy">{{ t('common.create') }}</button>
      </div>
    </div>
  </div>
</template>
