/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getFreshAuthHeaders } from '@/lib/auth-session'
import { cn } from '@/lib/utils'

import { SettingsSection } from '../components/settings-section'

/**
 * 数据迁移面板。
 *
 * 迁移要操作宿主机 Docker（停容器、读写命名卷），实现落在控制台的
 * migrate-runner 上，通过同源的 `/migrate/api/*` 暴露。控制台同时托管本前端
 * 构建产物，所以这里直接用系统设置的原生组件渲染，不再内嵌独立的 HTML 页面。
 */

const API_BASE = '/migrate/api'

const STAT_LABELS: Record<string, string> = {
  users: 'Users',
  tokens: 'Tokens',
  channels: 'Channels',
  redemptions: 'Redeem codes',
  subscription_plans: 'Subscription plans',
}

type MigrateJobStatus = 'running' | 'success' | 'failed'

type MigrateJobLogLine = {
  s?: 'err' | 'out'
  m: string
}

type MigrateJob = {
  id: string
  kind: 'export' | 'import'
  status: MigrateJobStatus
  logs?: MigrateJobLogLine[]
  error?: string
  result?: {
    downloadUrl?: string | null
    archiveName?: string
    kind?: string
  } | null
}

type MigratePackage = {
  kind: 'exports' | 'imports'
  name: string
  bytes: number
  mtime: number
}

type MigrateEnvInfo = {
  toolAvailable: boolean
  migrateDir: string
  activeJob: MigrateJob | null
  packages: MigratePackage[]
}

type InspectInfo = {
  ok?: boolean
  container?: {
    name?: string
    image?: string
    volume?: string
    running?: boolean
  }
  options?: {
    SystemName?: string
    ServerAddress?: string
  }
  stats?: Record<string, number>
  note?: string
  error?: string
}

type Notice = {
  kind: 'ok' | 'err'
  text: string
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

async function migrateRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const authHeaders = await getFreshAuthHeaders()
  const headers = new Headers(init?.headers)
  for (const [key, value] of Object.entries(authHeaders)) {
    if (!headers.has(key)) headers.set(key, value)
  }

  const response = await fetch(`${API_BASE}${path}`, {
    credentials: 'same-origin',
    ...init,
    headers,
  })
  const body: unknown = await response.json().catch(() => ({}))

  if (!response.ok) {
    const message =
      body && typeof body === 'object' && 'message' in body
        ? String((body as { message?: unknown }).message)
        : ''
    throw new Error(message || `请求失败（${response.status}）`)
  }

  return body as T
}

async function migrateDownload(url: string, filename: string): Promise<void> {
  const authHeaders = await getFreshAuthHeaders()
  const authorization = authHeaders.Authorization

  const response = await fetch(url, {
    credentials: 'same-origin',
    headers: authorization ? { Authorization: authorization } : undefined,
  })
  if (!response.ok) {
    throw new Error(`下载失败（${response.status}）`)
  }

  const blob = await response.blob()
  const objectUrl = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename || 'sliderapi-migration.tar.gz'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 4000)
}

function formatBytes(value?: number | null): string {
  if (value === null || value === undefined) return '-'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 * 1024 * 1024) {
    return `${(value / 1024 / 1024).toFixed(2)} MB`
  }
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function formatTime(ms: number): string {
  const date = new Date(ms)
  const pad = (value: number) => String(value).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}`
  )
}

export function MigrationPanel() {
  const { t } = useTranslation()

  const [env, setEnv] = useState<MigrateEnvInfo | null>(null)
  const [info, setInfo] = useState<InspectInfo | null>(null)
  const [inspecting, setInspecting] = useState(false)
  const [job, setJob] = useState<MigrateJob | null>(null)
  const [packages, setPackages] = useState<MigratePackage[]>([])
  const [packagesLoading, setPackagesLoading] = useState(true)
  const [notice, setNotice] = useState<Notice | null>(null)

  const [exporting, setExporting] = useState(false)
  const [importing, setImporting] = useState(false)
  const [dragging, setDragging] = useState(false)

  const [exportContainer, setExportContainer] = useState('')
  const [exportKeepStopped, setExportKeepStopped] = useState(false)
  const [importContainer, setImportContainer] = useState('')
  const [importSetOption, setImportSetOption] = useState('')
  const [importConfirm, setImportConfirm] = useState(false)
  const [file, setFile] = useState<File | null>(null)

  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const logRef = useRef<HTMLDivElement | null>(null)

  const toolAvailable = env ? env.toolAvailable : true
  const jobRunning = job?.status === 'running'

  const inspect = useCallback(async () => {
    setInspecting(true)
    try {
      const body = await migrateRequest<InspectInfo>('/inspect', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({}),
      })
      setInfo(body)
    } catch (error) {
      setInfo({ ok: false, error: errorMessage(error) })
    } finally {
      setInspecting(false)
    }
  }, [])

  const loadPackages = useCallback(async () => {
    try {
      const body = await migrateRequest<{ packages: MigratePackage[] }>(
        '/packages'
      )
      setPackages(body.packages ?? [])
    } catch (error) {
      setNotice({ kind: 'err', text: errorMessage(error) })
    } finally {
      setPackagesLoading(false)
    }
  }, [])

  const handleJobUpdate = useCallback(
    (next: MigrateJob) => {
      setJob(next)
      if (next.status === 'running') return

      void loadPackages()
      void inspect()
      if (next.status === 'success') {
        setNotice({
          kind: 'ok',
          text:
            next.kind === 'export'
              ? t('Export complete. Download the migration package below.')
              : t(
                  'Import complete. The container has started and passed verification.'
                ),
        })
      } else if (next.status === 'failed') {
        setNotice({
          kind: 'err',
          text: `${t('Task failed')}${next.error ? `: ${next.error}` : `: ${t('Please check the log')}`}`,
        })
      }
    },
    [inspect, loadPackages, t]
  )

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const body = await migrateRequest<MigrateEnvInfo>('/env')
        if (cancelled) return
        setEnv(body)
        setPackages(body.packages ?? [])
        if (body.activeJob && body.activeJob.status === 'running') {
          setJob(body.activeJob)
        }
      } catch (error) {
        if (!cancelled) setNotice({ kind: 'err', text: errorMessage(error) })
      } finally {
        if (!cancelled) setPackagesLoading(false)
      }
      if (!cancelled) void inspect()
    })()
    return () => {
      cancelled = true
    }
  }, [inspect])

  useEffect(() => {
    if (!job || job.status !== 'running') return
    const jobId = job.id
    let cancelled = false

    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const body = await migrateRequest<{ job: MigrateJob }>(
            `/jobs/${jobId}`
          )
          if (!cancelled) handleJobUpdate(body.job)
        } catch (error) {
          if (!cancelled) {
            setNotice({ kind: 'err', text: errorMessage(error) })
          }
        }
      })()
    }, 1000)

    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [job, handleJobUpdate])

  useEffect(() => {
    const element = logRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [job?.logs])

  const pickFile = (next: File | null) => {
    setFile(next)
    if (!next && fileInputRef.current) fileInputRef.current.value = ''
  }

  const startExport = async () => {
    setNotice(null)
    setExporting(true)
    try {
      const body = await migrateRequest<{ job: MigrateJob }>('/export', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          container: exportContainer.trim() || undefined,
          keepStopped: exportKeepStopped,
        }),
      })
      setJob(body.job)
    } catch (error) {
      setNotice({ kind: 'err', text: errorMessage(error) })
    } finally {
      setExporting(false)
    }
  }

  const startImport = async () => {
    if (!file) return
    setNotice(null)
    setImporting(true)
    try {
      const authHeaders = await getFreshAuthHeaders()
      const headers: Record<string, string> = {
        'content-type': 'application/octet-stream',
        'x-file-name': encodeURIComponent(file.name),
      }
      if (authHeaders.Authorization) {
        headers.Authorization = authHeaders.Authorization
      }
      const container = importContainer.trim()
      const setOption = importSetOption.trim()
      if (container) headers['x-container'] = encodeURIComponent(container)
      if (setOption) headers['x-set-option'] = encodeURIComponent(setOption)

      const response = await fetch(`${API_BASE}/import`, {
        method: 'POST',
        credentials: 'same-origin',
        headers,
        body: file,
      })
      const body: unknown = await response.json().catch(() => ({}))
      if (!response.ok) {
        const message =
          body && typeof body === 'object' && 'message' in body
            ? String((body as { message?: unknown }).message)
            : ''
        throw new Error(message || `请求失败（${response.status}）`)
      }

      pickFile(null)
      setImportConfirm(false)
      setJob((body as { job: MigrateJob }).job)
    } catch (error) {
      setNotice({ kind: 'err', text: errorMessage(error) })
    } finally {
      setImporting(false)
    }
  }

  const removePackage = async (item: MigratePackage) => {
    if (!window.confirm(t('Delete migration package {{name}}?', { name: item.name }))) {
      return
    }
    try {
      await migrateRequest(
        `/packages/${item.kind}/${encodeURIComponent(item.name)}`,
        { method: 'DELETE' }
      )
      void loadPackages()
    } catch (error) {
      setNotice({ kind: 'err', text: errorMessage(error) })
    }
  }

  const downloadPackage = async (url: string, filename: string) => {
    setNotice({ kind: 'ok', text: t('Preparing download…') })
    try {
      await migrateDownload(url, filename)
      setNotice(null)
    } catch (error) {
      setNotice({ kind: 'err', text: errorMessage(error) })
    }
  }

  const stats = info?.stats ?? {}
  const logLines = job?.logs ?? []

  const containerRows: Array<[string, string]> = []
  if (info?.container) {
    const container = info.container
    containerRows.push([t('Container'), container.name || '-'])
    containerRows.push([t('Container image'), container.image || '-'])
    containerRows.push([
      t('Data volume'),
      container.volume ? `${container.volume} → /data` : '-',
    ])
    containerRows.push([
      t('Status'),
      container.running ? t('Running') : t('Stopped'),
    ])
  }
  if (info?.options?.SystemName) {
    containerRows.push([t('Site name'), info.options.SystemName])
  }
  if (info?.options?.ServerAddress) {
    containerRows.push([t('Address'), info.options.ServerAddress])
  }

  const statusBadge = (() => {
    if (!job) return { label: t('Idle'), variant: 'outline' as const }
    if (job.status === 'running') {
      return { label: t('In progress'), variant: 'default' as const }
    }
    if (job.status === 'success') {
      return { label: t('Completed'), variant: 'secondary' as const }
    }
    return { label: t('Failed'), variant: 'destructive' as const }
  })()

  return (
    <SettingsSection title={t('Database Migration')}>
      <div className='flex flex-col gap-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Migrate the new-api SQLite database between two servers: export a migration package on the old server, then import and overwrite it on the new server.'
          )}{' '}
          {t(
            'Export briefly stops the container for a consistent snapshot; import backs up the current database first and rolls back automatically on failure.'
          )}
        </p>

        {env && !env.toolAvailable ? (
          <Alert variant='destructive'>
            <AlertDescription>
              {t(
                'The migration tool was not found on the host. Data migration is unavailable.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        {notice ? (
          <Alert variant={notice.kind === 'err' ? 'destructive' : 'default'}>
            <AlertDescription>{notice.text}</AlertDescription>
          </Alert>
        ) : null}

        <Card>
          <CardHeader>
            <CardTitle>{t('Current instance')}</CardTitle>
            <CardDescription>
              {t(
                'The target container is auto-detected via Docker. Specify it manually below if the container name differs.'
              )}
            </CardDescription>
            <CardAction>
              <Button
                variant='outline'
                size='sm'
                onClick={() => void inspect()}
                disabled={inspecting}
              >
                {t('Re-detect')}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className='space-y-4'>
            {!info ? (
              <p className='text-muted-foreground text-sm'>
                {t('Detecting…')}
              </p>
            ) : info.ok === false ? (
              <p className='text-muted-foreground text-sm'>
                {t('Detection failed:')}{' '}
                {info.error || t('Unknown reason')}
              </p>
            ) : (
              <>
                <div className='space-y-1.5'>
                  {containerRows.map(([key, value]) => (
                    <div key={key} className='flex gap-3 text-sm'>
                      <span className='text-muted-foreground min-w-20 shrink-0'>
                        {key}
                      </span>
                      <span className='min-w-0 break-all'>{value}</span>
                    </div>
                  ))}
                  {info.note ? (
                    <p className='text-muted-foreground text-sm'>
                      {info.note}
                    </p>
                  ) : null}
                </div>

                {Object.keys(stats).length > 0 ? (
                  <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5'>
                    {Object.entries(stats).map(([key, value]) => (
                      <div
                        key={key}
                        className='bg-muted/40 border-border rounded-lg border px-3 py-2'
                      >
                        <div className='text-muted-foreground text-xs'>
                          {t(STAT_LABELS[key] ?? key)}
                        </div>
                        <div className='text-lg font-semibold tabular-nums'>
                          {value}
                        </div>
                      </div>
                    ))}
                  </div>
                ) : null}
              </>
            )}
          </CardContent>
        </Card>

        <div className='grid gap-4 lg:grid-cols-2'>
          <Card>
            <CardHeader>
              <CardTitle>{t('Export (run on old server)')}</CardTitle>
              <CardDescription>
                {t(
                  'Package the current instance database and produce a downloadable migration package.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-4'>
              <div className='space-y-2'>
                <Label htmlFor='migrate-export-container'>
                  {t('Container name (leave blank to auto-detect)')}
                </Label>
                <Input
                  id='migrate-export-container'
                  value={exportContainer}
                  onChange={(event) => setExportContainer(event.target.value)}
                  placeholder={t('e.g. new-api')}
                />
              </div>

              <div className='flex items-start gap-3'>
                <Checkbox
                  id='migrate-export-keep-stopped'
                  className='mt-0.5'
                  checked={exportKeepStopped}
                  onCheckedChange={(checked) =>
                    setExportKeepStopped(checked === true)
                  }
                />
                <Label
                  htmlFor='migrate-export-keep-stopped'
                  className='text-muted-foreground text-sm leading-5 font-normal'
                >
                  {t(
                    'Keep the container stopped after export (do not restart automatically)'
                  )}
                </Label>
              </div>

              <Button
                onClick={() => void startExport()}
                disabled={exporting || jobRunning || !toolAvailable}
              >
                {exporting ? t('Starting…') : t('Export database')}
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('Import (run on new server)')}</CardTitle>
              <CardDescription>
                {t(
                  'Overwrite this instance database with the migration package. Existing users, tokens, channels and settings will be replaced.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-4'>
              <div
                role='button'
                tabIndex={0}
                onClick={() => fileInputRef.current?.click()}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    fileInputRef.current?.click()
                  }
                }}
                onDragOver={(event) => {
                  event.preventDefault()
                  setDragging(true)
                }}
                onDragLeave={() => setDragging(false)}
                onDrop={(event) => {
                  event.preventDefault()
                  setDragging(false)
                  const dropped = event.dataTransfer?.files?.[0]
                  if (dropped) pickFile(dropped)
                }}
                className={cn(
                  'border-border bg-muted/30 text-muted-foreground hover:border-primary/60 hover:text-foreground flex cursor-pointer flex-col items-center gap-1 rounded-lg border border-dashed px-4 py-6 text-center text-sm transition-colors',
                  dragging && 'border-primary text-primary'
                )}
              >
                {file ? (
                  <span>
                    {t('Selected:')}{' '}
                    <span className='text-foreground font-medium'>
                      {file.name}
                    </span>{' '}
                    ({formatBytes(file.size)})
                  </span>
                ) : (
                  <span>
                    {t('Click to select or drag a .tar.gz migration package here')}
                  </span>
                )}
              </div>
              <input
                ref={fileInputRef}
                type='file'
                accept='.gz,.tgz,application/gzip'
                className='hidden'
                onChange={(event) => pickFile(event.target.files?.[0] ?? null)}
              />

              <div className='space-y-2'>
                <Label htmlFor='migrate-import-container'>
                  {t('Container name (leave blank to auto-detect)')}
                </Label>
                <Input
                  id='migrate-import-container'
                  value={importContainer}
                  onChange={(event) => setImportContainer(event.target.value)}
                  placeholder={t('e.g. new-api')}
                />
              </div>

              <div className='space-y-2'>
                <Label htmlFor='migrate-import-options'>
                  {t('Overwrite options (optional, comma-separated K=V)')}
                </Label>
                <Input
                  id='migrate-import-options'
                  value={importSetOption}
                  onChange={(event) => setImportSetOption(event.target.value)}
                  placeholder={t('e.g. ServerAddress=https://new.example.com')}
                />
              </div>

              <div className='flex items-start gap-3'>
                <Checkbox
                  id='migrate-import-confirm'
                  className='mt-0.5'
                  checked={importConfirm}
                  onCheckedChange={(checked) =>
                    setImportConfirm(checked === true)
                  }
                />
                <Label
                  htmlFor='migrate-import-confirm'
                  className='text-muted-foreground text-sm leading-5 font-normal'
                >
                  {t(
                    'I understand this will overwrite all data on the target instance'
                  )}
                </Label>
              </div>

              <div className='flex flex-wrap items-center gap-2'>
                <Button
                  onClick={() => void startImport()}
                  disabled={
                    importing || !file || !importConfirm || !toolAvailable
                  }
                >
                  {importing ? t('Uploading…') : t('Import and overwrite')}
                </Button>
                <Button
                  variant='outline'
                  onClick={() => pickFile(null)}
                  disabled={!file || importing}
                >
                  {t('Clear selected file')}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2'>
              <span>{t('Task log')}</span>
              <Badge variant={statusBadge.variant}>{statusBadge.label}</Badge>
            </CardTitle>
          </CardHeader>
          <CardContent className='space-y-3'>
            <div
              ref={logRef}
              className='bg-muted/40 border-border h-[300px] overflow-auto rounded-lg border p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap'
            >
              {logLines.length === 0 ? (
                <span className='text-muted-foreground'>
                  {t(
                    'No task yet. Start an export or import and the migration process will appear here in real time.'
                  )}
                </span>
              ) : (
                logLines.map((line, index) => (
                  <span
                    key={`${index}-${line.m}`}
                    className={cn(
                      'block',
                      line.s === 'err' && 'text-destructive'
                    )}
                  >
                    {line.m}
                  </span>
                ))
              )}
            </div>

            {job && job.status === 'running' ? (
              <p className='text-muted-foreground text-sm'>
                {t('Waiting for output…')}
              </p>
            ) : null}

            {job &&
            job.status !== 'running' &&
            job.kind === 'export' &&
            job.result?.downloadUrl ? (
              <Button
                onClick={() =>
                  void downloadPackage(
                    job.result?.downloadUrl ?? '',
                    job.result?.archiveName ?? ''
                  )
                }
              >
                {t('Download migration package')}
              </Button>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t('Migration packages')}</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Type')}</TableHead>
                  <TableHead>{t('File name')}</TableHead>
                  <TableHead>{t('Size')}</TableHead>
                  <TableHead>{t('Time')}</TableHead>
                  <TableHead className='w-32'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {packagesLoading ? (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className='text-muted-foreground font-normal'
                    >
                      {t('Loading…')}
                    </TableCell>
                  </TableRow>
                ) : packages.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className='text-muted-foreground font-normal'
                    >
                      {t('No migration packages')}
                    </TableCell>
                  </TableRow>
                ) : (
                  packages.map((item) => (
                    <TableRow key={`${item.kind}/${item.name}`}>
                      <TableCell>
                        {item.kind === 'exports'
                          ? t('Export package')
                          : t('Uploaded package')}
                      </TableCell>
                      <TableCell>{item.name}</TableCell>
                      <TableCell>{formatBytes(item.bytes)}</TableCell>
                      <TableCell>{formatTime(item.mtime)}</TableCell>
                      <TableCell>
                        <div className='flex items-center gap-1'>
                          <Button
                            variant='ghost'
                            size='xs'
                            onClick={() =>
                              void downloadPackage(
                                `${API_BASE}/packages/${item.kind}/${encodeURIComponent(item.name)}`,
                                item.name
                              )
                            }
                          >
                            {t('Download')}
                          </Button>
                          <Button
                            variant='destructive'
                            size='xs'
                            onClick={() => void removePackage(item)}
                          >
                            {t('Delete')}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
    </SettingsSection>
  )
}