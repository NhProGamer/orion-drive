<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, LayoutDashboard, Users, Shield, HardDrive, Trash2, Plus, Ban, Check, Wrench, Palette, Eye, EyeOff } from 'lucide-vue-next'
import {
  api,
  type AdminStats,
  type AdminUser,
  type AdminGroup,
  type AdminPolicy,
} from '@/lib/api'
import { fmtSize } from '@/lib/format'
import { reloadCustomCss } from '@/lib/customCss'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'
import AccessDialog from '@/components/drive/AccessDialog.vue'
import AccessSwitch from '@/components/drive/AccessSwitch.vue'

const router = useRouter()
const auth = useAuthStore()
const ui = useUiStore()
const { t } = useI18n()

type Tab = 'dashboard' | 'users' | 'groups' | 'policies' | 'appearance'
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
  let appearance: { custom_css: string }
  ;[stats.value, users.value, groups.value, policies.value, appearance] = await Promise.all([
    api.adminStats(),
    api.adminUsers(),
    api.adminGroups(),
    api.adminPolicies(),
    api.adminAppearance(),
  ])
  customCss.value = appearance.custom_css
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

/* Appearance */
// Must match maxCustomCSSBytes on the server.
const CUSTOM_CSS_MAX_BYTES = 64000
const customCss = ref('')
const customCssBytes = computed(() => new TextEncoder().encode(customCss.value).length)
const cssBusy = ref(false)
const previewing = ref(false)
const PREVIEW_ID = 'od-custom-css-preview'

// The admin panel never loads the saved custom CSS; the preview injects the
// draft here only, through textContent (never parsed as HTML), until toggled off.
function setPreview(on: boolean) {
  previewing.value = on
  document.getElementById(PREVIEW_ID)?.remove()
  if (!on) return
  const style = document.createElement('style')
  style.id = PREVIEW_ID
  style.textContent = customCss.value
  document.head.appendChild(style)
}
function onCssInput() {
  if (previewing.value) setPreview(true)
}
async function saveCustomCss() {
  if (customCssBytes.value > CUSTOM_CSS_MAX_BYTES) {
    ui.toast(t('admin.cssTooLarge'), 'x')
    return
  }
  cssBusy.value = true
  try {
    await api.adminUpdateAppearance(customCss.value)
    reloadCustomCss()
    ui.toast(t('admin.cssSaved'), 'check')
  } catch (e: any) {
    ui.toast(t('admin.cssSaveFailed') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    cssBusy.value = false
  }
}
async function resetCustomCss() {
  if (!confirm(t('admin.confirmResetCss'))) return
  customCss.value = ''
  setPreview(false)
  await saveCustomCss()
}
onUnmounted(() => setPreview(false))

const TABS: { id: Tab; label: string; icon: any }[] = [
  { id: 'dashboard', label: t('admin.tabDashboard'), icon: LayoutDashboard },
  { id: 'users', label: t('admin.tabUsers'), icon: Users },
  { id: 'groups', label: t('admin.tabGroups'), icon: Shield },
  { id: 'policies', label: t('admin.tabStorage'), icon: HardDrive },
  { id: 'appearance', label: t('admin.tabAppearance'), icon: Palette },
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
    <div class="admin-maint">
      <button class="btn btn-secondary" :disabled="maintBusy" @click="runMaintenance">
        <Wrench :size="15" />{{ maintBusy ? t('admin.maintenanceRunning') : t('admin.runMaintenance') }}
      </button>
      <span class="stat-label">{{ t('admin.maintenanceHint') }}</span>
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

    <!-- Appearance -->
    <section v-else-if="tab === 'appearance'" class="admin-appearance">
      <p class="stat-label">{{ t('admin.cssHint') }}</p>
      <textarea
        v-model="customCss"
        class="input mono css-editor"
        spellcheck="false"
        :placeholder="':root {\n  --accent: #7c5cff;\n}'"
        @input="onCssInput"
      ></textarea>
      <div class="admin-maint">
        <button class="btn btn-primary" :disabled="cssBusy" @click="saveCustomCss"><Check :size="15" />{{ t('common.save') }}</button>
        <button class="btn btn-secondary" @click="setPreview(!previewing)">
          <component :is="previewing ? EyeOff : Eye" :size="15" />{{ previewing ? t('admin.cssStopPreview') : t('admin.cssPreview') }}
        </button>
        <button class="btn btn-ghost" :disabled="cssBusy || !customCss" @click="resetCustomCss"><Trash2 :size="15" />{{ t('admin.cssReset') }}</button>
        <span class="stat-label mono">{{ customCssBytes }} / {{ CUSTOM_CSS_MAX_BYTES }}</span>
      </div>
      <p class="stat-label">{{ t('admin.cssSafeMode') }}</p>
    </section>
  </div>

  <!-- Group editor -->
  <AccessDialog v-if="gForm" :title="gForm.id ? t('admin.editGroup') : t('admin.newGroup')" @close="gForm = null">
    <template #icon><Shield :size="19" /></template>
    <div class="acc-form">
      <div class="acc-field"><label>{{ t('common.name') }}</label><input v-model="gForm.name" class="input" /></div>
      <div class="acc-field">
        <label>{{ t('admin.storagePolicy') }}</label>
        <select v-model="gForm.storage_policy_id" class="input">
          <option v-for="p in policies" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </div>
      <div class="acc-row">
        <div class="acc-field"><label>{{ t('admin.quotaBytes') }}</label><input v-model="gForm.max_storage" class="input" type="number" min="0" /></div>
        <div class="acc-field"><label>{{ t('admin.maxDownloadSpeed') }}</label><input v-model="gForm.speed_limit" class="input" type="number" min="0" /></div>
      </div>
      <div class="acc-field"><label>{{ t('admin.ssoGroupsField') }}</label><input v-model="gForm.sso_groups" class="input" placeholder="engineering, ops" /></div>
      <AccessSwitch :model-value="!!gForm.can_share" :label="t('admin.allowSharing')" @update:model-value="gForm.can_share = $event" />
      <AccessSwitch :model-value="!!gForm.can_admin" :label="t('admin.adminAccess')" @update:model-value="gForm.can_admin = $event" />
    </div>
    <template #footer>
      <button class="btn btn-ghost" @click="gForm = null">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" @click="saveGroup">{{ t('common.save') }}</button>
    </template>
  </AccessDialog>

  <!-- Policy creator -->
  <AccessDialog v-if="pForm" :title="t('admin.newPolicyTitle')" @close="pForm = null">
    <template #icon><HardDrive :size="19" /></template>
    <div class="acc-form">
      <div class="acc-row">
        <div class="acc-field"><label>{{ t('common.name') }}</label><input v-model="pForm.name" class="input" /></div>
        <div class="acc-field">
          <label>{{ t('admin.colType') }}</label>
          <select v-model="pForm.type" class="input">
            <option value="local">{{ t('admin.typeLocal') }}</option>
            <option value="s3">{{ t('admin.typeS3') }}</option>
            <option value="remote">{{ t('admin.typeRemote') }}</option>
          </select>
        </div>
      </div>
      <template v-if="pForm.type === 'local'">
        <div class="acc-field"><label>{{ t('admin.directoryBasePath') }}</label><input v-model="pForm.base_path" class="input" placeholder="data/storage" /></div>
        <AccessSwitch :model-value="!!pForm.encrypt" :label="t('admin.encryptAtRest')" @update:model-value="pForm.encrypt = $event" />
      </template>
      <template v-else-if="pForm.type === 's3'">
        <div class="acc-field"><label>{{ t('admin.endpoint') }}</label><input v-model="pForm.server" class="input" placeholder="https://s3.amazonaws.com" /></div>
        <div class="acc-row">
          <div class="acc-field"><label>{{ t('admin.bucket') }}</label><input v-model="pForm.bucket_name" class="input" /></div>
          <div class="acc-field"><label>{{ t('admin.region') }}</label><input v-model="pForm.region" class="input" /></div>
        </div>
        <div class="acc-field"><label>{{ t('admin.accessKey') }}</label><input v-model="pForm.access_key" class="input" /></div>
        <div class="acc-field"><label>{{ t('admin.secretKey') }}</label><input v-model="pForm.secret_key" class="input" type="password" /></div>
        <div class="acc-field"><label>{{ t('admin.basePathPrefix') }}</label><input v-model="pForm.base_path" class="input" /></div>
        <AccessSwitch :model-value="!!pForm.path_style" :label="t('admin.pathStyle')" @update:model-value="pForm.path_style = $event" />
      </template>
      <template v-else>
        <div class="acc-field"><label>{{ t('admin.nodeUrl') }}</label><input v-model="pForm.server" class="input" placeholder="http://slave:5212" /></div>
        <div class="acc-field"><label>{{ t('admin.sharedSecret') }}</label><input v-model="pForm.secret_key" class="input" type="password" /></div>
        <div class="acc-field"><label>{{ t('admin.colBasePath') }}</label><input v-model="pForm.base_path" class="input" /></div>
      </template>
    </div>
    <template #footer>
      <button class="btn btn-ghost" @click="pForm = null">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" @click="savePolicy">{{ t('common.create') }}</button>
    </template>
  </AccessDialog>
</template>
