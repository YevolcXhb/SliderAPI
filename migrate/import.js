'use strict'

const fs = require('node:fs')
const path = require('node:path')

const {
  FORMAT,
  FORMAT_VERSION,
  TOOL_VERSION,
  DATA_DIR,
  DB_FILE,
  WAL_FILE,
  SHM_FILE,
  paint,
  COLOR,
  step,
  ok,
  warn,
  info,
  die,
  docker,
  parseArgs,
  mountPath,
  detectContainer,
  inspectContainer,
  helper,
  sha256,
  humanBytes,
  inspectDatabase,
  countRows,
  runTar,
  makeTempDir,
  confirm,
  logsSince,
  startedAt,
  waitForStartup,
  sleep,
} = require('./lib/common')

function usage() {
  console.log(`
用法：node import.js <迁移包路径> [选项]

在【新服务器】上运行，把导出包中的数据库导入到 new-api 实例。

选项：
  --container <名字>      指定 new-api 容器名（默认自动检测）
  --backup-dir <目录>     原库备份目录（默认 ./sliderapi-backups）
  --set-option K=V,...    导入后覆盖配置项，例如 ServerAddress=https://new.example.com
  --no-backup             跳过导入前的自动备份（不推荐）
  --keep-stopped          导入后不启动容器
  --yes                   跳过所有确认提示
  --force                 即使校验发现异常也强制导入
  -h, --help              显示帮助

说明：
  导入会整体替换新实例的数据库，新实例原有的用户、令牌、渠道、设置都会被覆盖。
  导入前会自动备份原库，导入失败会自动回滚。
`)
}

function readManifest(staging) {
  const manifestPath = path.join(staging, 'manifest.json')
  if (!fs.existsSync(manifestPath)) {
    die('迁移包中缺少 manifest.json，这不是有效的 SliderAPI 迁移包。')
  }
  let manifest
  try {
    manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
  } catch (error) {
    die('manifest.json 解析失败：' + error.message)
  }
  if (manifest.format !== FORMAT) {
    die(`迁移包格式不匹配：期望 ${FORMAT}，实际 ${manifest.format}`)
  }
  if (typeof manifest.formatVersion !== 'number' || manifest.formatVersion > FORMAT_VERSION) {
    die(
      `迁移包格式版本 ${manifest.formatVersion} 高于当前工具支持的 ${FORMAT_VERSION}，` +
        '请使用更新版本的迁移工具。'
    )
  }
  return manifest
}

function verifyPayload(staging, manifest) {
  const payloadDir = path.join(staging, 'payload')
  const files = Array.isArray(manifest.database && manifest.database.files)
    ? manifest.database.files
    : []
  if (files.length === 0) die('manifest.json 中没有记录任何数据文件。')

  for (const entry of files) {
    const filePath = path.join(payloadDir, entry.name)
    if (!fs.existsSync(filePath)) die(`迁移包缺少文件：payload/${entry.name}`)
    const actualBytes = fs.statSync(filePath).size
    if (entry.bytes !== undefined && actualBytes !== entry.bytes) {
      die(`payload/${entry.name} 大小不符：期望 ${entry.bytes}，实际 ${actualBytes}`)
    }
    const actualHash = sha256(filePath)
    if (entry.sha256 && actualHash !== entry.sha256) {
      die(
        `payload/${entry.name} 校验和不符，文件可能在传输中损坏：\n` +
          `  期望 ${entry.sha256}\n  实际 ${actualHash}`
      )
    }
    ok(`payload/${entry.name}  ${humanBytes(actualBytes)}  校验通过`)
  }
}

function normalizeDatabase(workDir, manifest, overrides) {
  const source = path.join(workDir, 'payload', DB_FILE)
  const normalized = path.join(workDir, 'normalized.db')
  fs.copyFileSync(source, normalized)

  const walSource = path.join(workDir, 'payload', WAL_FILE)
  if (fs.existsSync(walSource)) fs.copyFileSync(walSource, normalized + '-wal')

  const { DatabaseSync } = require('node:sqlite')
  const db = new DatabaseSync(normalized)
  try {
    db.exec('PRAGMA wal_checkpoint(TRUNCATE)')

    const tables = new Set(
      db.prepare("SELECT name FROM sqlite_master WHERE type='table'").all().map((r) => r.name)
    )
    const missing = ['users', 'options'].filter((t) => !tables.has(t))
    if (missing.length > 0) {
      throw new Error(`不是有效的 new-api 数据库：缺少 ${missing.join('、')} 表`)
    }

    for (const [key, value] of Object.entries(overrides)) {
      const exists = db.prepare('SELECT COUNT(*) AS c FROM options WHERE key = ?').get(key).c
      if (exists) {
        db.prepare('UPDATE options SET value = ? WHERE key = ?').run(value, key)
        info(`覆盖配置 ${key} = ${value}`)
      } else {
        warn(`配置项 ${key} 在库中不存在，已跳过`)
      }
    }
    db.exec('PRAGMA wal_checkpoint(TRUNCATE)')
  } finally {
    db.close()
  }

  for (const leftover of [normalized + '-wal', normalized + '-shm']) {
    if (fs.existsSync(leftover)) fs.rmSync(leftover, { force: true })
  }

  const check = inspectDatabase(normalized)
  return { path: normalized, stats: check.stats, journalMode: check.journalMode }
}

function backupCurrent(target, backupDir) {
  const dir = path.resolve(backupDir)
  fs.mkdirSync(dir, { recursive: true })
  const stamp = new Date().toISOString().replace(/[:.]/g, '-')
  const outDir = path.join(dir, `${target.name}-${stamp}`)
  fs.mkdirSync(outDir, { recursive: true })

  helper(
    target.image,
    target.volume,
    [
      'set -e',
      `if [ -f ${DATA_DIR}/${DB_FILE} ]; then cp ${DATA_DIR}/${DB_FILE} /io/${DB_FILE}; fi`,
      `if [ -f ${DATA_DIR}/${WAL_FILE} ]; then cp ${DATA_DIR}/${WAL_FILE} /io/${WAL_FILE}; fi`,
      'echo BACKUP_OK',
    ].join('\n'),
    [`${mountPath(outDir)}:/io`]
  )

  const dbFile = path.join(outDir, DB_FILE)
  if (!fs.existsSync(dbFile)) return null

  let users = null
  try {
    users = countRows(dbFile, 'users')
  } catch {
    users = null
  }
  return { dir: outDir, bytes: fs.statSync(dbFile).size, users }
}

function restoreBackup(target, backup) {
  if (!backup || !backup.dir) return false
  helper(
    target.image,
    target.volume,
    [
      'set -e',
      `rm -f ${DATA_DIR}/${DB_FILE} ${DATA_DIR}/${WAL_FILE} ${DATA_DIR}/${SHM_FILE}`,
      `cp /io/${DB_FILE} ${DATA_DIR}/${DB_FILE}`,
      `if [ -f /io/${WAL_FILE} ]; then cp /io/${WAL_FILE} ${DATA_DIR}/${WAL_FILE}; fi`,
      'echo RESTORE_OK',
    ].join('\n'),
    [`${mountPath(backup.dir)}:/io`]
  )
  return true
}

function main() {
  const args = parseArgs(process.argv.slice(2))
  if (args.help || args.h) {
    usage()
    return
  }

  const archiveArg = args._[0]
  if (!archiveArg) {
    usage()
    die('缺少迁移包路径参数。')
  }
  const archivePath = path.resolve(archiveArg)
  if (!fs.existsSync(archivePath)) die(`找不到迁移包：${archivePath}`)

  console.log(paint(COLOR.bold, '\nSliderAPI 数据库迁移 · 导入'))
  console.log(paint(COLOR.dim, `  格式版本 ${FORMAT_VERSION} · 工具版本 ${TOOL_VERSION}`))

  step('1/7 解压并校验迁移包')
  ok(`迁移包：${archivePath}  ${humanBytes(fs.statSync(archivePath).size)}`)
  const staging = makeTempDir('sliderapi-import-')
  runTar(['-xzf', archivePath, '-C', staging])
  const manifest = readManifest(staging)
  ok(`格式版本 ${manifest.formatVersion}，导出于 ${manifest.createdAt}`)
  info(`来源主机：${manifest.source.hostname}`)
  info(`来源镜像：${manifest.source.image}`)
  verifyPayload(staging, manifest)

  step('2/7 定位目标容器')
  const containerName = detectContainer(args.container)
  const target = inspectContainer(containerName)
  ok(`容器：${target.name}`)
  info(`镜像：${target.image}`)
  info(`数据卷：${target.volume} → ${DATA_DIR}`)
  info(`当前状态：${target.running ? '运行中' : '已停止'}`)

  step('3/7 读取现有数据，评估覆盖风险')
  let existing = null
  if (target.running) {
    try {
      const probeDir = makeTempDir('sliderapi-probe-')
      helper(
        target.image,
        target.volume,
        [
          'set -e',
          `if [ -f ${DATA_DIR}/${DB_FILE} ]; then cp ${DATA_DIR}/${DB_FILE} /io/${DB_FILE}; fi`,
          `if [ -f ${DATA_DIR}/${WAL_FILE} ]; then cp ${DATA_DIR}/${WAL_FILE} /io/${WAL_FILE}; fi`,
          'echo PROBE_OK',
        ].join('\n'),
        [`${mountPath(probeDir)}:/io`]
      )
      const probeDb = path.join(probeDir, DB_FILE)
      if (fs.existsSync(probeDb)) {
        const stats = inspectDatabase(probeDb).stats
        existing = { users: stats.users, tokens: stats.tokens, channels: stats.channels }
      }
      fs.rmSync(probeDir, { recursive: true, force: true })
    } catch {
      existing = null
    }
  }

  if (existing) {
    info(`目标现有：users=${existing.users ?? '?'}  tokens=${existing.tokens ?? '?'}  channels=${existing.channels ?? '?'}`)
    if ((existing.users ?? 0) > 1) {
      warn('目标实例不是全新安装（已存在多个用户），导入会覆盖这些数据。')
    }
  } else {
    info('未能读取目标现有数据（容器可能未运行），将直接覆盖。')
  }

  info(`来源数据：users=${manifest.stats.users ?? '?'}  tokens=${manifest.stats.tokens ?? '?'}  channels=${manifest.stats.channels ?? '?'}`)
  if (manifest.options.ServerAddress) {
    warn(`来源 ServerAddress=${manifest.options.ServerAddress}，若新实例域名不同请用 --set-option 覆盖。`)
  }

  if (!args.yes) {
    if (!confirm('  确认用迁移包整体覆盖目标实例的数据库？')) die('已取消。')
  }

  step('4/7 归一化数据库（合并 WAL）')
  const overrides = {}
  if (args['set-option']) {
    for (const pair of String(args['set-option']).split(',')) {
      const eq = pair.indexOf('=')
      if (eq === -1) die(`--set-option 格式错误：${pair}（应为 KEY=VALUE）`)
      overrides[pair.slice(0, eq).trim()] = pair.slice(eq + 1).trim()
    }
  }
  let normalized
  try {
    normalized = normalizeDatabase(staging, manifest, overrides)
  } catch (error) {
    fs.rmSync(staging, { recursive: true, force: true })
    die('迁移包内容校验失败，未对目标实例做任何改动：\n  ' + error.message)
  }
  ok(`已归一为单文件，journal_mode=${normalized.journalMode}`)
  info(`users=${normalized.stats.users ?? '?'}  tokens=${normalized.stats.tokens ?? '?'}`)

  step('5/7 备份目标原库')
  let backup = null
  if (args['no-backup']) {
    warn('按 --no-backup 要求，跳过备份')
  } else {
    backup = backupCurrent(target, args['backup-dir'] || './sliderapi-backups')
    if (backup) {
      ok(`原库已备份到 ${backup.dir}`)
      info(`${humanBytes(backup.bytes)}，users=${backup.users ?? '?'}`)
    } else {
      info('目标无现有数据库文件，无需备份')
    }
  }

  step('6/7 替换数据库并启动')
  if (target.running) {
    docker(['stop', target.name])
    ok('容器已停止')
  } else {
    info('容器本就处于停止状态')
  }

  const placeDir = makeTempDir('sliderapi-place-')
  fs.copyFileSync(normalized.path, path.join(placeDir, DB_FILE))

  let placed = false
  try {
    helper(
      target.image,
      target.volume,
      [
        'set -e',
        `rm -f ${DATA_DIR}/${DB_FILE} ${DATA_DIR}/${WAL_FILE} ${DATA_DIR}/${SHM_FILE}`,
        `cp /io/${DB_FILE} ${DATA_DIR}/${DB_FILE}`,
        `chmod 644 ${DATA_DIR}/${DB_FILE}`,
        'echo PLACE_OK',
      ].join('\n'),
      [`${mountPath(placeDir)}:/io`]
    )
    placed = true
    ok('数据库已写入目标卷（旧 db/wal/shm 已清除）')
  } catch (error) {
    warn('写入失败：' + error.message)
  } finally {
    fs.rmSync(placeDir, { recursive: true, force: true })
  }

  if (!placed) {
    if (backup) {
      warn('尝试回滚…')
      restoreBackup(target, backup)
      docker(['start', target.name])
      ok('已回滚并启动原实例')
    }
    die('导入失败。')
  }

  if (args['keep-stopped']) {
    warn('按 --keep-stopped 要求，容器保持停止状态，请自行启动后再验证')
    fs.rmSync(staging, { recursive: true, force: true })
    return
  }

  docker(['start', target.name])
  const since = startedAt(target.name)
  ok('容器已启动，等待就绪…')

  step('7/7 验证')
  const startup = waitForStartup(target.name, since, 90000)
  if (!startup.ok) {
    warn('启动确认失败：' + startup.reason)
    const logs = logsSince(target.name, since).split('\n').slice(-25).join('\n')
    console.error(paint(COLOR.dim, logs))
    if (backup && !args.force) {
      warn('正在回滚到导入前的数据库…')
      docker(['stop', target.name])
      restoreBackup(target, backup)
      docker(['start', target.name])
      ok(`已回滚，原库位于 ${backup.dir}`)
      die('导入失败，已回滚。')
    }
    die('导入失败（--force 指定，未回滚）。')
  }
  ok('容器启动完成')

  const startupLogs = logsSince(target.name, since)
  const problems = startupLogs
    .split('\n')
    .filter((line) => /panic|fatal|failed to migrate|no such table|database is locked/i.test(line))
  if (problems.length > 0) {
    warn('启动日志中发现可疑记录：')
    problems.slice(0, 8).forEach((line) => console.error(paint(COLOR.dim, '    ' + line.trim())))
  } else {
    ok('启动日志未见异常')
  }

  let verifyUsers = null
  for (let attempt = 0; attempt < 3 && verifyUsers === null; attempt++) {
    try {
      const checkDir = makeTempDir('sliderapi-check-')
      helper(
        target.image,
        target.volume,
        [
          'set -e',
          `if [ -f ${DATA_DIR}/${DB_FILE} ]; then cp ${DATA_DIR}/${DB_FILE} /io/${DB_FILE}; fi`,
          `if [ -f ${DATA_DIR}/${WAL_FILE} ]; then cp ${DATA_DIR}/${WAL_FILE} /io/${WAL_FILE}; fi`,
          'echo CHECK_OK',
        ].join('\n'),
        [`${mountPath(checkDir)}:/io`]
      )
      const checkDb = path.join(checkDir, DB_FILE)
      if (fs.existsSync(checkDb)) verifyUsers = countRows(checkDb, 'users')
      fs.rmSync(checkDir, { recursive: true, force: true })
    } catch {
      sleep(2000)
    }
  }

  const expectedUsers = manifest.stats.users
  if (verifyUsers === null) {
    warn('无法读取导入后的数据库做复核，请手动登录确认')
  } else if (expectedUsers !== undefined && verifyUsers !== expectedUsers) {
    warn(`用户数复核不一致：期望 ${expectedUsers}，实际 ${verifyUsers}`)
    if (backup && !args.force) {
      warn('正在回滚…')
      docker(['stop', target.name])
      restoreBackup(target, backup)
      docker(['start', target.name])
      ok(`已回滚，原库位于 ${backup.dir}`)
      die('导入结果校验失败，已回滚。')
    }
  } else {
    ok(`用户数复核通过：${verifyUsers}`)
  }

  fs.rmSync(staging, { recursive: true, force: true })

  console.log('')
  ok('迁移完成')
  if (backup) info(`原库备份保留在：${backup.dir}`)
  if (manifest.options.ServerAddress && !overrides.ServerAddress) {
    info(`提示：当前 ServerAddress 为 ${manifest.options.ServerAddress}，若与本站域名不符请在后台「系统设置」中修改。`)
  }
  console.log('')
}

try {
  main()
} catch (error) {
  die('未预期的错误：' + (error && error.message ? error.message : String(error)))
}