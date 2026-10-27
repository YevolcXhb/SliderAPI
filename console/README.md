# SliderAPI 控制台

基于 [new-api](https://github.com/QuantumNous/new-api) **公开 REST API** 实现的精简控制台。
用户侧提供**API 密钥**、**订阅/套餐计费**、**充值**、**操练场**、**用量日志**、**审计日志**、**任务/绘图日志**、**个人资料**、**安全与访问**；
管理员登录后侧栏额外出现**管理端**分组（数据看板 / 渠道 / 模型 / 用户 / 兑换码 / 订阅 / 充值订单 / 日志 / 系统信息 / 系统设置），按角色显示。
侧栏分组（对话 / 通用 / 个人 / 管理端）与菜单项逐一对齐 new-api 前端的 `use-sidebar-data`。

UI 采用 DeepSeek Web 端的蓝白配色风格，设计令牌直接从其线上样式表提取。

---

## 目录结构

```
SliderAPI/
├── new-api/          # 上游源码克隆（仅作接口契约参考，不修改）
└── console/          # 精简控制台
    ├── server.js     # 零依赖 Node 服务：静态托管 + /api 反向代理
    ├── package.json
    ├── .env.example
    ├── public/
    │   ├── index.html
    │   ├── styles.css
    │   ├── app.js              # 主应用：登录、概览、操练场、密钥、套餐、充值
    │   ├── usage.js            # 通用组：用量日志
    │   ├── audit.js            # 通用组：审计日志（用户 / 管理员共用）
    │   ├── task-logs.js        # 通用组：任务日志 + 绘图日志
    │   ├── profile.js          # 个人组：个人资料 + 安全与访问
    │   ├── admin.js            # 管理端：用户 / 渠道 / 兑换码 / 系统信息 / 系统设置
    │   ├── admin-dashboard.js  # 管理端：数据看板 + 日志管理
    │   └── admin-ops.js        # 管理端：模型管理 / 订阅管理 / 充值订单
    └── README.md
```

视图采用注册表模式解耦：用户侧视图写入 `window.UserViews`，管理端视图写入 `window.AdminViews`，
`app.js` 的 `renderView()` 按 `state.view` 分发；每个视图的最低角色声明在 `window.AdminViewRoles` 中。

---

## 为什么需要反向代理

new-api 的 CORS 配置是 `AllowAllOrigins = true` + `AllowCredentials = true` 的组合。
按规范，浏览器会**拒绝**任何携带 Cookie 的此类跨域请求，因此：

- 前端直接跨域调用 → refresh Cookie 无法携带，`access_token` 15 分钟一过就必须重新输密码
- 跨域 `PATCH /api/subscription/admin/plans/:id` → 预检失败（`PATCH` 不在 `AllowMethods` 白名单）

`server.js` 把 `/api/*` 反代到 new-api，使控制台与后端**同源**，上述问题全部消失，且无需改动 new-api 一行代码。

---

## 快速开始

前置：new-api 已在 `http://localhost:3000` 运行。

```powershell
cd console
$env:PORT="3210"
$env:NEWAPI_BASE="http://localhost:3000"
node server.js
```

打开 <http://localhost:3210>，用 new-api 的账号密码登录。

### 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `3000` | 控制台监听端口 |
| `HOST` | `0.0.0.0` | 监听地址，改 `127.0.0.1` 可限制仅本机 |
| `NEWAPI_BASE` | `http://localhost:3001` | 上游 new-api 地址 |
| `MIGRATE_DIR` | `../migrate` | 数据迁移工具目录 |
| `MIGRATE_DATA_DIR` | `./migrate-data` | 导出包 / 上传包 / 导入前备份的存放目录 |
| `MIGRATE_TOKEN` | 空 | 可选静态令牌，用于脚本化访问 `/migrate/api` |
| `MIGRATE_MAX_UPLOAD` | `2147483648` | 上传迁移包大小上限（字节） |

`server.js` 不读取 `.env` 文件，请用上面的方式注入环境变量（或在系统里设置）。

---

## 已实现功能

### 1. API 密钥管理

| 能力 | 对应接口 |
| --- | --- |
| 密钥列表（分页） | `GET /api/token/?p=&page_size=` |
| 按名称/密钥搜索 | `GET /api/token/search?keyword=` |
| 查看完整密钥（点击脱敏值自动复制） | `POST /api/token/:id/key` |
| 新建密钥 | `POST /api/token/` |
| 编辑密钥 | `PUT /api/token/` |
| 启用 / 禁用 | `PUT /api/token/?status_only=1` |
| 删除密钥 | `DELETE /api/token/:id` |
| 批量删除（勾选后操作） | `POST /api/token/batch` |
| 批量复制密钥（勾选后一键复制全部完整密钥） | `POST /api/token/batch/keys` |

创建/编辑支持：名称、不限额度或指定额度（USD 输入自动换算）、永不过期或指定天数、
分组、模型白名单、IP 白名单。

列表首列为勾选框，勾选后顶部出现批量操作条（批量复制密钥 / 批量删除 / 取消选择）；
表头勾选框可全选本页。`/api/token/batch/keys` 单次上限 100 个 ID。

### 2. 订阅 / 套餐计费

| 能力 | 对应接口 |
| --- | --- |
| 套餐商店 | `GET /api/subscription/plans` |
| 我的订阅（生效中 + 全部记录） | `GET /api/subscription/self` |
| 扣费优先级设置 | `PUT /api/subscription/self/preference` |
| 余额购买套餐 | `POST /api/subscription/balance/pay` |
| 套餐管理（管理员） | `GET/POST /api/subscription/admin/plans`、`PUT/PATCH /api/subscription/admin/plans/:id` |

### 3. 充值与账户

| 能力 | 对应接口 |
| --- | --- |
| 账户概览（余额、已用、请求数、分组） | `GET /api/user/self` |
| 充值配置 | `GET /api/user/topup/info` |
| 兑换码充值 | `POST /api/user/topup` |
| 在线支付下单 | `POST /api/user/pay` |
| 充值记录 | `GET /api/user/topup/self` |

### 4. 认证

- 密码登录 `POST /api/user/login`，支持 new-api 的 RSA-OAEP 密码加密模式（自动探测 `GET /api/user/login/encryption-key`）
- 二次验证 `POST /api/user/login/verify`
- 令牌自动续期 `POST /api/user/auth/refresh`（Cookie 同源可用）
- 退出登录 `POST /api/user/auth/logout`

### 5. 用量日志（`usage.js`）

| 能力 | 对应接口 |
| --- | --- |
| 用量明细（分页 + 类型/时间/模型/令牌筛选） | `GET /api/log/self?p=&page_size=&type=&start_timestamp=&end_timestamp=&model_name=&token_name=` |
| 区间统计（消费额度 + 近 60 秒 RPM / TPM） | `GET /api/log/self/stat` |
| 按天 / 按模型的聚合用量（趋势图 + 模型排行） | `GET /api/data/self?start_timestamp=&end_timestamp=` |

趋势图与排行榜用纯 CSS 柱状条渲染，不引入图表库。`/api/data/self` 的时间跨度上限为 1 个月（后端限制）。

### 6. 审计日志（`audit.js`）

new-api 把审计日志放在「通用」分组，前端按当前角色切接口，控制台沿用同一入口：

| 能力 | 对应接口 |
| --- | --- |
| 全站审计（管理员） | `GET /api/audit?p=&page_size=&category=&username=&success=&start_timestamp=&end_timestamp=` |
| 本人审计（普通用户） | `GET /api/audit/self?p=&page_size=&category=&success=&start_timestamp=&end_timestamp=` |

`category` 取值与后端常量一一对应：`login`（登录）、`security`（安全）、`operation`（管理操作）、`access_token`（访问令牌）；
`success` 只接受 `true` / `false` 字面量，传其它值后端直接 400。`username` 仅在管理员接口生效（用户侧接口只返回本人记录）。
列表展示时间、分类、操作者（含 IP）、操作内容与动作、HTTP 方法、结果（成功 / 失败 + 状态码）。

### 7. 任务日志 / 绘图日志（`task-logs.js`）

对齐 new-api 侧栏「任务日志」入口（其 `activeUrls` 同时涵盖绘图日志），用分段控件在同一个视图里切换：

| 能力 | 对应接口 |
| --- | --- |
| 任务日志（管理员 / 用户） | `GET /api/task` / `GET /api/task/self` |
| 绘图日志（管理员 / 用户） | `GET /api/mj/` / `GET /api/mj/self` |

公共查询参数 `p` / `page_size` / `start_timestamp` / `end_timestamp`；任务日志额外支持 `platform` / `task_id` / `status` / `action`，绘图日志额外支持 `mj_id`。

**两条链路返回结构完全不同，必须分开渲染**：

- 任务日志返回 `dto.TaskDto`，字段为 `task_id` / `platform` / `username` / `created_at` / `finish_time` / `quota` / `action` / `status` / `progress` / `result_url` / `fail_reason`，`properties` 是对象（`{ input, origin_model_name }`）。
- 绘图日志返回 `model.Midjourney`，字段为 `mj_id` / `submit_time` / `finish_time` / `image_url` / `video_url` / `prompt` / `action` / `status` / `progress` / `fail_reason` / `quota`，`properties` 是字符串；管理员接口**不带** `username`，只能显示 `#user_id`。

状态枚举两边一致：`NOT_START` / `QUEUED` / `SUBMITTED` / `IN_PROGRESS` / `SUCCESS` / `FAILURE` / `UNKNOWN`。
日志里的时间戳缺省显示「—」，不复用 `formatTime()`（它对 0 返回「永不过期」，那是给密钥过期时间用的）。

### 8. 个人资料 + 安全与访问（`profile.js`）

对齐 new-api「个人」组的两个独立入口：个人资料只留账号资料，密码 / 2FA / Passkey / PAT / 会话全部移到「安全与访问」。

| 能力 | 对应接口 |
| --- | --- |
| 账号资料读取 / 更新显示名称 | `GET` / `PUT /api/user/self` |
| 修改密码 | `PUT /api/user/self` + `X-Security-Proof` |
| 登录会话列表 / 下线单个 / 下线其他设备 | `GET /api/user/sessions`、`DELETE /api/user/sessions/:sid`、`POST /api/user/sessions/revoke-others` |
| 两步验证（TOTP）状态 / 开启 / 关闭 / 备用码 | `GET /api/user/2fa/status`、`POST /api/user/2fa/setup\|enable\|disable\|backup_codes` |
| Passkey（WebAuthn）注册 / 查询 / 删除 | `GET` / `POST /api/user/passkey*`、`DELETE /api/user/passkey` |
| 个人访问令牌（PAT）状态 / 生成 / 撤销 | `GET /api/user/token/status`、`POST` / `DELETE /api/user/token` |
| 安全验证方式探测与换取凭证 | `GET /api/verify/methods`、`POST /api/verify` |

敏感操作统一走 `requestProof()`：先 `POST /api/verify` 换取 `proof_token`，再以 `X-Security-Proof` 请求头携带。
支持三种验证方式：账号密码、2FA 动态验证码、Passkey（浏览器端做 Base64URL ↔ ArrayBuffer 编解码后调用 `navigator.credentials`）。
会话轮换后前端会同步新的 `access_token`，避免后续请求 401。

### 9. 管理端（按角色显示）

侧栏的「管理端」分组仅在 `role >= 10` 时出现，菜单项再按 `data-role` 细分：管理员（10）看到数据看板、渠道、模型、用户、兑换码、订阅、充值订单、日志，超级管理员（100）额外看到系统信息、系统设置。视图通过 `window.AdminViews` 注册、`window.AdminViewRoles` 声明最低角色，`app.js` 的 `renderView()` 与 `renderShell()` 做双重校验（直接改 URL 也无法越权进入）。

| 模块 | 最低角色 | 对应接口 |
| --- | --- | --- |
| 数据看板 | 10 | `GET /api/data/`、`GET /api/data/users`、`GET /api/data/flow`、`GET /api/log/stat` |
| 渠道管理 | 10 | `GET /api/channel/`、`GET /api/channel/search`、`GET /api/channel/:id`、`POST/PUT /api/channel/`、`DELETE /api/channel/:id`、`GET /api/channel/test/:id`、`POST /api/channel/:id/status` |
| 渠道高级操作 | 10 | `POST /api/channel/copy/:id`、`GET /api/channel/update_balance[/:id]`、`POST /api/channel/batch`、`POST /api/channel/status/batch`、`POST /api/channel/batch/tag`、`DELETE /api/channel/disabled` |
| 模型管理 | 10 | `GET /api/models/`、`GET /api/models/search`、`GET /api/models/:id`、`POST/PUT /api/models/`、`DELETE /api/models/:id`、`GET /api/vendors/`、`GET /api/vendors/search`、`GET /api/vendors/:id`、`POST/PUT /api/vendors/`、`DELETE /api/vendors/:id` |
| 用户管理 | 10 | `GET /api/user/?p=&page_size=`、`GET /api/user/search`、`POST /api/user/`、`PUT /api/user/`、`DELETE /api/user/:id`、`POST /api/user/manage` |
| 兑换码 | 10 | `GET /api/redemption/`、`GET /api/redemption/:id`、`POST /api/redemption/`、`PUT /api/redemption/`、`DELETE /api/redemption/:id` |
| 订阅管理 | 10 | `GET/POST /api/subscription/admin/plans`、`PUT/PATCH /api/subscription/admin/plans/:id`、`GET /api/subscription/admin/users/:id/subscriptions` |
| 充值订单 | 10 | `GET /api/user/topup`、`POST /api/user/topup/complete` |
| 日志管理 | 10 | `GET /api/log/`、`GET /api/log/search`、`GET /api/log/stat` |
| 系统信息 | 100 | `GET /api/status`、`GET /api/system-info/instances`、`GET /api/log/stat` |
| 系统设置 | 100 | `GET /api/option/`、`PUT /api/option/` |

渠道列表首列为勾选框，勾选后出现批量操作条（设置标签 / 批量启用 / 批量禁用 / 批量删除 / 取消选择），
行内还提供「复制」（`POST /api/channel/copy/:id`，可自定义名称后缀）、「余额」（`GET /api/channel/update_balance/:id`）；
页面右上角提供「刷新全部余额」（`GET /api/channel/update_balance`）与「清理禁用渠道」（`DELETE /api/channel/disabled`）。

模型管理页内用分段控件切换「模型 / 供应商」两个子页（`/api/models/` 与 `/api/vendors/` 都以 `/` 结尾注册，
少写结尾斜杠会被 Gin 301 重定向）；充值订单支持管理员补单（`POST /api/user/topup/complete`）；
订阅管理可查看并重置指定用户的订阅。

分组列表来自 `GET /api/group/`，供用户/渠道表单的下拉选择使用。

> **静态资源缓存策略**：`index.html` 一律 `Cache-Control: no-store`（它是资源引用的入口，一旦被缓存就会继续
> 引用旧版本的 js/css，表现为「改了代码、刷新后仍看不到新菜单」）；`app.js` / `usage.js` / `audit.js` /
> `task-logs.js` / `profile.js` / `admin.js` / `admin-dashboard.js` / `admin-ops.js` / `styles.css` 走 ETag 协商缓存，
> 且 HTML 里会注入基于各文件 mtime 的 `?v=` 版本号。`assetVersion()` 在每次请求 HTML 时重新计算，因此**改动
> `public/` 下的文件无需重启 `server.js`**（只有改动 `server.js` 自身才需要重启）。

---

### 10. 应用骨架（照抄 new-api 布局）与侧栏收起 / 展开

**骨架直接照抄 new-api 的 `AuthenticatedLayout`**（`new-api/web/src/components/layout/components/authenticated-layout.tsx`）：

```
SidebarProvider(flex-col)
├── AppHeader        通栏 + sticky + h=3rem，第一个子元素是 SidebarTrigger，紧接着 SystemBrand
└── div.flex.min-h-0.w-full.flex-1
    ├── AppSidebar   侧栏（fixed，可收起为图标模式）
    └── SidebarInset 内容区，h = 100svh - header，min-h-0 overflow-hidden
```

落到 `index.html` 就是 `.app`（flex 列）→ `.app-header`（通栏顶栏）→ `.app-body`（flex 行）→ `.sidebar` + `.main`。
与 new-api 唯一的差异是定位手法：它的侧栏是 `fixed` + 一层占位 `gap`，我们用 `sticky` 子项 + `flex-basis`，
视觉结果一致但少一层结构。

三个必须记住的结构约定：

- **品牌在全局顶栏里，不在侧栏里**。new-api 的 `AppHeader` 第一个子元素是 `SidebarTrigger`，紧跟 `SystemBrand`；
  照此把 `#sidebar-toggle` 与 `#sidebar-brand` 都放进 `.app-header`，侧栏顶部不放 Logo。
- **顶栏高度** `--app-header-h: 48px`，与 new-api 的 `--app-header-height: 3rem` 完全一致。
- **页面标题条** `.page-head`（`#topbar-title` 由 `renderView()` 写入）在内容区顶部，不随内容滚动。
  操练场是唯一例外：`.app-flush` 下整条标题条隐藏（对齐 new-api 的 `<Main className='p-0'><Playground /></Main>`），
  高度全部留给对话区，并用 `height: 100svh` 避开移动端地址栏伸缩造成的跳动。

侧栏收起参照 new-api 前端（`web/src/components/ui/sidebar.tsx`）的 `collapsible: 'icon'` 实现，用原生 CSS 变量 + 少量 JS 复刻，未引入任何依赖。

收起后侧栏宽度由 `--sidebar-w`（208px = new-api `--sidebar-width: 13rem`）切到 `--sidebar-w-icon`（44px = new-api `2.75rem`），
只留 32px 的方形图标按钮，悬停用深色浮层补上文字。

| 交互入口 | 说明 |
| --- | --- |
| 顶栏切换按钮 | `#sidebar-toggle`，位于全局顶栏最左侧（对齐 new-api「`Header` 的第一个子元素就是 `SidebarTrigger`」）；收起后图标水平镜像，暗示点击方向 |
| 侧栏边缘热区 | `#sidebar-rail`，侧栏右缘 6px 隐形区，hover 出现 2px 蓝线，光标 `w-resize` / `e-resize`，点击切换 |
| 快捷键 | `Ctrl/Cmd + B`，与 new-api 的 `SIDEBAR_KEYBOARD_SHORTCUT` 一致 |
| 点击品牌区 | `#sidebar-brand`，可聚焦（`role="button"` + `tabindex="0"`，Enter/Space 同样生效） |

实现要点：

- **状态写在 `<html>` 上**。`html.sidebar-collapsed { --sidebar-w: var(--sidebar-w-icon); }` 一个变量同时驱动
  `.sidebar` 的 `width` 与 `flex-basis`，主内容区自动跟随，不会有重排抖动；过渡是 `width / flex-basis / padding` 的 0.2s。
- **首屏不闪动**。`index.html` 的 `<head>` 里有一段内联脚本，在渲染前读 `localStorage` 并加上 class，
  避免「先画出展开态再收起」。
- **持久化**用 `localStorage`（键 `sliderapi.sidebar.collapsed`，`'1'`/`'0'`）。这里不用 Cookie，
  因为控制台的状态本来就存在 `sessionStorage`/`localStorage`；隐私模式下写入抛错会被 catch，仅本次会话生效。
- **悬停浮层是 JS 摆放的单例节点**（`.nav-tip`，`position: fixed`）。不能用 CSS `::after` 气泡：
  `.nav` 有 `overflow-y: auto`，绝对定位的伪元素会被裁掉。
- **`.nav` 的滚动条始终隐藏**（`scrollbar-width: none` + `-ms-overflow-style: none` + `::-webkit-scrollbar`）。
  对齐 new-api：它的侧栏容器是 `no-scrollbar flex min-h-0 flex-1 flex-col overflow-auto`（`web/src/components/ui/sidebar.tsx`），
  滚动条由 `@utility no-scrollbar`（`web/src/styles/index.css`）彻底隐藏。Windows 默认滚动条会常驻显示、吃掉十几像素宽度，
  展开态看着突兀、收起态（36px 栏位）还会把图标挤歪。`overflow-y: auto` 保留，内容仍可用滚轮 / 触控板滚动。
  （new-api 收起态是 `group-data-[collapsible=icon]:overflow-hidden`，即完全不能滚动；我们保留滚动能力，
  因为我们的管理端菜单项比 new-api 多，图标模式下更需要能翻到底部条目。）
- **分组标题改为分隔线**：`.nav-group-title` 收起后没有横向空间，改用 `.nav-group` 的 `border-top` 区分用户侧 / 管理端。
- **余额卡片换成图标**：`.quota-box` 收起后隐藏，由 `.quota-mini` 顶替，`data-tip` 在 `renderShell()` 里写入金额。
- **收起态只影响侧栏**。品牌与账户区都在全局顶栏里，收起时原地不动，因此不需要「收起后隐藏品牌区」这类补丁。
- **移动端（≤720px）**：`.app-body` 改为列向，侧栏从「左侧竖排」变成「顶栏下方的一条横向图标条」（只留图标）；
  收起后整条图标条 `display: none`，只剩顶栏上的切换按钮负责重新展开。此时右缘热区（`.sidebar-rail`）关闭，避免在窄条上误触。

### 侧栏菜单（对齐 new-api `use-sidebar-data`）

| 分组 | 菜单项 | 视图 key | 可见性 |
| --- | --- | --- | --- |
| 对话 | 操练场 | `playground` | 全部 |
| 通用 | 概览 | `overview` | 全部 |
| 通用 | API 密钥 | `keys` | 全部 |
| 通用 | 用量日志 | `usage` | 全部 |
| 通用 | 审计日志 | `audit` | 全部（接口按角色切 `/api/audit` / `/api/audit/self`） |
| 通用 | 任务日志 | `task-logs` | 全部（含绘图日志，页内分段切换） |
| 个人 | 充值余额 | `billing` | 全部 |
| 个人 | 订阅套餐 | `plans` | 全部 |
| 个人 | 个人资料 | `profile` | 全部 |
| 个人 | 安全与访问 | `security` | 全部 |
| 管理端 | 数据看板 | `admin-dashboard` | `role >= 10` |
| 管理端 | 渠道管理 | `admin-channels` | `role >= 10` |
| 管理端 | 模型管理 | `admin-models` | `role >= 10`（含供应商子页） |
| 管理端 | 用户管理 | `admin-users` | `role >= 10` |
| 管理端 | 兑换码 | `admin-redemptions` | `role >= 10` |
| 管理端 | 订阅管理 | `admin-subs` | `role >= 10` |
| 管理端 | 充值订单 | `admin-topups` | `role >= 10` |
| 管理端 | 日志管理 | `admin-logs` | `role >= 10` |
| 管理端 | 系统信息 | `admin-system` | `role >= 100` |
| 管理端 | 系统设置 | `admin-settings` | `role >= 100` |

`VIEW_TITLE`（`app.js`）与上表一一对应，`renderView()` 先查 `window.UserViews` 再查 `window.AdminViews`，
因此审计日志 / 任务日志这类「一个入口覆盖两种角色」的视图只需注册在 `UserViews` 上。

---

## 设计令牌

从 `chat.deepseek.com` 的样式表提取，写入 `styles.css` 的 `:root`：

| 令牌 | 值 |
| --- | --- |
| 品牌主色 | `#4d6bfe` |
| 主色 hover / active | `#3f5bf0` / `#3450d8` |
| 浅蓝底 | `#f5f8ff` `#eaf3ff` `#e4edfd` `#d3e2ff` |
| 页面底色 | `#f9fafb` |
| 边框 | `#e5e5e5` / `#ebeef2` |
| 正文 / 次级 / 弱化文字 | `#0f1115` / `#545557` / `#7f8287` |
| 语义色 | 成功 `#22c55e`、警告 `#f59e0b`、危险 `#ef4444` |
| 圆角 | 10 / 13 / 16 / 22 px（见下方映射） |
| 控件高度 | 24 / 28 / 32 / 36 px |
| 字体 | `Inter, system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif` |
| 缓动 | `cubic-bezier(.4, 0, .2, 1)`，时长 `.2s` |

### 圆角体系（对齐 new-api）

new-api 的圆角来自 `web/src/styles/theme.css` 的 `--radius` 标度（基准 `--radius: 1rem`，其余档位按倍数推导）。
`styles.css` 把同一套标度固化成四个令牌，数值取整到整数像素：

| 我们的令牌 | 值 | new-api 对应 | 推导 | 用在哪些控件 |
| --- | --- | --- | --- | --- |
| `--ds-radius-sm` | 10px | `--radius-sm` | `×0.6` = 9.6px | 复选框、`kbd` 类小标签、代码片段 |
| `--ds-radius-md` | 13px | `--radius-md` | `×0.8` = 12.8px | 侧栏菜单项、下拉/选择器条目、tooltip、小按钮（`sm`/`xs`）、骨架屏 |
| `--ds-radius` | 16px | `--radius-lg` | `×1` = 16px | 按钮（默认/`lg`/图标）、输入框、文本域、`select`、alert、popover、消息气泡 |
| `--ds-radius-xl` | 22px | `--radius-xl` | `×1.4` = 22.4px | 卡片、内容浮层面板、弹窗、空态图标、套餐卡 |
| 胶囊 `999px` | — | `--radius-4xl` | `×2.6` = 41.6px | badge / 标签（41.6px 超过自身 20px 高度，实际渲染成胶囊） |

三条容易踩的约定：

- **小按钮要降档**。new-api 的 `buttonVariants` 只有 `default`/`lg`/`icon`/`icon-lg` 用 `rounded-lg`，
  `xs`/`sm`/`icon-xs`/`icon-sm` 一律降到 `rounded-md`。落到我们这里：`.btn-sm`、`.btn-xs` 以及 26~30px 的图标按钮
  （`.pg-tool`、`.pg-msg-act`、`.modal-close`）走 13px；`.icon-btn`、`.quota-mini` 是 32px 的 `icon` 档，保持 16px。
- **消息气泡是均匀圆角**。new-api 的 `ai-elements/message.tsx` 用统一的 `rounded-lg`，没有「一角收窄」的尾巴，
  因此 `.pg-msg-user .pg-bubble` 四角都是 16px。
- **内容浮层面板**对齐 `SidebarInset` 的 `md:peer-data-[variant=inset]:m-2 ml-0 rounded-xl shadow-sm`：
  即 `.main` 四周留 8px（左侧贴侧栏故为 0）、22px 圆角、一层轻阴影，画布色 `--ds-canvas` 比面板白底略深以显出分层。

导航图标取自 new-api 前端所用的同一图标库（`lucide-react@1.25.0` 对应的 `lucide-static`），以 inline SVG 内联，不引入任何运行时依赖：概览 `Activity`、API 密钥 `Key`、订阅套餐 `CreditCard`、充值余额 `Wallet`。因而是描边渲染，`.nav-item svg` 必须保持 `fill: none; stroke: currentColor`，不要改回 `fill: currentColor`（会让图标变成实心块）。

额度换算单位（`quota_per_unit`，默认 1 USD = 500,000 quota）从 `GET /api/status` 动态读取，未硬编码。

---

## 已知限制

1. **Turnstile 人机验证未实现**。若 new-api 开启了 `turnstile_check`，登录需要额外的 Turnstile token，本控制台会登录失败。默认关闭。
2. **Passkey 依赖安全上下文**。WebAuthn 要求 `https` 或 `localhost`；通过 IP 访问控制台时 Passkey 注册/验证不可用，密码与 2FA 方式不受影响。
3. **支付合规未确认时无充值入口**。`payment_compliance_confirmed = false` 时 new-api 会关闭全部充值入口（含兑换码），控制台会给出提示。
4. **额度展示依赖 `display_in_currency`**。为 `false` 时按 quota 数值展示，否则按 USD 展示。
5. 套餐删除未实现 —— new-api 管理端只提供「上下架」(`PATCH enabled`) 与编辑，没有删除接口。
6. **多密钥渠道不支持余额查询**（`/api/channel/update_balance/:id` 会返回失败），控制台会原样提示后端错误信息。
7. 渠道「批量设置标签」的标签值以弹窗输入，留空表示清除标签。
8. **绘图日志的管理员视图只能显示用户 ID**。`GET /api/mj/` 返回的是 `model.Midjourney`，其中只有 `user_id`、没有 `username`（任务日志的 `TaskDto` 才会回填 `username`），因此管理员看到的是 `#user_id` 而不是用户名。
9. 审计日志的「按操作者筛选」只在管理员接口生效；用户侧 `/api/audit/self` 不接受 `username` 参数，控制台在该角色下不渲染该输入框。

---

## 数据迁移控制台

把 `migrate/` 下的命令行迁移工具接进了控制台，入口是 **`/migrate`**，
登录后在 `/console`、`/system-settings` 等控制台区域右下角会出现「数据迁移」入口。

为什么接口挂在控制台而不是 new-api 容器内：迁移要操作宿主机 Docker（停容器、读写命名卷、跑 helper 容器），
只有宿主机的 Node 进程有权限，`server.js` 正是跑在宿主机上的那一个。

### 页面能力

| 区块 | 说明 |
| --- | --- |
| 当前实例 | 自动探测 new-api 容器、镜像、数据卷、运行状态，并列出 users/tokens/channels 等关键表行数 |
| 导出 | 在**旧服务器**上打包数据库，完成后可直接下载 `.tar.gz` 迁移包 |
| 导入 | 在**新服务器**上上传迁移包并整体覆盖，导入前自动备份、失败自动回滚 |
| 任务日志 | 实时滚动迁移脚本的 stdout/stderr，结束后可直接下载产物 |
| 迁移包 | 列出导出包与上传包，支持下载 / 删除 |

### 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/migrate` | 迁移页面 |
| GET | `/migrate/api/env` | 工具可用性、进行中的任务、迁移包列表 |
| POST | `/migrate/api/inspect` | 探测容器与数据库概况（body: `{container?}`） |
| POST | `/migrate/api/export` | 发起导出任务（body: `{container?, keepStopped?}`） |
| POST | `/migrate/api/import` | 上传并导入，body 为迁移包二进制，文件名走 `x-file-name` 头 |
| GET | `/migrate/api/jobs/:id` | 查询任务状态与日志 |
| GET | `/migrate/api/packages` | 迁移包列表 |
| GET / DELETE | `/migrate/api/packages/:kind/:name` | 下载 / 删除迁移包（`kind` 为 `exports` 或 `imports`） |

### 鉴权

默认复用 new-api 的管理员会话：页面先用 refresh Cookie 换取 access token，接口再用该 token 调
`/api/user/self` 校验 `role >= 10`，非管理员返回 403。若需脚本化调用，可设置 `MIGRATE_TOKEN`，
请求携带 `x-migrate-token` 头即可。

### 与命令行工具的关系

页面并不重写迁移逻辑，而是以子进程方式调用 `migrate/export.js`、`migrate/import.js`、`migrate/inspect.js`，
迁移算法与终端版本是同一份实现。同一时刻只允许一个任务，避免并发停容器 / 换库互相破坏。
命令行用法与迁移包格式见 `migrate/README.md`。