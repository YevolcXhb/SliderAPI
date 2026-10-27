'use strict';

/**
 * 迁移任务运行器 —— 把 migrate/ 下的 CLI 包装成控制台可调用的后台任务。
 *
 * 为什么不把 export.js / import.js 的逻辑搬进来：
 *   那两个脚本依赖 stdin 确认、TTY 配色、process.exit，是为终端设计的。
 *   控制台只需要"起任务、看日志、拿结果"，所以这里以子进程方式调用它们，
 *   迁移算法与终端版本保持同一份实现，避免两处逻辑漂移。
 *
 * 同一时刻只允许一个任务：export 会停容器、import 会换数据库，
 * 并发跑必然互相破坏。第二个请求直接拒绝。
 */

const { spawn } = require('node:child_process');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const MAX_JOBS = 20;
const MAX_LOG_LINES = 4000;
const INSPECT_TIMEOUT_MS = 30000;

function timestamp() {
  const d = new Date();
  const p = (n) => String(n).padStart(2, '0');
  return (
    `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}` +
    `-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`
  );
}

/** 只保留文件名部分，去掉路径分隔符与控制字符 */
function sanitizeBaseName(name) {
  const base = path.basename(String(name || '').replace(/\\/g, '/'));
  return base.replace(/[^\w.\-]+/g, '_').replace(/^\.+/, '');
}

function isPackageName(name) {
  return /^[\w.\-]+\.(tar\.gz|tgz)$/i.test(name);
}

class MigrationRunner {
  constructor({ migrateDir, dataDir }) {
    this.migrateDir = migrateDir;
    this.dataDir = dataDir;
    this.exportsDir = path.join(dataDir, 'exports');
    this.importsDir = path.join(dataDir, 'imports');
    this.backupsDir = path.join(dataDir, 'backups');
    for (const dir of [this.exportsDir, this.importsDir, this.backupsDir]) {
      fs.mkdirSync(dir, { recursive: true });
    }
    this.jobs = new Map();
    this.order = [];
    this.activeId = null;
  }

  toolAvailable() {
    return (
      fs.existsSync(path.join(this.migrateDir, 'export.js')) &&
      fs.existsSync(path.join(this.migrateDir, 'import.js')) &&
      fs.existsSync(path.join(this.migrateDir, 'inspect.js'))
    );
  }

  dirFor(kind) {
    if (kind === 'exports') return this.exportsDir;
    if (kind === 'imports') return this.importsDir;
    return null;
  }

  /** 生成一个不会与现有文件冲突的包名 */
  uniqueName(dir, preferred) {
    const safe = sanitizeBaseName(preferred) || `sliderapi-migration-${timestamp()}.tar.gz`;
    const ext = safe.toLowerCase().endsWith('.tgz') ? '.tgz' : '.tar.gz';
    const stem = safe.slice(0, safe.length - ext.length) || 'sliderapi-migration';
    let candidate = stem + ext;
    let i = 1;
    while (fs.existsSync(path.join(dir, candidate))) {
      candidate = `${stem}-${i}${ext}`;
      i += 1;
    }
    return candidate;
  }

  listPackages() {
    const out = [];
    for (const [kind, dir] of [
      ['exports', this.exportsDir],
      ['imports', this.importsDir],
    ]) {
      let entries = [];
      try {
        entries = fs.readdirSync(dir);
      } catch {
        continue;
      }
      for (const name of entries) {
        if (!isPackageName(name)) continue;
        try {
          const stat = fs.statSync(path.join(dir, name));
          if (!stat.isFile()) continue;
          out.push({ kind, name, bytes: stat.size, mtime: stat.mtimeMs });
        } catch {
          /* 文件在读取间隙被删除，忽略 */
        }
      }
    }
    return out.sort((a, b) => b.mtime - a.mtime);
  }

  packagePath(kind, name) {
    const dir = this.dirFor(kind);
    if (!dir) return null;
    const safe = sanitizeBaseName(name);
    if (!isPackageName(safe)) return null;
    const full = path.join(dir, safe);
    if (path.dirname(full) !== dir) return null;
    return fs.existsSync(full) ? full : null;
  }

  deletePackage(kind, name) {
    const full = this.packagePath(kind, name);
    if (!full) return false;
    fs.rmSync(full, { force: true });
    return true;
  }

  serialize(job) {
    if (!job) return null;
    return {
      id: job.id,
      kind: job.kind,
      status: job.status,
      createdAt: job.createdAt,
      finishedAt: job.finishedAt,
      exitCode: job.exitCode,
      container: job.container || null,
      sourceFile: job.sourceFile || null,
      result: job.result || null,
      error: job.error || null,
      logs: job.logs,
    };
  }

  active() {
    return this.activeId ? this.serialize(this.jobs.get(this.activeId)) : null;
  }

  get(id) {
    return this.serialize(this.jobs.get(id));
  }

  #append(job, stream, chunk) {
    for (const line of String(chunk).split(/\r?\n/)) {
      if (line === '') continue;
      job.logs.push({ t: Date.now(), s: stream, m: line });
    }
    if (job.logs.length > MAX_LOG_LINES) {
      job.logs.splice(0, job.logs.length - MAX_LOG_LINES);
    }
  }

  #lastErrorLine(job) {
    for (let i = job.logs.length - 1; i >= 0; i -= 1) {
      if (job.logs[i].s === 'err' && job.logs[i].m.trim()) return job.logs[i].m.trim();
    }
    return null;
  }

  #record(kind, extra) {
    const job = {
      id: crypto.randomBytes(8).toString('hex'),
      kind,
      status: 'running',
      createdAt: Date.now(),
      finishedAt: null,
      exitCode: null,
      logs: [],
      result: null,
      error: null,
      child: null,
      ...extra,
    };
    this.jobs.set(job.id, job);
    this.order.push(job.id);
    while (this.order.length > MAX_JOBS) {
      this.jobs.delete(this.order.shift());
    }
    this.activeId = job.id;
    return job;
  }

  #spawnJob(job, args) {
    const child = spawn(process.execPath, args, {
      cwd: this.migrateDir,
      env: Object.assign({}, process.env, { FORCE_COLOR: '0', NO_COLOR: '1' }),
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    job.child = child;

    child.stdout.on('data', (chunk) => this.#append(job, 'out', chunk));
    child.stderr.on('data', (chunk) => this.#append(job, 'err', chunk));

    child.on('error', (err) => {
      job.status = 'failed';
      job.error = job.error || err.message;
      job.finishedAt = job.finishedAt || Date.now();
      job.child = null;
      if (this.activeId === job.id) this.activeId = null;
    });

    child.on('close', (code) => {
      job.exitCode = code;
      job.finishedAt = Date.now();
      job.child = null;
      if (job.status === 'running') {
        job.status = code === 0 ? 'success' : 'failed';
      }
      if (job.status === 'failed' && !job.error) {
        job.error = this.#lastErrorLine(job) || `迁移脚本以退出码 ${code} 结束`;
      }
      if (this.activeId === job.id) this.activeId = null;
    });

    return this.serialize(job);
  }

  startExport({ container, keepStopped } = {}) {
    if (!this.toolAvailable()) throw new Error('未找到迁移工具（migrate/export.js），无法导出');
    if (this.activeId) throw new Error('已有迁移任务正在运行，请等待其结束');
    const name = this.uniqueName(this.exportsDir, `sliderapi-migration-${timestamp()}.tar.gz`);
    const job = this.#record('export', { container: container || null });
    job.result = { archiveName: name, kind: 'exports', downloadUrl: null };
    const args = ['export.js', '--yes', '--out', this.exportsDir, '--name', name];
    if (container) args.push('--container', container);
    if (keepStopped) args.push('--keep-stopped');
    job.result.downloadUrl = `/migrate/api/packages/exports/${encodeURIComponent(name)}`;
    return this.#spawnJob(job, args);
  }

  startImport({ filePath, fileName, container, setOption, keepStopped, force } = {}) {
    if (!this.toolAvailable()) throw new Error('未找到迁移工具（migrate/import.js），无法导入');
    if (this.activeId) throw new Error('已有迁移任务正在运行，请等待其结束');
    const job = this.#record('import', { container: container || null, sourceFile: fileName || null });
    const args = ['import.js', filePath, '--yes', '--backup-dir', this.backupsDir];
    if (container) args.push('--container', container);
    if (setOption) args.push('--set-option', setOption);
    if (keepStopped) args.push('--keep-stopped');
    if (force) args.push('--force');
    return this.#spawnJob(job, args);
  }

  /** 探测目标容器与数据库概况，返回解析后的 JSON（失败时给出可读原因） */
  inspect(container) {
    return new Promise((resolve) => {
      const script = path.join(this.migrateDir, 'inspect.js');
      if (!fs.existsSync(script)) {
        resolve({ ok: false, error: '未找到 migrate/inspect.js' });
        return;
      }
      const args = ['inspect.js'];
      if (container) args.push('--container', container);

      const child = spawn(process.execPath, args, {
        cwd: this.migrateDir,
        env: Object.assign({}, process.env, { FORCE_COLOR: '0', NO_COLOR: '1' }),
        stdio: ['ignore', 'pipe', 'pipe'],
      });

      let stdout = '';
      let stderr = '';
      let settled = false;
      const finish = (value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        resolve(value);
      };
      const timer = setTimeout(() => {
        child.kill();
        finish({ ok: false, error: `探测超时（${INSPECT_TIMEOUT_MS / 1000}s）` });
      }, INSPECT_TIMEOUT_MS);

      child.stdout.on('data', (chunk) => {
        stdout += chunk;
      });
      child.stderr.on('data', (chunk) => {
        stderr += chunk;
      });
      child.on('error', (err) => finish({ ok: false, error: err.message }));
      child.on('close', (code) => {
        const line = stdout.trim().split(/\r?\n/).filter(Boolean).pop() || '';
        try {
          finish(JSON.parse(line));
        } catch {
          finish({ ok: false, error: stderr.trim() || `inspect 以退出码 ${code} 结束` });
        }
      });
    });
  }
}

module.exports = { MigrationRunner };