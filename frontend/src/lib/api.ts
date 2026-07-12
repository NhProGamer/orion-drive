import axios from 'axios'

/** A file or folder as returned by the backend. */
export interface FileNode {
  id: number
  parent_id: number | null
  name: string
  type: 'file' | 'folder'
  size: number
  starred: boolean
  owner: string
  modified: string
}

export interface Me {
  id: number
  email: string
  nick: string
  avatar: string
  storage_used: number
  oidc_enabled: boolean
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

  initUpload: (parent: string, name: string, size: number) =>
    post<UploadInit>('/upload', { parent, name, size }),
  putChunk: (sid: string, index: number, chunk: Blob) =>
    http.post(`/upload/${sid}/chunk`, chunk, {
      headers: { 'X-Chunk-Index': String(index), 'Content-Type': 'application/octet-stream' },
    }),
  completeUpload: (sid: string) => post<FileNode>(`/upload/${sid}/complete`),
  cancelUpload: (sid: string) => http.delete(`/upload/${sid}`),

  // Sharing
  createShare: (input: { file_id: number; password?: string; expires_days?: number; max_downloads?: number }) =>
    post<{ token: string; url: string }>('/share', input),
  listShares: () => get<ShareInfo[]>('/share'),
  deleteShare: (token: string) => http.delete(`/share/${token}`),
  shareView: (token: string) => get<ShareView>(`/share/${token}`),
  shareContentUrl: (token: string, password?: string) =>
    `/api/v1/share/${token}/content` + (password ? `?password=${encodeURIComponent(password)}` : ''),

  logout: () => post('/auth/logout'),
}

export interface ShareInfo {
  token: string
  file_id: number
  url: string
  has_password: boolean
  expires: string | null
  remain_downloads: number | null
  views: number
  downloads: number
  created_at: string
}

export interface ShareView {
  token: string
  name: string
  size: number
  has_password: boolean
  expired: boolean
  exhausted: boolean
  downloads: number
  owner: string
}
