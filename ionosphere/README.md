# 电离层 Chapman 层 TEC 计算服务

常驻 HTTP 后端：用 **Chapman 层模型**计算电子密度剖面，并沿几何高度数值积分出
**垂直总电子含量（VTEC，单位 TECU）**。仅提供算力，不含看板、订阅、工单与账户体系。

- 语言：Go 1.22（标准库 `net/http`，驱动为纯 Go 的 `pgx/v5`，CGO 关闭）
- 持久化：PostgreSQL 16
- 启动：`docker compose up --build`（一条命令构建并拉起服务与数据库）

## 物理模型

无因次高度

```
z = (h - hm) / H
```

其中 `h` 为几何海拔（米），`hm` 为峰值高度（米），`H` 为标高（米）。
天顶角 `χ`（度）以正割进入产生率，电子密度为

```
N(h) = Nm · exp(½(1 − sec χ)) · exp(½(1 − z − e^(−z)))
```

- `χ = 0` 时峰点密度恰好等于输入峰值密度 `Nm`；
- 形状因子 `exp(½(1 − z − e^(−z)))` 在 `z = 0` 处取唯一全局最大值 1，
  因此**极大值永远钉在 `hm`，峰位不随天顶角漂移**；`hm` 上下两侧密度都不超过峰点值；
- 天顶角增大时峰点密度 `Nm·exp(½(1−sec χ))` 单调下降（`χ = 60°` 时为 `Nm·e^(−1/2)`）。

数值积分采用复合辛普森法则，从地面（海拔 0 m）积到服务固定顶高（默认 2000 km），
不断加密网格，直到相邻两次 TEC 的相对变化小于容差（默认 `1e-6`）。

```
VTEC = ∫₀^top N(h) dh / 1e16        # TECU，1 TECU = 10^16 m^-2
```

**没有**「峰值密度 × 标高 × 固定系数」之类的捷径——改标高导致的剖面形状变化
真正参与积分。积分结果必须为正；天顶角越界或夜间层不会静默返回白天剖面。

> 单位约定：高度一律为**米**（海拔，地面基准 0 m），电子密度为 **m⁻³**，
> 天顶角为**度**，TEC 为 **TECU**。

## 接口

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| POST | `/api/v1/profile` | 返回完整电子密度剖面 + TEC |
| POST | `/api/v1/tec` | 轻量接口：只积分、只返回 TEC |
| POST | `/api/v1/batch` | 一次提交多组层参数，逐组核算 |
| GET  | `/api/v1/demo` | 内置正午 F2 层示范算例 |
| GET  | `/api/v1/history` | 历史检索（见下） |
| GET  | `/api/v1/config` | 配置回显：地面基准 / 顶高 / TECU / 容差 |
| GET  | `/livez` | 存活探针 |
| GET  | `/readyz` | 就绪探针（含数据库检查） |

请求体（单层）：

```json
{
  "peak_density": 1.2e12,
  "peak_height": 300000,
  "scale_height": 60000,
  "zenith_angle": 0.0
}
```

批量请求：`{"layers": [ {...}, {...} ], "include_profile": false}`。
**某组非法时响应仍为 200**，错误项带 `index`（第几组，从 0 起）与 `error.field`
（哪个参数），其余组正常返回；合法/非法全部持久化。非法情形包括：缺字段、
非数值、非有限值、峰值密度或标高非正、峰值高度不高于地面、天顶角不在 `[0,90)`、
积分顶高不足以覆盖峰值高度。

历史检索支持查询参数：`status`、`mode`、`batch_id`、`min_tec_u`、`max_tec_u`、
`since`、`until`（RFC3339）、`limit`、`offset`。

## 配置（环境变量）

| 变量 | 默认值 | 含义 |
| ---- | ---- | ---- |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `DATABASE_URL` | 指向 compose 中的 `db` | PostgreSQL 连接串 |
| `GROUND_ALTITUDE` | `0` | 地面基准（米） |
| `TOP_ALTITUDE` | `2000000` | 积分顶高（米） |
| `PROFILE_STEP` | `2000` | 剖面输出步长（米） |
| `INTEGRATION_TOLERANCE` | `1e-6` | TEC 积分相对容差 |

所有数值均可在 `GET /api/v1/config` 回显。

## 本地运行

```bash
docker compose up --build
curl -s localhost:8080/api/v1/demo | jq .
```

直接运行（需本机 Go 1.22）：

```bash
go run ./cmd/server
go test ./...
# 针对 compose 数据库跑 PostgreSQL 集成测试：
CHAPMAN_TEST_DATABASE_URL='postgres://ionosphere:ionosphere@localhost:5432/ionosphere?sslmode=disable' \
  go test ./internal/persistence/
```

## 代码结构（按职责分文件）

```
cmd/server/main.go                  常驻服务启动、优雅退出
internal/model/chapman.go           Chapman 剖面（无因次高度、天顶角因子、峰钉在 hm）
internal/tec/integrate.go           沿高度数值积分与 TECU 换算
internal/validate/validate.go       输入校验（逐字段、逐组错误定位）
internal/persistence/store.go       记录类型、存储接口、检索过滤器
internal/persistence/memory.go      并发安全的内存存储（测试用）
internal/persistence/postgres.go    PostgreSQL 16 持久化 + 迁移
internal/persistence/migrations/    建表 SQL（随二进制嵌入）
internal/config/config.go           配置与环境变量
internal/api/                       HTTP 路由、编排、请求解析
```
