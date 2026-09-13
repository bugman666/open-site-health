# Open Site Health

开源的站点健康监控：盯住公益站点、开源文档站和志愿项目官网是否可用、证书会不会过期，异常时发告警。

适合没有专职运维、又不能让捐赠入口或报名页静默挂掉的小团队。本仓库只做自托管，核心功能不设付费墙。

## 它做什么

1. 登记要监控的 URL
2. 按周期探测可用性，并检查 HTTPS 证书过期时间
3. 异常时通过邮件或 Webhook 通知（带去重，避免同一故障刷屏）

当前可以登记监控 URL、按周期探测可用性与证书，并在宕机或证书异常时发 Webhook / 邮件（带冷却去重）（[#1](https://github.com/bugman666/open-site-health/issues/1)、[#2](https://github.com/bugman666/open-site-health/issues/2)、[#3](https://github.com/bugman666/open-site-health/issues/3)）。

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
export OSH_API_TOKEN="$(openssl rand -hex 16)"
docker compose up --build -d
curl -sS http://127.0.0.1:8080/healthz
```

Compose 把容器端口发到本机 `127.0.0.1:8080`，并且**必须**设置 `OSH_API_TOKEN`（容器内监听 `:8080`，没有 token 进程会拒绝启动）。看到 `"status":"ok"` 即表示服务已起来。数据目录挂在 named volume `osh-data`。登记目标后，进程会按 `OSH_PROBE_INTERVAL`（默认 5 分钟）探测；也可立刻查历史（还没跑完一轮则列表为空）：

```bash
curl -sS -X POST http://127.0.0.1:8080/targets \
  -H "Authorization: Bearer ${OSH_API_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.org"}'
curl -sS -H "Authorization: Bearer ${OSH_API_TOKEN}" http://127.0.0.1:8080/targets
curl -sS -H "Authorization: Bearer ${OSH_API_TOKEN}" http://127.0.0.1:8080/probes
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
make run           # 默认监听 127.0.0.1:8080
# 或
make smoke         # 编译后短时拉起，检查 /healthz、目标登记和一轮探测
```

常用环境变量（覆盖 `configs/config.example.json`）：

| 变量 | 含义 | 默认 |
|------|------|------|
| `OSH_CONFIG` | JSON 配置文件路径 | `configs/config.example.json` |
| `OSH_LISTEN` | 监听地址 | `127.0.0.1:8080` |
| `OSH_DATA_DIR` | 目标列表等本地数据 | `./data` |
| `OSH_PROBE_INTERVAL` | 探测周期（Go duration） | `5m` |
| `OSH_TLS_WARN_DAYS` | 证书临期天数 | `14` |
| `OSH_API_TOKEN` | 管理 API 共享 token（`/targets`、`/probes`） | 空（仅 loopback 监听可省略） |
| `OSH_ALLOW_PRIVATE_TARGETS` | 允许探测回环 / 私网 / 链路本地地址 | `false` |
| `OSH_WEBHOOK_URL` | 告警 Webhook（HTTP POST JSON） | 空 |
| `OSH_ALERT_COOLDOWN` | 同一目标同一故障的冷却时间 | `1h` |
| `OSH_SMTP_HOST` | SMTP 主机（可选，与 Webhook 可并存） | 空 |
| `OSH_SMTP_PORT` | SMTP 端口 | `587` |
| `OSH_SMTP_USER` | SMTP 用户 | 空 |
| `OSH_SMTP_PASSWORD` | SMTP 密码 | 空 |
| `OSH_SMTP_FROM` | 发件人 | 空 |
| `OSH_ALERT_TO` | 收件人 | 空 |

至少配置 Webhook 或 SMTP（`OSH_SMTP_HOST` + `OSH_ALERT_TO`）才会发告警。探测超时固定 10 秒；HTTPS 会跳过证书校验以便单独标出临期/过期，不把坏证书当成宕机。

## 信任模型与部署安全

本进程是**自托管运维工具**，不是多租户 SaaS。谁能调用管理 API，谁就能决定服务器去探测哪些 URL。

### 默认暴露范围

- 默认监听 `127.0.0.1:8080`，只接受本机连接。
- Compose 把主机端口绑在 `127.0.0.1:8080`，避免一 `up` 就把管理口挂到所有网卡。
- `/` 和 `/healthz` 保持匿名，方便探活，不返回目标列表或 token。

### 认证

`/targets` 与 `/probes`（含其下路径）在配置了 `OSH_API_TOKEN` / `api_token` 时必须带 token，否则 `401`：

- `Authorization: Bearer <token>`
- 或 `X-API-Key: <token>`

规则：

- 监听不是 loopback（例如 `:8080`、`0.0.0.0:8080`）时**必须**设置 token，否则进程拒绝启动。
- 只绑 `127.0.0.1` / `::1` / `localhost` 时可以不设 token，相当于「本机可信」。仍建议设一个，避免把端口转发到别处后裸奔。

这是给自托管用的**共享 token**，不是账号体系。需要 TLS 或来源限制时，把进程放在反向代理后面（Caddy / nginx），由代理做 HTTPS，再按需限制来源 IP。不要把未加密的管理 API 直接挂到公网。

### 探测目标限制

登记和实际发起请求时，默认只允许 `http` / `https`，并拒绝明显危险的目的地：

- 回环（`127.0.0.0/8`、`::1`、`localhost`）
- 私网（RFC1918、IPv6 ULA）
- 链路本地（含 `169.254.169.254` 这类云 metadata）
- CGNAT（`100.64.0.0/10`）以及常见 metadata 主机名

解析后的 IP 在拨号前再检查一遍，重定向目标也会再走同一套规则。要监控内网站点，显式打开 `OSH_ALLOW_PRIVATE_TARGETS=true`（你自己承担内网探测风险）。

### 仍须自己负责的部分

- Token 是共享密钥，泄漏即等于管理权限；不要写进公开配置仓库。
- HTTPS 探测故意跳过证书校验（`InsecureSkipVerify`），以便把证书问题标成 `cert_status` 而不是宕机。探测路径**不会**因证书不受信任而拒绝连接，仍有中间人等残余 TLS 风险；这只用于健康检查，不能当作对目标站点的安全审计。
- 告警 Webhook / SMTP 地址来自运维配置，不走上述目标限制。
- DNS 名称在登记时按字面检查，拨号时按解析结果拦截；这挡住常见 SSRF，但不是完整的应用层代理沙箱。

CI 跑 `go test` 和 `make smoke`，不跑 `docker compose`；Compose 路径需在本机自行验证。

## 仓库结构

```
cmd/osh/            进程入口
internal/config/    配置（JSON + 环境变量）
internal/httpapi/   HTTP：/、/healthz、/targets、/probes
internal/safeurl/   探测目的地限制（私网 / metadata）
internal/targets/   监控目标存储（文件 JSON，后续可换 SQLite）         #1
internal/probe/     定时探测（可用性 + 证书）与近期结果               #2
internal/alert/     邮件/Webhook + 冷却去重                             #3
configs/            示例配置
Dockerfile
docker-compose.yml
```

单进程、无外部数据库。目标列表落在 `data/targets.json`，近期探测记录落在 `data/probes.json`（每个目标最多保留 50 条），最近一次告警落在 `data/alerts.json` 以便冷却跨重启仍生效。

### 目标 API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/targets` | 列出全部目标（按创建时间，稳定顺序） |
| `POST` | `/targets` | 登记 URL，body：`{"url":"https://..."}` |
| `GET` | `/targets/{id}` | 读取单个目标 |
| `PUT` | `/targets/{id}` | 修改 URL |
| `DELETE` | `/targets/{id}` | 删除；之后探测循环也不会再读到它 |

配置了 `OSH_API_TOKEN` 时，上表路径都要带 Bearer 或 `X-API-Key`。URL 必须是带 `http` / `https` 的绝对地址。缺 scheme、空字符串、无法解析、或默认策略下的私网 / metadata 地址返回 `400`；规范化后与已有目标相同则返回 `409`。主机名大小写、默认端口、末尾 `/` 会先规范化再比较。

### 探测 API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/targets/{id}/probes` | 该目标的近期记录（新→旧） |
| `GET` | `/targets/{id}/status` | 最近一次探测；还没有记录则 `404` |
| `GET` | `/probes` | 全局近期记录；`?target_id=`、`?limit=` |

这些路径同样受 API token 保护（若已配置）。

每条记录带 `checked_at`、`availability`（`up` / `down`）、`cert_status`（`ok` / `warn` / `expired` / `n/a`）。纯 HTTP 目标的证书字段是 `n/a`。

约定：最终状态 2xx/3xx 为 `up`；超时、连接失败、4xx、5xx 为 `down`。证书状态与连通性分开：站点可达但证书 14 天内到期是 `up` + `warn`，证书已过期是 `up` + `expired`（探测时不因证书校验失败而当成宕机）。

### 告警

探测结果为 `down`、`cert_warn` 或 `cert_expired` 时投递一条通知；同一目标同一 `kind` 在冷却窗口内不再发。故障类型变化会立刻再发。曾告警的目标恢复为 `up`（证书 `ok` 或 `n/a`）时发一条 `recovered`，之后保持安静，直到再次故障。

Webhook 请求是 JSON POST，字段包括 `target_id`、`url`、`kind`（`down` / `cert_warn` / `cert_expired` / `recovered`）、`message`。SMTP 主题和正文同样带目标与故障类型。

## 当前进度

MVP 见 [Milestone: MVP](https://github.com/bugman666/open-site-health/milestone/1)：

- [x] 仓库骨架与可复现启动（本 README + Compose）
- [x] 登记监控目标（[#1](https://github.com/bugman666/open-site-health/issues/1)）
- [x] 定时探测（可用性 + 证书）（[#2](https://github.com/bugman666/open-site-health/issues/2)）
- [x] 告警（邮件或 Webhook + 去重）（[#3](https://github.com/bugman666/open-site-health/issues/3)）
- [x] 公开前加固：管理 API token + 探测目的地限制（[#9](https://github.com/bugman666/open-site-health/issues/9)）

本项目免费、可自托管，不设付费墙。

## License

[MIT](LICENSE)
