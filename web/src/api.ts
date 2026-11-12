// Thin JSON client for the license server API. Every state-changing call
// carries the CSRF header the server requires.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE'

let onUnauthorized: () => void = () => {}
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api/v1' + path, {
    method,
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-Requested-With': 'taktaplus',
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  const ct = res.headers.get('Content-Type') ?? ''
  const data = ct.includes('application/json') ? await res.json() : await res.text()
  if (!res.ok) {
    if (res.status === 401 && !path.startsWith('/auth/')) onUnauthorized()
    const msg = typeof data === 'object' && data && 'error' in data ? String(data.error) : res.statusText
    throw new ApiError(res.status, msg)
  }
  return data as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {}),
  put: <T>(p: string, b: unknown) => request<T>('PUT', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}

// --- types mirroring the server JSON ---

export type User = {
  id: number
  email: string
  name: string
  role: string
  org_id: number
  org_kind: string
  totp_enabled: boolean
}

export type Contact = { tech_name: string; tech_phone: string; tech_email: string; finance_name: string }

export type Org = {
  id: number
  kind: string
  code: string
  name: string
  legal_name: string
  national_id: string
  parent_id: number | null
  parent_name: string
  tier: string
  contact: Contact
  active: boolean
  created_at: string
  licenses: number
  serials: number
  otp_users: number
  nearest_expiry: string | null
}

export type LicenseSummary = {
  id: number
  code: string
  customer_id: number
  customer_name: string
  reseller_id: number | null
  reseller_name: string
  status: 'active' | 'revoked'
  mode: 'online' | 'offline'
  installation_id: string | null
  serials: number
  otp_users: number
  nearest_expiry: string | null
  grace_days: number
  created_at: string
}

export type Item = {
  id: number
  kind: 'device' | 'otp'
  serial: string | null
  platform: string | null
  users: number | null
  starts_on: string
  expires_on: string
  active: boolean
}

export type Installation = {
  id: string
  product_version: string
  status: string
  created_at: string
  last_heartbeat_at: string | null
  last_seen_ip: string
  last_usage: { devices?: number; otp_users?: number }
  warnings: string[]
}

export type LicenseDetail = LicenseSummary & {
  heartbeat_hours: number
  order_ref: string
  notes: string
  revoked_at: string | null
  items: Item[]
  installation: Installation | null
  files: { id: number; at: string; installation_id: string; kid: string; sha256: string; reason: string; delivery: string }[]
  serial_changes: { at: string; old_serial: string; new_serial: string; reason: string }[]
}

export type InstallationRow = Installation & {
  license_id: number | null
  license_code: string | null
  customer_name: string | null
  mode: string | null
}

export type AuditEntry = {
  id: number
  at: string
  source: string
  actor: string
  ip: string
  action: string
  object_type: string
  object_id: string
  details: Record<string, unknown>
}

export type Inspection = {
  request: {
    type: string
    license_key?: string
    license_id?: string
    installation_id: string
    product_version: string
    created_at: string
    usage?: { devices: string[]; otp_users: number }
  }
  license_id: number | null
  license_code: string
  customer: string
  checks: { name: string; ok: boolean; note: string }[]
  can_issue: boolean
}

export type ItemInput = {
  kind: 'device' | 'otp'
  serial?: string
  platform?: string
  users?: number
  starts_on: string
  expires_on: string
}

export type RepoPackage = {
  id: number
  file: string
  platform: 'fortigate' | 'fortiweb'
  type: string
  version: string
  models: string[]
  fw_min: string
  fw_max: string
  upgrade_from: string[]
  notes: string
  released_at: string
  sha256: string
  size: number
  status: 'draft' | 'published' | 'withdrawn'
  uploaded_by: string
  created_at: string
  in_manifest: boolean
}

export type ManifestInfo = { id: number; published_at: string; kid: string; package_count: number; published_by: string }

// --- portals (phase 5) ---

export type Credits = { serial_years: number; user_years: number }
export type CreditTx = {
  id: number
  at: string
  serial_years: number
  user_years: number
  balance_serial: number
  balance_user: number
  description: string
  license_code: string
  by: string
}
export type PortalUser = { id: number; email: string; name: string; role: string; totp_enabled: boolean; active: boolean; last_login_at: string | null; created_at: string }
export type PortalOverview = {
  org: Org
  licenses: LicenseSummary[]
  details?: LicenseDetail[]
  credits?: Credits
  transactions?: CreditTx[]
  customers?: Org[]
}

// --- product backup and upgrade ---
export type SFTPConfig = { host: string; port: number; user: string; password?: string; private_key?: string; dir: string; host_key: string }
export type SystemBackupSettings = {
  enabled: boolean; cron: string; keep: number; sftp_enabled: boolean; sftp: SFTPConfig; sftp_keep: number
  notify_channel_id: number | null; notify_to: string[]
}
export type SystemBackupView = SystemBackupSettings & { has_password: boolean; has_private_key: boolean; key_set: boolean; key_set_at: string | null }
export type SystemRun = {
  id: number; trigger: 'manual' | 'schedule' | 'pre_upgrade' | 'file'; status: 'running' | 'ok' | 'failed'; started_at: string; finished_at: string | null
  file: string; size: number; sftp_status: '' | 'ok' | 'failed'; error: string; created_by: string; available: boolean
}
export type SystemUpgrade = {
  id: number; version: string; from_version: string; notes: string
  status: 'staged' | 'queued' | 'running' | 'done' | 'failed' | 'rolled_back' | 'cancelled'
  step: string; message: string; log: string; uploaded_by: string; created_at: string; finished_at: string | null
}
export type SystemInfo = {
  version: string; commit: string; build_date: string; can_verify: boolean; updater: boolean; updater_seen: string | null
  backup_bytes: number; free_bytes: number
}

// --- API tokens ---
export type APIToken = {
  id: number; token_id: string; name: string; role: string; read_only: boolean; allowed_nets: string[] | null
  expires_at: string | null; created_by: string; created_at: string; last_used_at: string | null; last_ip: string; revoked_at: string | null
}
