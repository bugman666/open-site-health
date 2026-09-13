# Open Site Health

开源的站点健康监控：盯住公益站点、开源文档站和志愿项目官网是否可用、证书会不会过期，异常时发告警。

适合没有专职运维、又不能让捐赠入口或报名页静默挂掉的小团队。本仓库只做自托管，核心功能不设付费墙。

## 它做什么

1. 登记要监控的 URL
2. 按周期探测可用性，并检查 HTTPS 证书过期时间
3. 异常时通过邮件或 Webhook 通知（带去重，避免同一故障刷屏）

当前可以登记监控 URL，并按周期探测可用性与证书（[#1](https://github.com/bugman666/open-site-health/issues/1)、[#2](https://github.com/bugman666/open-site-health/issues/2)）。告警仍是占位模块（[#3](https://github.com/bugman666/open-site-health/issues/3)）。

## 谁会用

- 小型公益组织的站点维护者
- 开源项目官网 / 文档站维护者
- 志愿活动报名页、信息公开页的负责人

## 为什么要有服务端

探测和告警需要一直跑，不能指望有人开着浏览器：

- 定时对已登记地址做 HTTP(S) 检查
- 读取证书过期时间并提前预警
- 投递邮件 / Webhook，并保存近期结果

### 起步环境（参考）

| 项 | 建议 |
|----|------|
| OS | Linux（Ubuntu 22.04+ / Debian 12+） |
| CPU | 1 vCPU |
| 内存 | 512 MB–1 GB |
| 磁盘 | 约 10 GB（系统 + 日志 + 轻量存储） |
| 网络 | 能访问被监控站点，以及你选用的邮件/Webhook 服务 |

初期大约 50–200 个 URL、每 5 分钟一轮就够用；负载大致随「目标数 × 探测频率」增长。默认探测间隔是 `5m`，和这个规模对齐。

## 如何运行

依赖：Docker Compose **或** Go 1.22+。不需要商业托管账号。

### Docker Compose（推荐）

```bash
git clone https://github.com/bugman666/open-site-health.git
cd open-site-health
docker compose up --build -d
curl -sS http://127.0.0.1:8080/healthz
```

看到 `"status":"ok"` 即表示服务已起来。数据目录挂在 named volume `osh-data`。登记目标后，进程会按 `OSH_PROBE_INTERVAL`（默认 5 分钟）探测；也可立刻查历史（还没跑完一轮则列表为空）：

```bash
curl -sS -X POST http://127.0.0.1:8080/targets \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.org"}'
curl -sS http://127.0.0.1:8080/targets
curl -sS http://127.0.0.1:8080/probes
```

停止：

```bash
docker compose down
```

等价 Makefile 目标：`make compose-up` / `make compose-down`。

### 本地 Go

```bash
git clone https://github.com/bugman666/open-site-health.git
cd open-site-health
make test          # 单元测试
make run           # 默认监听 :8080
# 或
make smoke         # 编译后短时拉起，检查 /healthz、目标登记和一轮探测
```

常用环境变量（覆盖 `configs/config.example.json`）：

| 变量 | 含义 | 默认 |
|------|------|------|
| `OSH_CONFIG` | JSON 配置文件路径 | `configs/config.example.json` |
| `OSH_LISTEN` | 监听地址 | `:8080` |
| `OSH_DATA_DIR` | 目标列表等本地数据 | `./data` |
| `OSH_PROBE_INTERVAL` | 探测周期（Go duration） | `5m` |
| `OSH_TLS_WARN_DAYS` | 证书临期天数 | `14` |
| `OSH_WEBHOOK_URL` | Webhook（#3 才会用到） | 空 |
| `OSH_ALERT_COOLDOWN` | 告警冷却 | `1h` |

配置文件里也可以写 SMTP 字段；告警投递要等 #3，现在不会发信。探测超时固定 10 秒；HTTPS 会跳过证书校验以便单独标出临期/过期，不把坏证书当成宕机。

## 仓库结构

```
cmd/osh/            进程入口
internal/config/    配置（JSON + 环境变量）
internal/httpapi/   HTTP：/、/healthz、/targets、/probes
internal/targets/   监控目标存储（文件 JSON，后续可换 SQLite）         #1
internal/probe/     定时探测（可用性 + 证书）与近期结果               #2
internal/alert/     邮件/Webhook + 去重占位                            #3
configs/            示例配置
Dockerfile
docker-compose.yml
```

单进程、无外部数据库。目标列表落在 `data/targets.json`，近期探测记录落在 `data/probes.json`（每个目标最多保留 50 条）。

### 目标 API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/targets` | 列出全部目标（按创建时间，稳定顺序） |
| `POST` | `/targets` | 登记 URL，body：`{"url":"https://..."}` |
| `GET` | `/targets/{id}` | 读取单个目标 |
| `PUT` | `/targets/{id}` | 修改 URL |
| `DELETE` | `/targets/{id}` | 删除；之后探测循环也不会再读到它 |

URL 必须是带 `http` / `https` 的绝对地址。缺 scheme、空字符串、无法解析的值返回 `400`；规范化后与已有目标相同则返回 `409`。主机名大小写、默认端口、末尾 `/` 会先规范化再比较。

### 探测 API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/targets/{id}/probes` | 该目标的近期记录（新→旧） |
| `GET` | `/targets/{id}/status` | 最近一次探测；还没有记录则 `404` |
| `GET` | `/probes` | 全局近期记录；`?target_id=`、`?limit=` |

每条记录带 `checked_at`、`availability`（`up` / `down`）、`cert_status`（`ok` / `warn` / `expired` / `n/a`）。纯 HTTP 目标的证书字段是 `n/a`。

约定：最终状态 2xx/3xx 为 `up`；超时、连接失败、4xx、5xx 为 `down`。证书状态与连通性分开：站点可达但证书 14 天内到期是 `up` + `warn`，证书已过期是 `up` + `expired`（探测时不因证书校验失败而当成宕机）。

## 当前进度

MVP 见 [Milestone: MVP](https://github.com/bugman666/open-site-health/milestone/1)：

- [x] 仓库骨架与可复现启动（本 README + Compose）
- [x] 登记监控目标（[#1](https://github.com/bugman666/open-site-health/issues/1)）
- [x] 定时探测（可用性 + 证书）（[#2](https://github.com/bugman666/open-site-health/issues/2)）
- [ ] 告警（邮件或 Webhook + 去重）（[#3](https://github.com/bugman666/open-site-health/issues/3)）

本项目免费、可自托管，不设付费墙。

## License

[MIT](LICENSE)
