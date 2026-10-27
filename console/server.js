'use strict';

/**
 * SliderAPI 控制台 —— 零依赖 Node 服务
 *
 * 职责只有两件事：
 *   1. 托管 new-api 官方前端的构建产物（public/ = new-api/web 的 dist）
 *   2. 把 /api/*、/pg/*、/v1/*、/mj/* 反向代理到 new-api 后端
 *
 * 为什么要反代而不是前端直接跨域调用：
 *   new-api 的 CORS 是 AllowAllOrigins=true + AllowCredentials=true 的非法组合，
 *   浏览器会拒绝任何带 Cookie 的跨域请求，refresh Cookie 也无法携带。
 *   同源反代后 Cookie、PATCH、SessionCookieOriginGuard 全部正常工作。
 *
 * /pg/* 是操练场专用通道（router/relay-router.go:64，UserAuth 鉴权、非 API 密钥），
 * 它属于 relay 路由、未挂 gzip，因此流式 SSE 可以逐块透传，不会因压缩而攒包。
 *
 * 前端是 TanStack Router 的多路由 SPA（/console、/keys、/system-settings/site/$section …），
 * 所以任何**非静态资源**的路径都必须回落到 index.html，由前端路由接管；
 * 但 /api、/v1、/pg、/mj 这些前缀要走代理分支，/static 走静态资源分支，都不能回落。
 *
 * 此外还挂了数据迁移控制台（/migrate 页面 + /migrate/api 接口），
 * 它调用宿主机上的 migrate/ 工具，鉴权复用 new-api 的管理员会话。
 */

const http = require('node:http');
const https = require('node:https');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { MigrationRunner } = require('./migrate-runner');

/** 轻量 .env 加载：只认 KEY=VALUE 与 # 注释，已存在的真实环境变量优先 */
(function loadDotEnv() {
  let text;
  try {
    text = fs.readFileSync(path.join(__dirname, '.env'), 'utf8');
  } catch {
    return;
  }
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const eq = line.indexOf('=');
    if (eq < 0) continue;
    const key = line.slice(0, eq).trim();
    let value = line.slice(eq + 1).trim();
    if (
      value.length >= 2 &&
      ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))
    ) {
      value = value.slice(1, -1);
    }
    if (key && process.env[key] === undefined) process.env[key] = value;
  }
})();

const PORT = Number(process.env.PORT || 3000);
const HOST = process.env.HOST || '0.0.0.0';
const UPSTREAM = (process.env.NEWAPI_BASE || 'http://localhost:3001').replace(/\/+$/, '');
const PUBLIC_DIR = path.join(__dirname, 'public');

const upstream = new URL(UPSTREAM);
const upstreamClient = upstream.protocol === 'https:' ? https : http;
const upstreamPort = upstream.port || (upstream.protocol === 'https:' ? 443 : 80);

/** 需要原样透传给 new-api 后端的前缀 */
const PROXY_PREFIXES = ['/api', '/pg', '/v1', '/mj'];

// —— 数据迁移（见 migrate/README.md）——
// 迁移工具跑在宿主机上（要 docker 权限），所以接口挂在控制台而不是 new-api 容器内。
const MIGRATE_DIR = process.env.MIGRATE_DIR || path.join(__dirname, '..', 'migrate');
const MIGRATE_DATA_DIR = process.env.MIGRATE_DATA_DIR || path.join(__dirname, 'migrate-data');
const MIGRATE_PAGE = path.join(__dirname, 'migrate-ui.html');
const MIGRATE_TOKEN = process.env.MIGRATE_TOKEN || '';
const MAX_UPLOAD_BYTES = Number(process.env.MIGRATE_MAX_UPLOAD || 2 * 1024 * 1024 * 1024);
const PACKAGE_EXT_RE = /\.(tar\.gz|tgz)$/i;

const runner = new MigrationRunner({ migrateDir: MIGRATE_DIR, dataDir: MIGRATE_DATA_DIR });

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.otf': 'font/otf',
  '.eot': 'application/vnd.ms-fontobject',
  '.map': 'application/json; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
  '.webmanifest': 'application/manifest+json',
};

function sendJson(res, status, payload) {
  const body = Buffer.from(JSON.stringify(payload));
  res.writeHead(status, {
    'content-type': 'application/json; charset=utf-8',
    'content-length': body.length,
    'cache-control': 'no-store',
  });
  res.end(body);
}

function proxy(req, res) {
  const headers = Object.assign({}, req.headers, { host: upstream.host });

  const proxyReq = upstreamClient.request(
    {
      protocol: upstream.protocol,
      hostname: upstream.hostname,
      port: upstreamPort,
      method: req.method,
      path: req.url,
      headers,
    },
    (proxyRes) => {
      res.writeHead(proxyRes.statusCode || 502, proxyRes.headers);
      proxyRes.pipe(res);
    }
  );

  proxyReq.on('error', (err) => {
    if (res.headersSent) {
      res.end();
      return;
    }
    sendJson(res, 502, {
      success: false,
      message: `无法连接上游 new-api（${UPSTREAM}）：${err.message}`,
    });
  });

  req.pipe(proxyReq);
}

/** 带扩展名视为静态资源；否则视为前端路由，需要回落 index.html */
function looksLikeAsset(pathname) {
  return path.extname(pathname) !== '';
}

function serveFile(res, filePath, stat) {
  const ext = path.extname(filePath).toLowerCase();
  const isHtml = ext === '.html';
  // 带内容哈希的构建产物（/static/js/xxx.<hash>.js）可长缓存；
  // index.html 绝不缓存，否则刷新拿不到新版本
  const immutable = /[\\/]static[\\/].+[.-][A-Za-z0-9_-]{8,}\.(js|css)$/.test(filePath);
  const etag = '"' + stat.size.toString(16) + '-' + stat.mtimeMs.toString(16) + '"';

  if (!immutable && !isHtml) {
    res.setHeader('etag', etag);
    res.setHeader('cache-control', 'no-cache');
  } else if (immutable) {
    res.setHeader('cache-control', 'public, max-age=31536000, immutable');
  }

  fs.readFile(filePath, (readErr, buf) => {
    if (readErr) {
      sendJson(res, 404, { success: false, message: 'Not Found' });
      return;
    }
    const body = buf;
    res.writeHead(200, {
      'content-type': MIME[ext] || 'application/octet-stream',
      'content-length': body.length,
      ...(isHtml ? { 'cache-control': 'no-store' } : {}),
    });
    res.end(body);
  });
}

function sendIndex(res) {
  const index = path.join(PUBLIC_DIR, 'index.html');
  fs.stat(index, (err, stat) => {
    if (err) {
      sendJson(res, 500, {
        success: false,
        message: '控制台未构建：public/index.html 不存在。请先在 new-api/web 执行 npm run build 并拷贝 dist。',
      });
      return;
    }
    serveFile(res, index, stat);
  });
}

function serveStatic(req, res) {
  let pathname;
  try {
    pathname = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
  } catch {
    sendJson(res, 400, { success: false, message: '非法请求路径' });
    return;
  }

  const filePath = path.join(PUBLIC_DIR, pathname);
  const rel = path.relative(PUBLIC_DIR, filePath);
  if (rel.startsWith('..') || path.isAbsolute(rel)) {
    sendJson(res, 403, { success: false, message: '禁止访问' });
    return;
  }

  fs.stat(filePath, (err, stat) => {
    if (!err && stat.isFile()) {
      serveFile(res, filePath, stat);
      return;
    }
    // 目录或不存在的路径：
    //   - 看起来像静态资源（有扩展名）却不存在的 → 直接 404，避免把 .js/.css 请求喂成 HTML
    //   - 其余是前端路由（/console、/keys/xxx）→ 回落 index.html
    if (looksLikeAsset(pathname)) {
      sendJson(res, 404, { success: false, message: 'Not Found' });
      return;
    }
    sendIndex(res);
  });
}

/* ============================================================
   数据迁移控制台
   ------------------------------------------------------------
   页面：GET /migrate（自包含单页，主题沿用 deepseek-theme.css）
   接口：/migrate/api/*
   鉴权：复用 new-api 的管理员会话（Bearer access token → /api/user/self，role>=10），
         另可用 MIGRATE_TOKEN 提供脚本化访问。
   ============================================================ */

function serveMigratePage(res) {
  fs.readFile(MIGRATE_PAGE, (err, buf) => {
    if (err) {
      sendJson(res, 500, {
        success: false,
        message: '迁移页面缺失：console/migrate-ui.html 不存在。',
      });
      return;
    }
    res.writeHead(200, {
      'content-type': 'text/html; charset=utf-8',
      'content-length': buf.length,
      'cache-control': 'no-store',
    });
    res.end(buf);
  });
}

function decodeHeaderValue(value) {
  if (!value) return '';
  const raw = String(value);
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

function timingSafeEqualStr(a, b) {
  const left = Buffer.from(String(a));
  const right = Buffer.from(String(b));
  if (left.length !== right.length) {
    crypto.timingSafeEqual(left, left);
    return false;
  }
  return crypto.timingSafeEqual(left, right);
}

function bearerToken(req) {
  const match = /^Bearer\s+(.+)$/i.exec(String(req.headers.authorization || '').trim());
  return match ? match[1].trim() : '';
}

const adminCache = new Map();

/** 用浏览器会话换来的 access token 去 new-api 校验身份，要求 role>=10（管理员） */
function verifyAdminToken(token) {
  return new Promise((resolve) => {
    if (!token) {
      resolve({ ok: false, reason: 'missing' });
      return;
    }
    const cached = adminCache.get(token);
    if (cached && cached.expires > Date.now()) {
      resolve(cached.value);
      return;
    }

    const req = upstreamClient.request(
      {
        protocol: upstream.protocol,
        hostname: upstream.hostname,
        port: upstreamPort,
        method: 'GET',
        path: '/api/user/self',
        headers: {
          host: upstream.host,
          authorization: `Bearer ${token}`,
          accept: 'application/json',
        },
      },
      (upstreamRes) => {
        const chunks = [];
        upstreamRes.on('data', (chunk) => chunks.push(chunk));
        upstreamRes.on('end', () => {
          let value = { ok: false, reason: 'invalid' };
          try {
            const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
            const role = body && body.data ? Number(body.data.role) : NaN;
            if (body && body.success === true && Number.isFinite(role) && role >= 10) {
              value = { ok: true, role, username: body.data.username };
            } else if (body && body.success === true) {
              value = { ok: false, reason: 'insufficient', role: Number.isFinite(role) ? role : null };
            }
          } catch {
            /* 上游返回非 JSON，按无效处理 */
          }
          if (adminCache.size > 500) adminCache.clear();
          adminCache.set(token, { expires: Date.now() + (value.ok ? 60000 : 5000), value });
          resolve(value);
        });
      }
    );
    req.on('error', () => resolve({ ok: false, reason: 'upstream' }));
    req.end();
  });
}

async function requireAdmin(req, res) {
  if (MIGRATE_TOKEN) {
    const provided = String(req.headers['x-migrate-token'] || '');
    if (provided && timingSafeEqualStr(provided, MIGRATE_TOKEN)) return true;
  }
  const result = await verifyAdminToken(bearerToken(req));
  if (result.ok) return true;
  sendJson(res, result.reason === 'insufficient' ? 403 : 401, {
    success: false,
    code: 'MIGRATE_AUTH_REQUIRED',
    message:
      result.reason === 'insufficient'
        ? '需要管理员权限才能使用数据迁移。'
        : '登录状态已失效，请重新登录控制台。',
  });
  return false;
}

function readJsonBody(req, limit = 64 * 1024) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > limit) {
        reject(new Error('请求体过大'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      if (chunks.length === 0) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString('utf8')));
      } catch {
        reject(new Error('请求体不是合法 JSON'));
      }
    });
    req.on('error', reject);
  });
}

/** 把上传的迁移包流式写入磁盘，避免把大文件整个读进内存 */
function receiveUpload(req, dir, rawName) {
  return new Promise((resolve, reject) => {
    const name = runner.uniqueName(dir, rawName);
    const dest = path.join(dir, name);
    const ws = fs.createWriteStream(dest);
    let size = 0;
    let settled = false;

    const fail = (err) => {
      if (settled) return;
      settled = true;
      ws.destroy();
      try {
        fs.rmSync(dest, { force: true });
      } catch {
        /* 清理失败不影响错误上报 */
      }
      req.destroy();
      reject(err);
    };

    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_UPLOAD_BYTES) fail(new Error('迁移包超过大小上限'));
    });
    req.on('error', fail);
    ws.on('error', fail);
    ws.on('finish', () => {
      if (settled) return;
      settled = true;
      resolve({ path: dest, name, bytes: size });
    });

    req.pipe(ws);
  });
}

function servePackage(res, kind, name) {
  const full = runner.packagePath(kind, name);
  if (!full) {
    sendJson(res, 404, { success: false, message: '文件不存在' });
    return;
  }
  fs.stat(full, (err, stat) => {
    if (err || !stat.isFile()) {
      sendJson(res, 404, { success: false, message: '文件不存在' });
      return;
    }
    res.writeHead(200, {
      'content-type': 'application/gzip',
      'content-length': stat.size,
      'content-disposition': `attachment; filename="${path.basename(full)}"`,
      'cache-control': 'no-store',
    });
    fs.createReadStream(full).pipe(res);
  });
}

async function handleMigrateApi(req, res, pathname) {
  if (!(await requireAdmin(req, res))) return;

  const rest = pathname.slice('/migrate/api'.length).replace(/^\/+/, '');
  const segs = rest ? rest.split('/').map((s) => decodeURIComponent(s)) : [];

  if (req.method === 'GET' && segs.length === 1 && segs[0] === 'env') {
    sendJson(res, 200, {
      success: true,
      toolAvailable: runner.toolAvailable(),
      migrateDir: MIGRATE_DIR,
      activeJob: runner.active(),
      packages: runner.listPackages(),
    });
    return;
  }

  if (req.method === 'POST' && segs.length === 1 && segs[0] === 'inspect') {
    let body = {};
    try {
      body = await readJsonBody(req);
    } catch {
      body = {};
    }
    const container = body && body.container ? String(body.container).trim() : '';
    sendJson(res, 200, await runner.inspect(container));
    return;
  }

  if (req.method === 'POST' && segs.length === 1 && segs[0] === 'export') {
    let body = {};
    try {
      body = await readJsonBody(req);
    } catch {
      body = {};
    }
    try {
      const job = runner.startExport({
        container: body && body.container ? String(body.container).trim() : '',
        keepStopped: Boolean(body && body.keepStopped),
      });
      sendJson(res, 200, { success: true, job });
    } catch (err) {
      sendJson(res, 409, { success: false, message: err.message });
    }
    return;
  }

  if (req.method === 'POST' && segs.length === 1 && segs[0] === 'import') {
    const rawName = decodeHeaderValue(req.headers['x-file-name']);
    if (!PACKAGE_EXT_RE.test(rawName)) {
      sendJson(res, 400, { success: false, message: '只接受 .tar.gz / .tgz 迁移包' });
      return;
    }
    try {
      const saved = await receiveUpload(req, runner.importsDir, rawName);
      const job = runner.startImport({
        filePath: saved.path,
        fileName: saved.name,
        container: decodeHeaderValue(req.headers['x-container']).trim(),
        setOption: decodeHeaderValue(req.headers['x-set-option']).trim(),
        keepStopped: req.headers['x-keep-stopped'] === '1',
        force: req.headers['x-force'] === '1',
      });
      sendJson(res, 200, { success: true, job });
    } catch (err) {
      sendJson(res, 400, { success: false, message: err.message });
    }
    return;
  }

  if (req.method === 'GET' && segs.length === 2 && segs[0] === 'jobs') {
    const job = runner.get(segs[1]);
    if (!job) {
      sendJson(res, 404, { success: false, message: '任务不存在' });
      return;
    }
    sendJson(res, 200, { success: true, job });
    return;
  }

  if (segs[0] === 'packages') {
    if (req.method === 'GET' && segs.length === 1) {
      sendJson(res, 200, { success: true, packages: runner.listPackages() });
      return;
    }
    if (segs.length === 3) {
      if (req.method === 'GET') {
        servePackage(res, segs[1], segs[2]);
        return;
      }
      if (req.method === 'DELETE') {
        const removed = runner.deletePackage(segs[1], segs[2]);
        sendJson(res, removed ? 200 : 404, {
          success: removed,
          message: removed ? '已删除' : '文件不存在',
        });
        return;
      }
    }
  }

  sendJson(res, 404, { success: false, message: '未知的迁移接口' });
}

const server = http.createServer((req, res) => {
  const pathname = (req.url || '/').split('?')[0];

  if (pathname === '/healthz') {
    sendJson(res, 200, { ok: true, upstream: UPSTREAM, port: PORT });
    return;
  }

  if (pathname === '/migrate' || pathname === '/migrate/') {
    serveMigratePage(res);
    return;
  }

  if (pathname === '/migrate/api' || pathname.startsWith('/migrate/api/')) {
    handleMigrateApi(req, res, pathname).catch((err) => {
      if (!res.headersSent) {
        sendJson(res, 500, { success: false, message: '迁移接口异常：' + err.message });
      } else {
        res.end();
      }
    });
    return;
  }

  if (PROXY_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + '/'))) {
    proxy(req, res);
    return;
  }

  serveStatic(req, res);
});

server.listen(PORT, HOST, () => {
  console.log(`[SliderAPI] 控制台已启动: http://localhost:${PORT}`);
  console.log(`[SliderAPI] 数据迁移: http://localhost:${PORT}/migrate`);
  console.log(`[SliderAPI] 迁移工具: ${MIGRATE_DIR}${runner.toolAvailable() ? '' : '（未找到，迁移功能不可用）'}`);
  console.log(`[SliderAPI] 上游 new-api: ${UPSTREAM}`);
  console.log(`[SliderAPI] 前端根目录: ${PUBLIC_DIR}`);
});
