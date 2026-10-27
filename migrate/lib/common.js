'use strict'

process.removeAllListeners('warning')
process.on('warning', (w) => {
  if (w.name !== 'ExperimentalWarning') console.warn(`${w.name}: ${w.message}`)
})

const { spawnSync } = require('node:child_process')
const crypto = require('node:crypto')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const FORMAT = 'sliderapi-migration'
const FORMAT_VERSION = 1
const TOOL_VERSION = '1.0.0'

const DATA_DIR = '/data'
const DB_FILE = 'one-api.db'
const WAL_FILE = DB_FILE + '-wal'
const SHM_FILE = DB_FILE + '-shm'
const PAYLOAD_FILES = [DB_FILE, WAL_FILE]

const COLOR = {
  reset: '\x1b[0m',
  dim: '\x1b[2m',
  red: '\x1b[31m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  cyan: '\x1b[36m',
  bold: '\x1b[1m',
}

function paint(color, text) {
  return process.stdout.isTTY ? color + text + COLOR.reset : text
}

function step(message) {
  console.log('\n' + paint(COLOR.cyan, '▶ ' + message))
}
function ok(message) {
  console.log('  ' + paint(COLOR.green, '✓') + ' ' + message)
}
function warn(message) {
  console.log('  ' + paint(COLOR.yellow, '!') + ' ' + message)
}
function info(message) {
  console.log('    ' + paint(COLOR.dim, message))
}
function die(message) {
  console.error('\n' + paint(COLOR.red, '✗ ' + message))
  process.exit(1)
}

function docker(args, { allowFail = false, inherit = false } = {}) {
  const result = spawnSync('docker', args, {
    encoding: 'utf8',
    maxBuffer: 128 * 1024 * 1024,
    stdio: inherit ? 'inherit' : 'pipe',
  })
  if (result.error) {
    die(
      '无法执行 docker 命令：' +
        result.error.message +
        '\n请确认宿主机已安装 Docker，且 docker 在 PATH 中可用。'
    )
  }
  if (!allowFail && result.status !== 0) {
    const detail = (result.stderr || result.stdout || '').trim()
    die(`docker ${args.slice(0, 4).join(' ')} 执行失败：\n${detail}`)
  }
  return result
}

function dockerOut(args) {
  return (docker(args).stdout || '').trim()
}

function dockerOk(args) {
  return docker(args, { allowFail: true }).status === 0
}

function parseArgs(argv) {
  const parsed = { _: [] }
  for (let i = 0; i < argv.length; i++) {
    const token = argv[i]
    if (!token.startsWith('--')) {
      parsed._.push(token)
      continue
    }
    const eq = token.indexOf('=')
    if (eq !== -1) {
      parsed[token.slice(2, eq)] = token.slice(eq + 1)
      continue
    }
    const key = token.slice(2)
    const next = argv[i + 1]
    if (next !== undefined && !next.startsWith('--')) {
      parsed[key] = next
      i++
    } else {
      parsed[key] = true
    }
  }
  return parsed
}

function mountPath(target) {
  const abs = path.resolve(target)
  return process.platform === 'win32' ? abs.replace(/\\/g, '/') : abs
}

function listNewApiContainers() {
  const rows = dockerOut(['ps', '-a', '--format', '{{.Names}}\t{{.Image}}'])
    .split('\n')
    .filter(Boolean)
    .map((line) => line.split('\t'))
  return rows
    .filter(([name, image]) => /new-api|one-api|sliderapi/i.test(name) || /new-api|one-api|sliderapi/i.test(image))
    .map(([name]) => name)
}

function detectContainer(explicit) {
  if (explicit) {
    if (!dockerOk(['inspect', explicit])) {
      const similar = listNewApiContainers()
      die(
        `找不到容器 ${explicit}` +
          (similar.length > 0 ? `（本机 new-api 相关容器：${similar.join('、')}）` : '')
      )
    }
    return explicit
  }
  const rows = dockerOut(['ps', '-a', '--format', '{{.Names}}\t{{.Image}}\t{{.State}}'])
    .split('\n')
    .filter(Boolean)
    .map((line) => line.split('\t'))

  const candidates = rows
    .map(([name, image, state]) => {
      let score = 0
      if (/new-api|one-api|sliderapi/i.test(image)) score += 2
      if (/new-api|one-api|sliderapi/i.test(name)) score += 1
      if (state === 'running') score += 1
      return { name, image, state, score }
    })
    .filter((c) => c.score >= 3)
    .sort((a, b) => b.score - a.score)

  if (candidates.length === 0) {
    die('未检测到 new-api 容器，请用 --container <容器名> 手动指定。')
  }
  // 多个历史容器并存时，运行中的那个必然是活实例；仅当唯一运行候选时才自动选定
  const running = candidates.filter((c) => c.state === 'running')
  if (running.length === 1 && candidates.length > 1) {
    warn(`检测到多个候选容器，已自动选择运行中的 ${running[0].name}：`)
    candidates.forEach((c) => info(`${c.name}  (${c.image}, ${c.state})`))
    return running[0].name
  }
  if (candidates.length > 1) {
    warn('检测到多个候选容器：')
    candidates.forEach((c) => info(`${c.name}  (${c.image}, ${c.state})`))
    die(
      '为避免误操作，请用 --container <容器名> 明确指定。候选容器：' +
        candidates.map((c) => c.name).join('、')
    )
  }
  return candidates[0].name
}

function inspectContainer(container) {
  const raw = dockerOut(['inspect', container])
  const [data] = JSON.parse(raw)
  const mount = (data.Mounts || []).find((m) => m.Destination === DATA_DIR)
  if (!mount) {
    die(`容器 ${container} 未挂载 ${DATA_DIR}，无法定位数据库。`)
  }
  if (mount.Type !== 'volume') {
    die(
      `容器 ${container} 的 ${DATA_DIR} 是 ${mount.Type} 挂载（${mount.Source}），` +
        '本工具只支持命名卷。请改用 --volume 指定命名卷，或手动迁移。'
    )
  }
  return {
    name: String(data.Name).replace(/^\//, ''),
    image: data.Config.Image,
    running: Boolean(data.State && data.State.Running),
    volume: mount.Name,
  }
}

function helper(image, volume, script, extraMounts = []) {
  const args = ['run', '--rm', '--entrypoint', 'sh', '-v', `${volume}:${DATA_DIR}`]
  for (const m of extraMounts) args.push('-v', m)
  args.push(image, '-c', script)
  return docker(args)
}

function helperOut(image, volume, script, extraMounts = []) {
  return (helper(image, volume, script, extraMounts).stdout || '').trim()
}

function sha256(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')
}

function humanBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

function openDatabase(file) {
  const { DatabaseSync } = require('node:sqlite')
  return new DatabaseSync(file, { readOnly: true })
}

function inspectDatabase(file) {
  const db = openDatabase(file)
  try {
    const stats = {}
    const tables = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
      )
      .all()
    for (const { name } of tables) {
      try {
        stats[name] = db.prepare(`SELECT COUNT(*) AS c FROM "${name}"`).get().c
      } catch {
        stats[name] = null
      }
    }
    const journalMode = db.prepare('PRAGMA journal_mode').get().journal_mode
    const options = {}
    try {
      for (const row of db.prepare('SELECT key, value FROM options').all()) {
        options[row.key] = row.value
      }
    } catch {
      // 表不存在时不视为错误，交由 assertLooksLikeNewApiDatabase 统一判断
    }
    return { stats, journalMode, options, tableCount: tables.length }
  } finally {
    db.close()
  }
}

function assertLooksLikeNewApiDatabase(stats, context) {
  const required = ['users', 'options']
  const missing = required.filter((table) => stats[table] === undefined)
  if (missing.length > 0) {
    die(
      `${context}不是有效的 new-api 数据库：缺少 ${missing.join('、')} 表。\n` +
        '请确认迁移包来源正确，或该文件是否为完整的 new-api SQLite 库。'
    )
  }
}

function countRows(file, table) {
  const db = openDatabase(file)
  try {
    return db.prepare(`SELECT COUNT(*) AS c FROM "${table}"`).get().c
  } catch {
    return null
  } finally {
    db.close()
  }
}

function runTar(args) {
  const result = spawnSync('tar', args, { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
  if (result.error) die('无法执行 tar 命令：' + result.error.message)
  if (result.status !== 0) {
    die(`tar 执行失败：\n${(result.stderr || '').trim()}`)
  }
}

function makeTempDir(prefix) {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix))
}

function confirm(question) {
  process.stdout.write(question + ' [y/N] ')
  const buffer = Buffer.alloc(256)
  try {
    const read = fs.readSync(0, buffer, 0, buffer.length, null)
    const answer = buffer.toString('utf8', 0, read).trim().toLowerCase()
    return answer === 'y' || answer === 'yes'
  } catch {
    return false
  }
}

function sleep(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms)
}

function logsSince(container, since) {
  const args = ['logs', container]
  if (since) args.push('--since', since)
  const result = docker(args, { allowFail: true })
  return ((result.stdout || '') + (result.stderr || '')).trim()
}

function startedAt(container) {
  return dockerOut(['inspect', container, '--format', '{{.State.StartedAt}}'])
}

function containerState(container) {
  return dockerOut(['inspect', container, '--format', '{{.State.Status}}'])
}

function waitForStartup(container, since, timeoutMs = 90000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const state = containerState(container)
    if (state === 'exited' || state === 'dead') return { ok: false, reason: `容器状态变为 ${state}` }
    if (/started/i.test(logsSince(container, since))) return { ok: true }
    sleep(1500)
  }
  return { ok: false, reason: `等待 ${timeoutMs / 1000}s 仍未在日志中看到启动完成标志` }
}

module.exports = {
  FORMAT,
  FORMAT_VERSION,
  TOOL_VERSION,
  DATA_DIR,
  DB_FILE,
  WAL_FILE,
  SHM_FILE,
  PAYLOAD_FILES,
  COLOR,
  paint,
  step,
  ok,
  warn,
  info,
  die,
  docker,
  dockerOut,
  dockerOk,
  parseArgs,
  mountPath,
  detectContainer,
  inspectContainer,
  helper,
  helperOut,
  sha256,
  humanBytes,
  inspectDatabase,
  assertLooksLikeNewApiDatabase,
  countRows,
  runTar,
  makeTempDir,
  confirm,
  logsSince,
  startedAt,
  containerState,
  waitForStartup,
  sleep,
}