# ionosphere — Chapman 层电子密度与垂直 TEC 服务

电离层研究后端：给定 Chapman 层参数，返回电子密度剖面，并沿高度数值积分
得到垂直总电子含量（TEC，单位 TECU）。常驻 HTTP 服务，Go 1.22，
持久化使用 PostgreSQL 16，Docker Compose 一条命令拉起。

## 物理模型

无因次高度 `z = (h − hmF2) / H`（几何高度减峰值高度，除以标高）。

零天顶角标准 Chapman 层：

```
N(h) = Nmax · exp( ½ · (1 − z − e^(−z)) )
```

太阳天顶角 χ 以正割进入产生率，峰点密度：

```
Nmax(χ) = NmF2 · exp( ½ · (1 − sec χ) )
```

因此极大值**始终钉在 hmF2**，不随天顶角漂移；χ = 0 时峰点密度恰为
输入的 NmF2；χ 增大时峰点密度单调下降；χ ≥ 90° 直接拒绝（非白昼层）。

TEC 为密度沿高度的**真实数值积分**（自适应梯形加密，直到两次加密估值
之差小于配置容差），从地面（海拔 0）积到配置顶高，单位换算
`1 TECU = 10^16 m⁻²`。绝非「NmF2 × H × 固定系数」。

## 快速开始

```bash
docker compose up --build
```

服务监听 `localhost:8080`，PostgreSQL 16 由 compose 一并拉起，建表自动完成。

本地开发（无数据库时退化为内存存储并打印警告）：

```bash
go build -o ionod . && ./ionod
```

## 接口

所有计算接口共用同一组输入参数（JSON）：

| 字段 | 含义 | 单位 | 约束 |
|---|---|---|---|
| `peakDensity` | 峰值电子密度 NmF2 | m⁻³ | > 0 |
| `peakHeight` | 峰值高度 hmF2 | m | 高于地面、低于积分顶高 |
| `scaleHeight` | 标高 H | m | > 0 |
| `solarZenithAngle` | 太阳天顶角 χ | 度 | [0, 90) |

### `POST /v1/profile` — 完整剖面 + TEC

可选额外字段 `points`（剖面采样点数，2–10000，默认 400）。

```bash
curl -s -X POST localhost:8080/v1/profile -d '{
  "peakDensity": 1.2e12, "peakHeight": 300000,
  "scaleHeight": 60000, "solarZenithAngle": 0
}'
```

```json
{
  "id": 1,
  "input": {"peakDensity": 1.2e12, "peakHeight": 300000, "scaleHeight": 60000, "solarZenithAngle": 0},
  "peak": {"height": 300000, "density": 1.2e12},
  "tec": 2.9686e17,
  "tecu": 29.686,
  "integration": {"from": 0, "to": 1000000, "intervals": 8192, "converged": true},
  "profile": [{"height": 0, "density": 1.4e-19}, ...]
}
```

### `POST /v1/tec` — 轻量接口，只积分、只返回 TEC

请求体相同，响应不含 `profile`。

### `POST /v1/batch` — 批量核算

```json
{
  "includeProfile": false,
  "requests": [ {"peakDensity": ..., ...}, {"peakDensity": ..., ...} ]
}
```

每组独立校验、独立计算。非法组在对应位置给出 `index` 与出错
`parameter`，其余组照常返回；合法结果持久化并共享同一个 `batchId`。
响应恒为 200（除非整体请求体非法，如空 `requests` 或超过批量上限）。

### `GET /v1/demo` — 内置正午 F2 层示范算例

NmF2 = 1.2×10¹² m⁻³，hmF2 = 300 km，H = 60 km，χ = 0。峰落在给定峰值
高度，χ = 0 时峰点密度等于输入峰值密度，TEC ≈ 29.7 TECU。

### `GET /v1/config` — 配置回显

```json
{
  "groundAltitude": 0, "topHeight": 1000000,
  "tecu": 1e16, "tecTolerance": 1e-9,
  "profilePoints": 400, "maxBatchSize": 500, "version": "1.0.0"
}
```

### `GET /v1/status` — 运行状态（监控采集）

`status`、`db`（up/down）、运行时长、请求与核算计数。数据库不可达时
返回 503 + `"status": "degraded"`。

### `GET /v1/history` — 历史检索

每次核算的输入与结果都持久化到 PostgreSQL。查询条件（可组合）：

- `kind`：`profile` / `tec` / `batch` / `demo`
- `since` / `until`：RFC3339 时间窗
- `minTecu` / `maxTecu`：TECU 区间
- `limit`（1–1000，默认 50）/ `offset`

列表结果中剖面被裁剪以保持紧凑；`GET /v1/history/{id}` 返回含完整剖面
在内的整条记录。

### 错误格式

```json
{"error": {"parameter": "solarZenithAngle", "message": "must be < 90 degrees; ..."}}
```

缺字段、非数值、非有限值、峰值密度或标高非正、峰值高度不高于地面、
天顶角越界、积分顶高不覆盖峰值高度，均以 400 指明参数拒绝，绝不静默
返回一套白天剖面。

## 配置（环境变量）

| 变量 | 默认 | 含义 |
|---|---|---|
| `PORT` | `8080` | HTTP 端口 |
| `DATABASE_URL` | 空 | PostgreSQL DSN；为空时用内存存储（不持久化） |
| `GROUND_ALTITUDE_M` | `0` | 地面基准（海拔，m） |
| `TOP_HEIGHT_M` | `1000000` | 积分顶高（m） |
| `TEC_TOLERANCE` | `1e-9` | 积分加密收敛容差（相对） |
| `PROFILE_POINTS` | `400` | 剖面默认采样点数 |
| `MAX_BATCH_SIZE` | `500` | 批量上限 |

## 代码结构（按职责分文件）

| 文件 | 职责 |
|---|---|
| `chapman.go` | Chapman 剖面：无因次高度、形状函数、天顶角因子 |
| `tec.go` | 按高度自适应数值积分、TECU 换算 |
| `compute.go` | 剖面采样与单次核算装配 |
| `validate.go` | 输入解析与物理约束校验（指明出错参数） |
| `store.go` | 持久化抽象与内存实现 |
| `pgstore.go` | PostgreSQL 实现与建表迁移 |
| `handlers.go` | HTTP 路由与处理器 |
| `config.go` / `main.go` | 配置加载、服务装配、优雅停机 |

## 测试

```bash
go test ./...          # 单元 + HTTP 端到端（内存存储），无需数据库
go test -race ./...    # 并发竞态
# PostgreSQL 集成测试（需要可连的数据库）：
IONO_TEST_DATABASE_URL="postgres://iono:iono@localhost:5432/ionosphere?sslmode=disable" \
  go test -run Integration ./...
```

覆盖：χ=0 峰点密度等于输入峰值密度、极大值高度等于峰值高度、峰位不随
天顶角漂移、峰值密度翻倍则剖面与 TEC 翻倍、标高翻倍则峰位不变且 TEC
近似翻倍、抬高峰值高度则峰位跟随、χ 增至 60° 峰点密度下降、χ=90° 被
拒、各类非法参数被拒、批量部分失败其余成功、TEC 对积分上下界敏感
（反「固定系数」判据）、与 Chapman 全柱解析值 √(2πe)·NmF2·H 对拍、
历史持久化与条件检索、并发多请求互不串扰。
