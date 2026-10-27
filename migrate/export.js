'use strict'

const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')

const {
  FORMAT,
  FORMAT_VERSION,
  TOOL_VERSION,
  DATA_DIR,
  DB_FILE,
  WAL_FILE,
  PAYLOAD_FILES,
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
  assertLooksLikeNewApiDatabase,
  runTar,
  makeTempDir,
  confirm,
  logsSince,
  startedAt,
  sleep,
} = require('./lib/common')

function usage() {
  console.log(`
用法：node export.js [选项]

在【旧服务器】上运行，把 new-api 的 SQLite 数据库打成可迁移的压缩包。

选项：
  --container <名字>   指定 new-api 容器名（默认自动检测）
  --out <目录>         输出目录（默认当前目录）
  --name <文件名>      指定输出文件名（默认 sliderapi-migration-<时间戳>.tar.gz）
  --yes                跳过停机确认
  --keep-stopped       导出后不重新启动容器
  -h, --help           显示帮助

说明：
  导出过程会短暂停止容器（通常数秒），以保证 SQLite 快照一致。
  容器的启动/停止状态会在导出后恢复。
`)
}

function timestamp() {
  const d = new Date()
  const p = (n, w = 2) => String(n).padStart(w, '0')
  return (
    `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}` +
    `-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`
  )
}

function main() {
  const args = parseArgs(process.argv.slice(2))
  if (args.help || args.h) {
    usage()
    return
  }

  console.log(paint(COLOR.bold, '\nSliderAPI 数据库迁移 · 导出'))
  console.log(paint(COLOR.dim, `  格式版本 ${FORMAT_VERSION} · 工具版本 ${TOOL_VERSION}`))

  step('1/6 定位目标容器')
  const containerName = detectContainer(args.container)
  const target = inspectContainer(containerName)
  ok(`容器：${target.name}`)
  info(`镜像：${target.image}`)
  info(`数据卷：${target.volume} → ${DATA_DIR}`)
  info(`当前状态：${target.running ? '运行中' : '已停止'}`)

  if (!args.yes) {
    warn('导出需要短暂停止容器以保证 SQLite 快照一致（WAL 模式下直接拷贝文件会丢数据）。')
    if (!confirm('  确认继续？')) die('已取消。')
  }

  step('2/6 停止容器，获取一致快照')
  if (target.running) {
    docker(['stop', target.name], { inherit: false })
    ok('容器已停止')
  } else {
    info('容器本就处于停止状态，无需停止')
  }

  const staging = makeTempDir('sliderapi-export-')
  const payloadDir = path.join(staging, 'payload')
  fs.mkdirSync(payloadDir, { recursive: true })

  let snapshotOk = false
  try {
    helper(
      target.image,
      target.volume,
      [
        'set -e',
        `test -f ${DATA_DIR}/${DB_FILE} || { echo "缺少 ${DB_FILE}" >&2; exit 3; }`,
        `cp ${DATA_DIR}/${DB_FILE} /io/payload/${DB_FILE}`,
        `if [ -f ${DATA_DIR}/${WAL_FILE} ]; then cp ${DATA_DIR}/${WAL_FILE} /io/payload/${WAL_FILE}; fi`,
        'echo SNAPSHOT_OK',
      ].join('\n'),
      [`${mountPath(payloadDir)}:/io/payload`]
    )
    snapshotOk = true
    ok('数据库快照完成')
  } finally {
    if (target.running && !args['keep-stopped']) {
      docker(['start', target.name])
      const since = startedAt(target.name)
      let ready = false
      for (let i = 0; i < 40; i++) {
        if (/started/i.test(logsSince(target.name, since))) {
          ready = true
          break
        }
        sleep(1000)
      }
      if (ready) ok('容器已重新启动')
      else warn('容器已启动，但未在日志中确认到启动完成标志，请留意')
    } else if (args['keep-stopped']) {
      warn('按 --keep-stopped 要求，容器保持停止状态')
    }
  }

  if (!snapshotOk) die('快照失败，未生成迁移包。')

  step('3/6 校验快照完整性')
  const dbPath = path.join(payloadDir, DB_FILE)
  const walPath = path.join(payloadDir, WAL_FILE)
  const walIncluded = fs.existsSync(walPath)
  if (!fs.existsSync(dbPath)) die('快照中缺少数据库文件。')
  ok(`${DB_FILE}  ${humanBytes(fs.statSync(dbPath).size)}`)
  if (walIncluded) {
    ok(`${WAL_FILE}  ${humanBytes(fs.statSync(walPath).size)}（含未落盘事务）`)
  } else {
    info('无 WAL 文件（停止时已正常 checkpoint 落盘）')
  }

  step('4/6 读取数据库内容')
  const inspectCopy = path.join(staging, 'inspect.db')
  fs.copyFileSync(dbPath, inspectCopy)
  if (walIncluded) fs.copyFileSync(walPath, inspectCopy + '-wal')

  let inspected
  try {
    inspected = inspectDatabase(inspectCopy)
  } catch (error) {
    die(`无法读取数据库，快照可能已损坏：${error.message}`)
  }
  assertLooksLikeNewApiDatabase(inspected.stats, '快照中的数据库')
  ok(`共 ${inspected.tableCount} 张表，journal_mode=${inspected.journalMode}`)

  const highlight = ['users', 'tokens', 'channels', 'redemptions', 'subscription_plans']
  for (const table of highlight) {
    if (inspected.stats[table] !== undefined) {
      info(`${table}: ${inspected.stats[table]}`)
    }
  }

  step('5/6 生成清单')
  const files = []
  for (const name of PAYLOAD_FILES) {
    const p = path.join(payloadDir, name)
    if (fs.existsSync(p)) {
      files.push({ name, bytes: fs.statSync(p).size, sha256: sha256(p) })
    }
  }

  const manifest = {
    format: FORMAT,
    formatVersion: FORMAT_VERSION,
    toolVersion: TOOL_VERSION,
    createdAt: new Date().toISOString(),
    source: {
      hostname: os.hostname(),
      container: target.name,
      image: target.image,
      dataDir: DATA_DIR,
      volume: target.volume,
    },
    database: {
      engine: 'sqlite',
      journalMode: inspected.journalMode,
      files,
    },
    stats: inspected.stats,
    options: {
      SystemName: inspected.options.SystemName ?? null,
      ServerAddress: inspected.options.ServerAddress ?? null,
    },
  }

  const manifestPath = path.join(staging, 'manifest.json')
  fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + '\n', 'utf8')
  ok('manifest.json 已生成')
  info(`users=${inspected.stats.users ?? '?'}  channels=${inspected.stats.channels ?? '?'}`)
  info(`SystemName=${manifest.options.SystemName ?? '(未设置)'}`)
  info(`ServerAddress=${manifest.options.ServerAddress ?? '(未设置)'}`)

  step('6/6 打包')
  const outDir = path.resolve(args.out || process.cwd())
  fs.mkdirSync(outDir, { recursive: true })
  const archiveName = args.name || `sliderapi-migration-${timestamp()}.tar.gz`
  const archivePath = path.join(outDir, archiveName)
  if (fs.existsSync(archivePath)) die(`目标文件已存在：${archivePath}`)

  const entries = ['manifest.json', ...files.map((f) => path.join('payload', f.name))]
  runTar(['-czf', archivePath, '-C', staging, ...entries])

  const archiveSize = fs.statSync(archivePath).size
  console.log('')
  ok(`迁移包已生成：${archivePath}`)
  ok(`大小：${humanBytes(archiveSize)}`)
  ok(`校验和（sha256）：${sha256(archivePath)}`)

  console.log(
    paint(
      COLOR.bold,
      '\n下一步：把该文件传到新服务器，在新服务器上执行 node import.js <文件路径>\n'
    )
  )

  fs.rmSync(staging, { recursive: true, force: true })
}

try {
  main()
} catch (error) {
  die('未预期的错误：' + (error && error.message ? error.message : String(error)))
}