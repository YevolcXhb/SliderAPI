# SliderAPI 数据库迁移工具

把一台服务器上 new-api 实例的数据（用户、令牌、渠道、日志、订阅、设置）整体迁移到另一台服务器。

底层直接迁移 SQLite 数据库文件，**不依赖 new-api 的任何导出接口**（new-api 本身没有提供备份/导入功能）。

## 前置条件

- 宿主机已安装 **Docker**，且 `docker` 命令在 PATH 中
- 宿主机已安装 **Node.js 22 或更高版本**（用到了内置的 `node:sqlite`）
- 目标实例的数据目录 `/data` 必须是**命名卷（named volume）**挂载，而不是 bind mount
- 迁移工具目录（`migrate/`）在两台服务器上都可用

## 迁移流程

假设旧服务器容器名为 `new-api`，新服务器容器名也是 `new-api`。

### 第一步：在旧服务器上导出

```bash
node export.js
```

会在当前目录生成 `sliderapi-migration-<时间戳>.tar.gz`，并打印 sha256 校验和。

常用参数：

```bash
node export.js --container new-api      # 指定容器（默认自动检测）
node export.js --out /tmp               # 指定输出目录
node export.js --yes                    # 跳过停机确认
```

> 导出会**短暂停止容器**（通常数秒）。这是刻意设计：SQLite 处于 WAL 模式，直接拷贝 `.db` 文件会丢掉尚未落盘的已提交事务。正常停止时 SQLite 会自动 checkpoint 落盘，从而得到一致快照。导出结束后容器会按原状态重新启动。

### 第二步：把迁移包传到新服务器

```bash
scp sliderapi-migration-*.tar.gz user@new-server:/root/
```

### 第三步：在新服务器上导入

```bash
node import.js /root/sliderapi-migration-20261003-022539.tar.gz
```

脚本会依次：校验包完整性 → 读取目标现有数据 → 归一化数据库 → 备份目标原库 → 替换数据库并重启容器 → 验证用户数。

如果新服务器域名与旧服务器不同，用 `--set-option` 一并改掉：

```bash
node import.js /root/sliderapi-migration-*.tar.gz \
  --set-option ServerAddress=https://new.example.com
```

## 命令参考

### export.js

| 参数 | 说明 |
|---|---|
| `--container <名字>` | 指定 new-api 容器名，默认自动检测 |
| `--out <目录>` | 输出目录，默认当前目录 |
| `--name <文件名>` | 指定输出文件名 |
| `--yes` | 跳过停机确认 |
| `--keep-stopped` | 导出后不重新启动容器 |

### import.js

| 参数 | 说明 |
|---|---|
| `<迁移包路径>` | 必填 |
| `--container <名字>` | 指定 new-api 容器名，默认自动检测 |
| `--backup-dir <目录>` | 原库备份目录，默认 `./sliderapi-backups` |
| `--set-option K=V,...` | 导入后覆盖配置项，逗号分隔 |
| `--no-backup` | 跳过导入前自动备份（不推荐） |
| `--keep-stopped` | 导入后不启动容器 |
| `--yes` | 跳过所有确认提示 |
| `--force` | 校验异常时也强制导入、不自动回滚 |

## 设计要点

### 为什么必须处理 WAL

new-api 的 SQLite 默认开启 WAL 模式，`/data` 下会出现 `one-api.db-wal` 和 `one-api.db-shm`。未 checkpoint 的事务只存在于 `-wal` 文件里，只拷 `one-api.db` 会静默丢数据。

导出时通过「停止容器 → 拷贝 → 启动容器」拿到一致快照，并且会连同 `-wal` 一起拷贝（若停止后仍存在）。导入时会在宿主机上把 `-wal` 合并进主库（`PRAGMA wal_checkpoint(TRUNCATE)`），归一成单个 `.db` 文件后再写入目标卷，避免残留的旧 `-wal` 污染新库。

### 表结构兼容性

new-api 启动时会执行 `AutoMigrate` 和一系列幂等迁移，所以**旧版本的库导入新版本实例会自动升级表结构**，不需要手工改 schema。反向（新库导入旧版本实例）不受支持，脚本也不会做降级。

### 安全保障

- 导入前自动备份目标原库到 `--backup-dir`，文件名带时间戳
- 导入后校验用户数与清单是否一致，不一致会自动回滚并恢复原库
- 校验和比对失败、缺少 `users`/`options` 表等异常会在**触碰目标实例之前**中止
- 所有异常路径都不会留下半成品状态

### 容器名自动检测

脚本按「镜像名包含 new-api / one-api / sliderapi」+「运行中」打分选容器。若检测到多个候选，会列出并要求用 `--container` 明确指定，避免误操作。

### 实现细节

- 复用 new-api 镜像自身作为 helper 容器执行 `cp`/`rm`/`chmod`，**不需要额外拉取镜像**，也不挂载 docker.sock
- 容器名、数据卷名通过 `docker inspect` 解析，卷名不写死
- 迁移包为 `tar.gz`，内含 `manifest.json`（来源主机、镜像、表行数、各文件 sha256）和 `payload/` 目录

## 常见问题

**Q：导出一定要停机吗？**
是。当前实现以一致性优先。停机时间通常只有几秒。若你的旧服务器完全不能停机，需要改用 `VACUUM INTO` 做在线快照，这需要额外在服务器上拉取带 sqlite3 的镜像。

**Q：导入后用户需要重新登录吗？**
通常不需要。用户表连 `password` 哈希和 `auth_version` 一起迁移，登录态保持有效。

**Q：能只迁移用户、保留新实例的渠道和设置吗？**
当前不支持。只搬部分表需要重映射用户 ID、处理 `aff_code` 推荐关系和 `auth_version`，风险明显更高。若确有此需求需要单独实现。

**Q：迁移包能跨版本使用吗？**
可以，只要目标实例版本 **不低于** 源实例版本。建议两台服务器使用同一个镜像 tag。

**Q：导入后 `ServerAddress` 还是旧域名怎么办？**
两种方式：导入时加 `--set-option ServerAddress=https://新域名`，或导入后到后台「系统设置」里改。