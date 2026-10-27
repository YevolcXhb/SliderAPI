# SliderAPI 项目瘦身分析报告

> 生成时间：2026-10-01
> 分析对象：`C:\Users\32927\Desktop\SliderAPI`（当前磁盘占用约 **1.19 GB**）
> 分析目的：识别可删除的冗余、无用、用不上的内容，给出分级删除建议与风险提示

---

## 一、现状盘点

| 目录 | 大小 | 性质 | 是否线上必需 |
| --- | --- | --- | --- |
| `new-api/web/node_modules` | 1091.3 MB | 前端构建依赖 | ❌ 否（仅构建时需要） |
| `new-api/web/dist` | 63.8 MB | 前端构建产物 | ⚠️ 源产物（已拷贝到 console/public） |
| `new-api/web/src` | 12.4 MB | 前端源码 | ⚠️ 改前端才需要 |
| `new-api/.git` | 6.7 MB | 上游 git 历史 | ❌ 否 |
| `console/public/static` | ~64 MB | 实际线上静态资源 | ✅ **是** |
| `new-api`（Go 源码） | ~9 MB | 后端源码 | ⚠️ 打后端二进制才需要 |
| `console-backup-20260930-214247` | ~0.4 MB | 自研控制台备份 | ❌ 否 |
| `locales-backup` | ~2 MB | 语言包备份 | ❌ 否 |
| `ANALYSIS.md` | 0 KB（空文件） | 早期分析残留 | ❌ 否 |

**核心结论**：真正的线上必需内容只有 **`console/` 目录（server.js + public/）**，约 70 MB。
其余 1.1 GB 全是"构建链"产物。当前运行时（`node server.js`）**完全不读** `new-api/web/node_modules` 和 `new-api/web/dist`。

---

## 二、可删清单（按推荐程度分级）

### 🟢 第一级：确定可删，零风险（合计约 1098 MB）

| # | 对象 | 大小 | 理由 |
| --- | --- | --- | --- |
| 1 | `new-api/web/node_modules/` | **1091.3 MB** | 只在 `npm run build` 时需要。当前运行完全不读。删除后想重新构建前端再 `npm install` 即可（已验证可用 `--legacy-peer-deps` 重装） |
| 2 | `console-backup-20260930-214247/` | ~0.4 MB | 自研控制台备份，已被官方前端取代，确认不再回滚即可删 |
| 3 | `ANALYSIS.md` | 0 KB | 空文件，早期残留 |
| 4 | `new-api/.git/` | 6.7 MB | 上游 git 历史。若不打算 pull/提交上游代码可删（**若想保留升级能力则保留**） |

### 🟡 第二级：可删但需确认（合计约 76 MB）

| # | 对象 | 大小 | 风险 | 说明 |
| --- | --- | --- | --- | --- |
| 5 | `new-api/web/dist/` | 63.8 MB | 低 | 已完整拷贝到 `console/public/`。删了不影响运行，但下次改前端要重新 build。**保留则多一层本地备份** |
| 6 | `locales-backup/` | ~2 MB | 低 | 语言包改品牌名前的备份。改动已验证通过，可删 |
| 7 | `new-api/e2e/` | <0.1 MB | 无 | 端到端测试脚手架，本项目不跑 |
| 8 | `new-api/electron/` | 0.2 MB | 无 | 桌面客户端封装，本项目不用 |

### 🔵 第三级：可精简（不影响运行，只影响"能否改后端/前端"）

| # | 对象 | 大小 | 说明 |
| --- | --- | --- | --- |
| 9 | 后端 `*_test.go` | 3.2 MB / 279 个 | Go 测试文件。删了不影响 `go build`，但失去回归能力 |
| 10 | 前端 `*.test.ts(x)` | ~164 个 | 前端测试。删了不影响 build |
| 11 | `new-api/docs/` | 0.5 MB | 上游文档 |
| 12 | `new-api/.github/` | 0.1 MB | CI 配置，本项目不用 GitHub Actions |

### 🔴 不建议删（动了会出事）

| 对象 | 原因 |
| --- | --- |
| `console/server.js`、`console/public/**` | **线上运行的核心** |
| `console/sync-dist.js` | 重新构建前端后的同步脚本 |
| `console/public/deepseek-theme.css` | DeepSeek 配色覆盖层，删了配色失效 |
| `new-api/web/src/` | 前端源码，删了无法再改前端（品牌名、文案等） |
| `new-api/{controller,model,service,router,relay,...}` | 后端源码，删了无法再编译 |
| `new-api/web/dist/`（若打算保留备份） | 见第二级说明 |

---

## 三、"功能级"可删项（不只是文件，是功能）

以下是从**产品功能**角度的建议，与上面"文件级"独立。

### 3.1 前端可隐藏的功能（改代码即可，不动后端）

| 功能 | 入口 | 建议 | 理由 |
| --- | --- | --- | --- |
| 模型广场 / 排行榜 | 顶部导航 | 可隐藏 | 面向多租户 SaaS，自用场景用不上 |
| 文档 / 关于 | 顶部导航 | **保留**（AGPL 署名要求） | 见法律说明 |
| 任务插件市场 | 任务插件页 | 可隐藏 | 需外网拉取插件 |
| Loop / Chat 高级编排 | 侧栏 | 可隐藏 | 复杂，自用很少碰 |
| 多语言切换 | 顶栏 | 可精简为「中/英」 | 当前 7 语言，翻译维护成本高 |

### 3.2 后端可裁剪的功能（需改 Go 代码 + 重新编译，成本较高）

| 功能 | 相关文件 | 说明 |
| --- | --- | --- |
| 视频/绘图任务（Suno/Kling/Sora/Veo/Vidu…） | `plugins/`、`relay/`、`task*.go` | 体量大，若不做视频业务可裁 |
| WeChat / Telegram / LinuxDO / OIDC / Discord OAuth | `oauth/`、`controller/oauth.go` | 若只用账号密码登录可裁 |
| Waffo / Creem / Stripe 支付渠道 | `controller/topup_*.go` | 若只做余额充值可裁 |
| Passkey / 2FA | `controller/passkey.go` | 若要简化登录可裁 |
| 部署管理（io.net GPU 部署） | `controller/deployment.go` | 属于实验特性 |

> ⚠️ **重要提醒**：后端裁剪需重新编译 Go 二进制，且会失去向上游同步升级的能力。**除非明确要长期自维护，否则不建议动后端。**

---

## 四、法律与合规红线（不可删）

以下内容受 **AGPL v3.0 许可证**约束，属于原项目署名，**删除即违约**：

| 位置 | 内容 |
| --- | --- |
| 前端页脚 / 关于页 | `© 2026 New API` 及其 GitHub 仓库链接 |
| 关于页 | "基于 One API © 2023 JustSong" |
| 关于页 | "此项目的使用必须遵守 AGPL v3.0 协议" |
| 源码文件头 | `Copyright (C) 2023-2026 QuantumNous` 许可证声明块 |
| 构建产物 | `*.LICENSE.txt`（39 个，第三方依赖许可证） |

**另注**：若 SliderAPI 对外提供服务（而非纯自用），AGPL 要求**开放修改后的源码**。建议保留 `new-api/` 源码目录与 `dist` 中的 LICENSE 文件。

---

## 五、推荐执行方案

### 方案 A：激进瘦身（省 ~1.1 GB，适合确定不再改前端）

```powershell
# 1. 删前端构建依赖（最大头，省 1091 MB）
Remove-Item -Recurse -Force 'new-api\web\node_modules'

# 2. 删各类备份与残留
Remove-Item -Recurse -Force 'console-backup-20260930-214247'
Remove-Item -Recurse -Force 'locales-backup'
Remove-Item -Force 'ANALYSIS.md'

# 3. 可选：删上游 git 历史（放弃 pull 能力）
# Remove-Item -Recurse -Force 'new-api\.git'

# 4. 可选：删源 dist（已拷贝到 console/public）
# Remove-Item -Recurse -Force 'new-api\web\dist'
```

**结果**：约 1.19 GB → **约 90 MB**（保留源码以便日后改前端）

### 方案 B：保守瘦身（省 ~1.09 GB，保留一切重建能力）

只执行第 1、2 步（删 `node_modules` + 备份 + 空文件），保留 `.git` 与 `dist`。

**结果**：约 1.19 GB → **约 100 MB**

### 方案 C：不动文件，只隐藏前端入口

保持磁盘占用不变，仅通过修改前端源码隐藏不用的导航项（模型广场、排行榜、插件市场等），重新构建部署。

---

## 六、待你决策的问题

1. **今后还会不会改前端源码？**（影响是否删 `node_modules` / `web/src`）
   - 会 → 建议保留 `node_modules`（或接受每次 `npm install` 约 3 分钟）
   - 不会 → 可全删
2. **是否需要保留向 new-api 上游同步升级的能力？**（影响是否删 `.git`）
3. **SliderAPI 是自用还是对外提供服务？**（影响 AGPL 义务与可裁剪范围）
4. **是否做视频/绘图类业务？**（影响 `plugins/` 与任务系统是否可裁）

---

## 附：当前运行链（说明为什么能删那么多）

```
浏览器
  ↓ http://localhost:3210
console/server.js                    ← 唯一运行时进程
  ├─ 静态托管 console/public/**      ← 官方前端构建产物（+ DeepSeek 主题）
  └─ 反代 /api /v1 /pg /mj  → localhost:3000（new-api 二进制）
                                   ↑
                        new-api/web/node_modules  ← 与运行完全无关
                        new-api/web/dist          ← 源产物，已被拷贝
                        new-api/web/src           ← 仅构建时读取
```

`node_modules` 和 `dist` 只在「构建」阶段参与，`server.js` 运行时不读它们。
