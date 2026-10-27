'use strict'

/**
 * 机器可读的探测脚本：把目标容器与数据库概况以单行 JSON 输出到 stdout。
 *
 * 供 SliderAPI 控制台（console/server.js）调用，人类使用的 CLI 请用 export.js / import.js。
 * 设计约定：
 *   - stdout 只允许出现一行 JSON，其余诊断信息一律走 stderr；
 *   - 探测失败不抛出，而是把原因放进 JSON 的 note/error 字段，让控制台决定如何展示。
 */

const fs = require('node:fs')
const path = require('node:path')

const {
  DATA_DIR,
  DB_FILE,
  WAL_FILE,
  parseArgs,
  detectContainer,
  inspectContainer,
  helper,
  mountPath,
  inspectDatabase,
  makeTempDir,
} = require('./lib/common')

function probeDatabase(target) {
  const dir = makeTempDir('sliderapi-inspect-')
  try {
    helper(
      target.image,
      target.volume,
      [
        'set -e',
        `if [ -f ${DATA_DIR}/${DB_FILE} ]; then cp ${DATA_DIR}/${DB_FILE} /io/${DB_FILE}; fi`,
        `if [ -f ${DATA_DIR}/${WAL_FILE} ]; then cp ${DATA_DIR}/${WAL_FILE} /io/${WAL_FILE}; fi`,
        'echo INSPECT_OK',
      ].join('\n'),
      [`${mountPath(dir)}:/io`]
    )

    const dbFile = path.join(dir, DB_FILE)
    if (!fs.existsSync(dbFile)) {
      return { stats: null, options: null, note: '数据卷中尚无数据库文件（可能是全新实例）' }
    }
    const result = inspectDatabase(dbFile)
    return {
      stats: result.stats,
      options: {
        SystemName: result.options.SystemName ?? null,
        ServerAddress: result.options.ServerAddress ?? null,
      },
      note: null,
    }
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
}

function main() {
  const args = parseArgs(process.argv.slice(2))
  const containerName = detectContainer(args.container)
  const target = inspectContainer(containerName)

  let probe = { stats: null, options: null, note: null }
  let error = null
  try {
    probe = probeDatabase(target)
  } catch (err) {
    error = err && err.message ? err.message : String(err)
  }

  process.stdout.write(
    JSON.stringify({
      ok: true,
      container: target,
      stats: probe.stats,
      options: probe.options,
      note: probe.note,
      error,
    }) + '\n'
  )
}

try {
  main()
} catch (error) {
  process.stderr.write('inspect 失败：' + (error && error.message ? error.message : String(error)) + '\n')
  process.exit(1)
}