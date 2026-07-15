import axios from 'axios'

/** A file or folder as returned by the backend. */
export interface FileNode {
  id: number
  parent_id: number | null
  name: string
  type: 'file' | 'folder'
  size: number
  starred: boolean
  locked: boolean
  owner: string
  modified: string
}

export interface Version {
  id: number
  size: number
  created: string
  current: boolean
  encrypted: boolean
}

export interface DirectLink {
  token: string
  url: string
  downloads: number
  created: string
}

export interface ArchiveEntry {
  name: string
  size: number
  is_dir: boolean
}

export interface Task {
  id: string
  type: string
  status: 'pending' | 'running' | 'done' | 'failed'
  progress: number
  message: string
  error?: string
  result?: Record<string, any>
  created_at: string
  updated_at: string
}

export interface Me {
  id: number
  email: string
  nick: string
  avatar: string
  storage_used: number
  oidc_enabled: boolean
  can_share: boolean
  wopi: boolean
  admin: boolean
}

export interface AdminStats {
  users: number
  files: number
  shares: number
  groups: number
  policies: number
  storage_used: number
}

export interface AdminUser {
  id: number
  email: string
  nick: string
  status: number
  storage_used: number
  group_id: number
  group_name: string
  admin: boolean
  created_at: string
}

export interface AdminGroup {
  id: number
  name: string
  max_storage: number
  speed_limit: number
  storage_policy_id: number
  can_share: boolean
  can_admin: boolean
  sso_groups: string
  user_count: number
}

export interface AdminPolicy {
  id: number
  name: string
  type: string
  server: string
  bucket_name: string
  base_path: string
  settings: any
}

export interface Capacity {
  used: number
  total: number
}

export interface UploadInit {
  session_id: string
  chunk_size: number
  num_chunks: number
  received: boolean[]
}

const http = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
})

// Unwrap the {code, data, msg} envelope, turning non-zero codes into errors.
http.interceptors.response.use(
  (resp) => {
    const body = resp.data
    if (body && typeof body === 'object' && 'code' in body) {
      if (body.code !== 0) {
        return Promise.reject(new ApiError(body.code, body.msg || 'error'))
      }
      return body.data
    }
    return resp.data
  },
  (err) => {
    const body = err.response?.data
    if (body && typeof body === 'object' && 'code' in body) {
      return Promise.reject(new ApiError(body.code, body.msg || 'error'))
    }
    return Promise.reject(err)
  },
)

export class ApiError extends Error {
  code: number
  constructor(code: number, message: string) {
    super(message)
    this.code = code
  }
}

type P = <T = any>(...a: any[]) => Promise<T>
const get = http.get.bind(http) as P
const post = http.post.bind(http) as P
const put = http.put.bind(http) as P

export const api = {
  me: () => get<Me>('/user/me'),
  capacity: () => get<Capacity>('/user/capacity'),

  list: (params: { parent?: string; view?: string; q?: string; all?: string }) =>
    get<FileNode[]>('/file', { params }),
  createFolder: (parent: string, name: string) =>
    post<FileNode>('/file/folder', { parent, name }),
  rename: (id: number, name: string) => post<FileNode>('/file/rename', { id, name }),
  star: (id: number) => post<FileNode>('/file/star', { id }),
  move: (ids: number[], parent: string) => post('/file/move', { ids, parent }),
  trash: (ids: number[]) => post('/file/trash', { ids }),
  restore: (ids: number[]) => post('/file/restore', { ids }),
  purge: (ids: number[]) => post('/file/purge', { ids }),
  contentUrl: (id: number) => `/api/v1/file/content/${id}`,
  inlineUrl: (id: number) => `/api/v1/file/content/${id}?inline=1`,
  thumbUrl: (id: number) => `/api/v1/file/thumb/${id}`,
  saveText: (id: number, content: string) => put<FileNode>('/file/text', { id, content }),
  saveBlob: (id: number, blob: Blob) =>
    put<FileNode>(`/file/blob/${id}`, blob, { headers: { 'Content-Type': 'application/octet-stream' } }),
  officeUrl: (id: number) => `/api/v1/file/office/${id}`,
  archiveUrl: (ids: number[]) => `/api/v1/file/archive?ids=${ids.join(',')}`,

  // Archives (background tasks)
  compress: (parent: string, ids: number[], name?: string) =>
    post<Task>('/file/archive/compress', { parent, ids, name }),
  extract: (id: number, parent: string) => post<Task>('/file/archive/extract', { id, parent }),
  archiveEntries: (id: number) => get<ArchiveEntry[]>(`/file/archive/entries/${id}`),
  taskStatus: (id: string) => get<Task>(`/task/${id}`),

  // Locking
  lock: (id: number) => post<FileNode>('/file/lock', { id }),
  unlock: (id: number) => post<FileNode>('/file/unlock', { id }),

  // Versioning
  listVersions: (id: number) => get<Version[]>(`/file/versions/${id}`),
  restoreVersion: (file_id: number, entity_id: number) =>
    post<FileNode>('/file/version/restore', { file_id, entity_id }),
  deleteVersion: (file_id: number, entity_id: number) =>
    post('/file/version/delete', { file_id, entity_id }),

  // Direct links
  createDirectLink: (id: number) => post<DirectLink>('/file/direct-link', { id }),
  listDirectLinks: (id: number) => get<DirectLink[]>(`/file/direct-links/${id}`),
  deleteDirectLink: (token: string) => http.delete(`/file/direct-link/${token}`),

  initUpload: (parent: string, name: string, size: number) =>
    post<UploadInit>('/upload', { parent, name, size }),
  putChunk: (sid: string, index: number, chunk: Blob, onProgress?: (sent: number) => void) =>
    http.post(`/upload/${sid}/chunk`, chunk, {
      headers: { 'X-Chunk-Index': String(index), 'Content-Type': 'application/octet-stream' },
      onUploadProgress: onProgress ? (e) => onProgress(e.loaded ?? 0) : undefined,
    }),
  completeUpload: (sid: string) => post<FileNode>(`/upload/${sid}/complete`),
  cancelUpload: (sid: string) => http.delete(`/upload/${sid}`),

  // Sharing
  createShare: (input: { file_id: number; password?: string; expires_days?: number; max_downloads?: number }) =>
    post<{ token: string; url: string }>('/share', input),
  listShares: () => get<ShareInfo[]>('/share'),
  updateShare: (
    token: string,
    input: { password?: string; expires_days?: number; max_downloads?: number },
  ) => http.patch(`/share/${token}`, input),
  deleteShare: (token: string) => http.delete(`/share/${token}`),
  shareView: (token: string) => get<ShareView>(`/share/${token}`),
  shareList: (token: string, path: string, password?: string) =>
    get<{ name: string; entries: ShareEntry[] }>(`/share/${token}/list`, { params: { path, password } }),
  shareContentUrl: (token: string, path?: string, password?: string) => {
    const q = new URLSearchParams()
    if (path) q.set('path', path)
    if (password) q.set('password', password)
    const s = q.toString()
    return `/api/v1/share/${token}/content` + (s ? `?${s}` : '')
  },
  shareArchiveUrl: (token: string, path?: string, password?: string) => {
    const q = new URLSearchParams()
    if (path) q.set('path', path)
    if (password) q.set('password', password)
    const s = q.toString()
    return `/api/v1/share/${token}/archive` + (s ? `?${s}` : '')
  },

  logout: () => post('/auth/logout'),

  // Admin
  adminStats: () => get<AdminStats>('/admin/stats'),
  adminUsers: () => get<AdminUser[]>('/admin/users'),
  adminUpdateUser: (id: number, input: { group_id?: number; status?: number }) =>
    http.patch(`/admin/users/${id}`, input),
  adminGroups: () => get<AdminGroup[]>('/admin/groups'),
  adminCreateGroup: (g: Partial<AdminGroup>) => post<{ id: number }>('/admin/groups', g),
  adminUpdateGroup: (id: number, g: Partial<AdminGroup>) => http.patch(`/admin/groups/${id}`, g),
  adminDeleteGroup: (id: number) => http.delete(`/admin/groups/${id}`),
  adminPolicies: () => get<AdminPolicy[]>('/admin/policies'),
  adminCreatePolicy: (p: any) => post<{ id: number }>('/admin/policies', p),
  adminDeletePolicy: (id: number) => http.delete(`/admin/policies/${id}`),
}

export interface ShareInfo {
  token: string
  file_id: number
  name: string
  is_dir: boolean
  url: string
  has_password: boolean
  expired: boolean
  exhausted: boolean
  expires: string | null
  remain_downloads: number | null
  views: number
  downloads: number
  created_at: string
}

export interface ShareView {
  token: string
  name: string
  is_dir: boolean
  size: number
  has_password: boolean
  expired: boolean
  exhausted: boolean
  downloads: number
  owner: string
}

export interface ShareEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
}
