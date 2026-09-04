# wow-dashboard-api

[English](README.md) | 简体中文

面向 Minimal Starter 仪表盘项目的 Go API 服务，可供 Next.js 和 Vite 前端接入。项目提供类型化 HTTP 接口、PostgreSQL 持久化，以及随代码提交的 OpenAPI 契约，前端无需自行猜测请求和响应结构。

**技术栈：** Go 1.27.1 · Chi · Huma v2 · PostgreSQL · pgx · sqlc · goose · River

## 功能概览

- 身份认证：注册、登录、HttpOnly access/refresh cookie、刷新令牌轮换、密码重置和邮箱验证。
- 账户安全：TOTP MFA、恢复码、活跃会话管理、近期安全活动，以及密码变更、MFA 变更和新设备登录的邮件/站内通知。
- 权限控制：数据库驱动的角色、代码定义的权限目录、管理员用户模拟。
- 业务模块：用户、角色、支持 owner/editor/viewer 权限的项目、通知和审计事件。
- 后台任务：基于 PostgreSQL 的 River worker，处理邮件投递和定期数据清理。
- 运维能力：结构化日志、就绪探测、Prometheus 指标、OpenTelemetry 追踪，以及以非 root 用户运行的多架构容器镜像。

本仓库仅包含后端，不包含仪表盘 UI。PostgreSQL 为必需依赖；Redis 和本地可观测性组件为可选依赖。

## 快速开始

### 环境要求

- Go 1.27.1 或 1.27 系列中的后续版本；项目固定版本见 [go.mod](go.mod)。
- Docker 与正在运行的 Docker daemon、Docker Compose v2，用于本地 PostgreSQL、Mailpit 和集成测试。
- Air，用于 API 热重载。
- Bun，或 Node.js 与 npm，用于 TypeScript 契约生成检查。Newman 冒烟测试使用 `npx`。

以下命令均在仓库根目录执行。

### 1. 准备环境

如果尚无本地配置，复制示例文件，然后检查其中的配置值：

```sh
test -f .env || cp .env.example .env
go mod download
go install github.com/air-verse/air@latest
```

确保 Air 位于 `PATH` 中。[`.env.example`](.env.example) 仅用于本地开发，不要在生产环境复用其中的凭据和密钥。

### 2. 初始化数据库并启动 API

```sh
make local-setup
docker compose up -d mailpit
make dev
```

`make local-setup` 会启动并等待 PostgreSQL 就绪，执行 goose 和 River 迁移，然后创建演示管理员。默认本地数据库地址与 `compose.yaml` 一致；只有明确使用其他开发数据库时才覆盖 `LOCAL_DATABASE_URL`。

Air 会加载 `.env` 和 `.env.local`。API 默认监听 [http://localhost:7272](http://localhost:7272)：

| 地址 | 用途 |
| --- | --- |
| [`/docs`](http://localhost:7272/docs) | 交互式 API 文档 |
| [`/openapi`](http://localhost:7272/openapi) | 运行时 OpenAPI JSON |
| [`/healthz`](http://localhost:7272/healthz) | 存活探测 |
| [`/readyz`](http://localhost:7272/readyz) | 就绪探测，包含 PostgreSQL 检查 |
| [Mailpit](http://localhost:8025) | 查看本地捕获的邮件 |

演示管理员：`demo@wow-dashboard.test`，密码：`@Password`。仅用于可丢弃的本地数据。再次执行 seed 会更新该账户，包括密码和管理员角色。

### 3. 启动邮件和后台 worker

另开一个终端，显式传入本地配置：

```sh
DATABASE_URL='postgres://wow_dashboard:wow_dashboard@localhost:5432/wow_dashboard_api?sslmode=disable' \
EMAIL_SMTP_HOST=localhost \
EMAIL_SMTP_PORT=1025 \
EMAIL_SMTP_TLS=none \
make worker
```

与 Air 不同，`make worker` 和直接执行的 `go run` 不会自动加载 `.env`。请传入与 API 一致的相关环境变量，尤其是自定义数据库或生产配置。

API 只负责将邮件任务加入队列，实际投递由 worker 完成。不运行 worker，邮箱验证、密码重置和安全提醒邮件就会留在队列中。worker 也负责清理过期令牌和超过保留期限的审计事件。

### 4. 验证服务

```sh
make smoke-auth
# Optional: Postman/Newman checks against the running API
make postman-test
```

需要独立的黑盒验证时，可运行 `make smoke-local`：它会准备本地数据库、启动自己的 API 进程、执行 Newman，最后停止该 API 进程，但保留 PostgreSQL 运行。详见 [postman/README.md](postman/README.md)。

**破坏性命令：** `make local-reset` 会删除本地 PostgreSQL 数据卷及其中的数据，再重新初始化数据库。不要对需要保留的数据执行此命令。

## 前端接入

以仓库内的 [OpenAPI JSON](openapi/openapi.json) 和 [TypeScript 类型](openapi/typescript/schema.ts) 为接口契约。Next.js starter 可配置：

```dotenv
NEXT_PUBLIC_SERVER_URL=http://localhost:7272
```

Vite 前端应使用其自身约定的 API 基础地址环境变量。

### 基于 cookie 的认证

成功建立会话时，接口返回 `{ "user": ... }`，并设置两个 HttpOnly cookie。**JSON 中不返回 access token，也不应将令牌存入浏览器 localStorage。**

```ts
const response = await fetch(baseURL + '/api/auth/me', {
  credentials: 'include',
});
// Axios: axios.create({ baseURL, withCredentials: true })
```

- 登录返回 `mfaRequired: true` 时，应携带 credentials 向 `POST /api/auth/mfa/verify` 提交验证码或恢复码；验证成功后才算完成登录。
- 会话过期返回 `401` 时，携带 credentials 调用 `POST /api/auth/refresh`，成功后仅重试原请求一次；刷新失败则返回登录页。
- 退出登录会清除两个 cookie；刷新操作会轮换 refresh token。
- JWT 默认 15 分钟过期。access cookie 的 MaxAge 则跟随更长的刷新会话有效期，便于前端在 JWT 过期后尝试刷新。仅存在 cookie 不代表认证有效。
- 非浏览器客户端也可使用已签发的 access token，通过 `Authorization: Bearer <token>` 认证；显式请求头优先于 access cookie。

在 `CORS_ALLOWED_ORIGINS` 中配置准确的前端 Origin。使用 cookie 的状态变更请求会经过 CSRF 检查：不同 Origin，**包括同站点的其他子域名**，都必须匹配 Origin 白名单。没有浏览器 Fetch Metadata 的 cookie 客户端应发送允许的 `Origin`。

真正的跨站 cookie 需要 `SameSite=None` 和 `Secure=true`，且仍受浏览器第三方 cookie 策略限制。仅仅调用其他子域名上的 API，不需要共享 cookie 域；只有明确要将 access cookie 共享给可信主机时，才设置 `ACCESS_TOKEN_COOKIE_DOMAIN`。

更多客户端配置见 [前端接入指南](docs/frontend-integration.md)。

## API 索引

当前契约包含 35 个路径、45 个操作。请求字段、状态码、验证规则和错误结构以 [openapi/openapi.json](openapi/openapi.json) 为准，前端不应手写重复 DTO。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/auth/sign-up` | 注册，返回 201 并建立会话。 |
| `POST` | `/api/auth/sign-in` | 登录，可能需要继续完成 MFA。 |
| `POST` | `/api/auth/refresh` | 轮换 refresh token 并设置新的会话 cookie。 |
| `POST` | `/api/auth/sign-out` | 撤销当前 refresh token 并清除两个 cookie。 |
| `POST` | `/api/auth/sign-out-others` | 保留当前会话，撤销其他会话。 |
| `GET` | `/api/auth/me` | 读取个人资料、角色、权限及 MFA/邮箱验证状态。 |
| `PATCH` | `/api/auth/me` | 更新自己的个人资料。 |
| `POST` | `/api/auth/change-password` | 修改密码并撤销刷新会话。 |
| `POST` | `/api/auth/forgot-password` | 申请密码重置邮件，不通过响应暴露账户是否存在。 |
| `POST` | `/api/auth/reset-password` | 使用一次性令牌重置密码。 |
| `POST` | `/api/auth/verify-email` | 使用一次性令牌验证邮箱。 |
| `POST` | `/api/auth/resend-verification` | 重新发送验证邮件。 |
| `POST` | `/api/auth/mfa/setup` | 开始绑定 TOTP。 |
| `POST` | `/api/auth/mfa/confirm` | 启用 MFA，恢复码仅展示一次。 |
| `POST` | `/api/auth/mfa/verify` | 完成需要 MFA 的登录。 |
| `DELETE` | `/api/auth/mfa` | 完成必要验证后关闭 MFA。 |
| `GET` | `/api/auth/sessions` | 列出自己的活跃会话和设备信息。 |
| `DELETE` | `/api/auth/sessions/{id}` | 撤销自己的一个会话族。 |
| `GET` | `/api/auth/security-activity` | 读取自己的近期安全事件。 |
| `POST` | `/api/auth/impersonate/{targetUserId}` | 管理员开始模拟指定用户。 |
| `POST` | `/api/auth/impersonate/stop` | 返回管理员会话。 |
| `GET` | `/api/users` | 分页、筛选用户。 |
| `GET` | `/api/users/{id}` | 读取用户详情。 |
| `PATCH` | `/api/users/{id}` | 修改状态或替换角色集合。 |
| `GET` | `/api/roles` | 列出角色。 |
| `POST` | `/api/roles` | 创建自定义角色。 |
| `GET` | `/api/roles/{id}` | 读取角色详情。 |
| `PATCH` | `/api/roles/{id}` | 更新自定义角色。 |
| `DELETE` | `/api/roles/{id}` | 删除未分配给用户的自定义角色。 |
| `GET` | `/api/permissions` | 列出可分配权限。 |
| `GET` | `/api/projects` | 列出自己拥有及他人共享的项目。 |
| `POST` | `/api/projects` | 创建项目。 |
| `GET` | `/api/projects/{id}` | 读取有权访问的项目。 |
| `PATCH` | `/api/projects/{id}` | 所有者或 editor 更新项目。 |
| `DELETE` | `/api/projects/{id}` | 所有者归档项目，不物理删除。 |
| `GET` | `/api/projects/{id}/members` | 列出项目成员。 |
| `POST` | `/api/projects/{id}/members` | 所有者通过邮箱为已注册用户授予访问权。 |
| `PATCH` | `/api/projects/{id}/members/{userId}` | 所有者修改成员角色。 |
| `DELETE` | `/api/projects/{id}/members/{userId}` | 所有者移除成员。 |
| `GET` | `/api/system-events` | 读取系统审计日志。 |
| `GET` | `/api/notifications` | 读取自己的通知及未读数量。 |
| `POST` | `/api/notifications/{id}/read` | 将自己的通知标记为已读。 |
| `POST` | `/api/notifications/read-all` | 将自己的全部通知标记为已读。 |
| `GET` | `/healthz` | 存活探测，不依赖外部服务。 |
| `GET` | `/readyz` | 通过 PostgreSQL ping 判断就绪状态，失败返回 503。 |

### 权限与资源行为

| 权限 | 能力 |
| --- | --- |
| `users:read` | 查看用户列表与详情 |
| `users:manage` | 修改用户状态和角色 |
| `roles:read` | 查看角色和权限目录 |
| `roles:manage` | 创建、修改和删除自定义角色 |
| `system_events:read` | 查看系统级审计事件 |
| `projects:create` | 创建项目 |

内置 `admin` 角色拥有保留权限 `*`；新注册用户获得 `user` 角色及 `projects:create` 权限。系统角色不能通过 API 修改。自定义角色存储于数据库，有效权限为所有角色权限的并集，并在请求认证时从数据库读取。

- 项目访问权限独立于全局 RBAC：所有者和成员可读，所有者/editor 可修改，只有所有者能归档或管理成员。SQL 查询约束资源访问范围。
- 项目名去除两端空白后，在同一所有者下大小写敏感且唯一。归档项目仍占用名称；重复归档请求可成功。
- 用户管理操作不能修改当前管理员自己的账户。仍分配给用户的自定义角色不能删除。
- 用户/项目列表使用 `page`、`pageSize` 和 `search`，默认每页 20 条、最大 100 条。审计、安全活动和通知列表使用 `limit`、`nextCursor` 游标分页；筛选条件以各接口为准。
- 通知和近期安全活动仅限当前用户。管理员模拟状态下，账户安全操作还有额外限制。
- 邮箱验证状态会记录并返回给前端，但目前不作为登录前置条件。

### 错误与审计事件

API 错误使用统一结构：

```json
{
  "code": "not_found",
  "message": "The requested resource was not found.",
  "request_id": "request-id"
}
```

验证错误可额外包含 `details`，其中每项包含 `field` 和 `message`。空列表返回 `[]`，而不是 `null`。未预期错误向客户端返回安全消息，原始原因仅在服务端记录。

审计生产者使用稳定事件类型和安全元数据写入 `system_events`。审计写入采用尽力而为策略，不改变原业务操作结果。事件分类及元数据规则见 [审计策略](docs/audit-policy.md)。

## 配置

运行时设置由 [internal/config/config.go](internal/config/config.go) 解析和验证。下表是**环境变量未设置时的程序默认值**，不是 `.env.example` 中的全部示例值；该示例明确设置了本地数据库地址和 Mailpit 传输。名称带 SECONDS 的参数均以秒为单位。

| 环境变量 | 程序默认值 | 用途 |
| --- | --- | --- |
| `APP_NAME` | `wow-dashboard-api` | 日志和邮件通知使用的应用名称。 |
| `PORT` | `7272` | HTTP 监听端口。 |
| `ENV` | `development` | development、staging 或 production。 |
| `APP_BASE_URL` | `http://localhost:3000` | 密码重置和邮箱验证链接指向的前端地址。 |
| `LOG_FORMAT` | `（自动）` | 非生产环境为 text，生产环境为 json。 |
| `LOG_LEVEL` | `info` | debug、info、warn 或 error。 |
| `READ_TIMEOUT_SECONDS` | `15` | HTTP 读取超时。 |
| `WRITE_TIMEOUT_SECONDS` | `15` | HTTP 写入超时。 |
| `IDLE_TIMEOUT_SECONDS` | `60` | HTTP 空闲连接超时。 |
| `HTTP_SHUTDOWN_TIMEOUT_SECONDS` | `10` | HTTP 优雅关闭的最长等待时间。 |
| `REQUEST_BODY_MAX_BYTES` | `1048576` | 传输层请求体上限；0 仅关闭该外层限制，不关闭 Huma 的接口级限制。 |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000,http://localhost:5173,http://localhost:8082,http://localhost:8083,http://localhost:8084,http://localhost:8085` | 逗号分隔的前端 Origin；生产环境要求精确的 HTTPS Origin。 |
| `DATABASE_URL` | `（空）` | API/worker 启动必填。本地 Compose 连接串写在 .env.example 中，不是程序默认值。 |
| `DB_MAX_CONNS` | `10` | 连接池最大连接数。 |
| `DB_MIN_CONNS` | `1` | 连接池最小连接数，范围为 0 到 DB_MAX_CONNS。 |
| `DB_MAX_CONN_LIFETIME_SECONDS` | `1800` | 单个连接最大生命周期。 |
| `DB_MAX_CONN_IDLE_TIME_SECONDS` | `300` | 连接最大空闲时间。 |
| `DB_HEALTH_TIMEOUT_SECONDS` | `3` | 数据库就绪探测超时。 |
| `DB_STATEMENT_TIMEOUT_SECONDS` | `30` | PostgreSQL 单条 SQL 超时。 |
| `DB_HEALTH_CHECK_PERIOD_SECONDS` | `30` | 连接池健康检查间隔。 |
| `AUTH_RATE_LIMIT_ENABLED` | `true` | 启用敏感认证接口的共享限流预算。 |
| `AUTH_RATE_LIMIT_REQUESTS` | `10` | 每个 IP 在一个窗口内允许的请求数。 |
| `AUTH_RATE_LIMIT_WINDOW_SECONDS` | `60` | 认证限流窗口。 |
| `AUTH_RATE_LIMIT_BURST` | `5` | 内存限流器允许的瞬时突发请求数。 |
| `AUTH_MAX_FAILED_LOGIN_ATTEMPTS` | `10` | 触发账户锁定的连续登录失败次数。 |
| `AUTH_ACCOUNT_LOCKOUT_SECONDS` | `900` | 账户临时锁定时长。 |
| `REDIS_URL` | `（空）` | 可选的 Redis 共享认证限流；留空时使用进程内存。 |
| `JWT_ACCESS_SECRET` | `dev-only-change-me-min-32-characters` | HS256 签名密钥，至少 32 个字符；生产环境必须替换。 |
| `MFA_ENCRYPTION_KEY` | `dev-only-change-me-mfa-encryption-key-32+` | TOTP 加密密钥，至少 32 个字符；生产环境必须使用与 JWT 不同的非占位密钥。 |
| `JWT_ISSUER` | `wow-dashboard-api` | JWT 签发者。 |
| `JWT_AUDIENCE` | `wow-dashboard` | JWT 受众。 |
| `JWT_ACCESS_TOKEN_TTL_SECONDS` | `900` | JWT 有效期；生产环境允许 60–3600 秒。 |
| `REFRESH_TOKEN_TTL_SECONDS` | `7776000` | 90 天刷新会话有效期，轮换后续期；也用于 access cookie 的 MaxAge。 |
| `REFRESH_TOKEN_COOKIE_NAME` | `wow_dashboard_refresh_token` | HttpOnly refresh cookie，Path=/api/auth。 |
| `REFRESH_TOKEN_COOKIE_SECURE` | `false` | 生产环境未设置时默认为 true，显式 false 会被拒绝。 |
| `REFRESH_TOKEN_COOKIE_SAMESITE` | `lax` | lax、strict 或 none；none 要求 Secure。 |
| `ACCESS_TOKEN_COOKIE_NAME` | `wow_dashboard_access_token` | HttpOnly access cookie，Path=/；名称不得与 refresh cookie 相同。 |
| `ACCESS_TOKEN_COOKIE_SECURE` | `false` | 生产环境未设置时默认为 true，显式 false 会被拒绝。 |
| `ACCESS_TOKEN_COOKIE_SAMESITE` | `lax` | lax、strict 或 none；none 要求 Secure。 |
| `ACCESS_TOKEN_COOKIE_DOMAIN` | `（空）` | 默认仅当前主机；仅在明确需要共享 cookie 时配置父域名。 |
| `EMAIL_SMTP_HOST` | `（空）` | 留空使用开发用 LogSender；生产环境必须指定 SMTP 主机。 |
| `EMAIL_SMTP_PORT` | `0` | 0 根据 TLS 模式选择端口：none=25、starttls=587、tls=465。 |
| `EMAIL_SMTP_USERNAME` | `（空）` | SMTP 认证用户名。 |
| `EMAIL_SMTP_PASSWORD` | `（空）` | SMTP 认证密码。 |
| `EMAIL_SMTP_TLS` | `starttls` | none、starttls 或 tls；本地 Mailpit 使用 none。 |
| `EMAIL_FROM_ADDRESS` | `noreply@wow-dashboard.test` | 发件人地址；生产环境使用已验证的发件人。 |
| `EMAIL_FROM_NAME` | `WOW Dashboard` | 发件人显示名称。 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `（空）` | OTLP/HTTP Collector 地址；留空不导出追踪。 |
| `METRICS_ADDR` | `（空）` | 可选独立指标监听地址；未设置时仅非生产环境在 API 端口提供 /metrics。 |
| `ENABLE_DOCS` | `true` | 交互文档 /docs；生产环境未设置时默认为 false；/openapi 仍可访问。 |
| `SYSTEM_EVENTS_RETENTION_DAYS` | `90` | worker 定期清理任务使用的审计记录保留天数。 |

仅当 `HTTP_SHUTDOWN_TIMEOUT_SECONDS` 未设置时，才接受旧别名 `SHUTDOWN_TIMEOUT_SECONDS`。

### 生产环境检查清单

- 设置 `ENV=production`、真实的 `DATABASE_URL`、有效 HTTPS `APP_BASE_URL`、自有且精确匹配的 HTTPS CORS Origin，以及 SMTP 主机和发件人。
- 提供高熵 `JWT_ACCESS_SECRET` 和不同的 `MFA_ENCRYPTION_KEY`；开发占位值会被拒绝。若轮换 MFA 密钥而不迁移已有密文，已绑定用户需要重新绑定。
- 启用 Secure cookie。不要直接复用 `.env.example`：它显式将两个 Secure 标志设为 false，并启用了文档。
- 服务会拒绝无效超时、连接池参数、cookie 名称及不支持的配置。默认不信任 `X-Forwarded-For` 或 `X-Real-IP`。
- 认证使用 Argon2id 密码哈希、HS256 JWT 验证、IP 限流和账户临时锁定。Redis 可选：启动时连接失败会回退到进程内存限流，运行时 Redis 错误会放行请求。公网或多实例部署应在可信入口另设限流。
- 不要公开 `/metrics`，生产环境通过内部 `METRICS_ADDR` 独立监听。`/docs` 默认关闭，但 `/openapi` 仍然提供。
- 同时运行 API 和 worker。开发用 `LogSender` 会记录可能含一次性链接的邮件正文；生产环境应使用真实 SMTP，并保护邮件和队列数据。

运维流程见 [运维指南](docs/operations.md) 和 [部署手册](docs/deployment.md)。

## 目录结构

```text
cmd/
  api/                  HTTP 服务入口
  worker/               River 后台任务进程
  openapi/              离线 OpenAPI 生成器
  seed/                 演示管理员初始化
  smoke-auth/           运行中服务的认证冒烟检查
  river-migrate/        River 数据库迁移
  queue-ping/           队列冒烟测试生产者
  healthcheck/          容器存活探测
internal/
  app/                  依赖装配与进程生命周期
  config/               类型化环境配置
  http/                 Huma handler、中间件、错误与分页
  auth/                 密码、令牌、认证与 RBAC
  <resource>/           资源领域类型与业务服务
  store/sql/            手写 SQL
  store/query/          sqlc 生成的 Go 代码
  store/<resource>repo/ 仓储适配器与事务边界
  jobs/                 邮件与数据保留任务
  securityalerts/       邮件和站内安全提醒
  observability/        追踪与数据库/队列指标
migrations/             goose 应用数据库迁移
openapi/                随代码提交的 JSON 和 TypeScript 契约
observability/          本地 Prometheus、Grafana 和 Jaeger 配置
postman/                黑盒冒烟测试集合
docs/                   模块、集成与运维指南
```

Handler 负责 HTTP 输入输出的验证和映射；Service 负责业务决策；Store Adapter 负责 PostgreSQL 访问和事务。新增资源前请阅读 [CRUD 模块开发指南](docs/crud-module-guide.md)。

## 开发与验证

```sh
make check
```

该命令执行格式检查、`go vet`、sqlc 漂移检查、单元测试、race 测试、Testcontainers 集成测试，以及 OpenAPI JSON/TypeScript 漂移检查。集成测试阶段需要可用的 Docker。

| 命令 | 用途 |
| --- | --- |
| `make fmt` / `make fmt-check` | 格式化/检查 Go 源码 |
| `make vet` | 静态分析 |
| `make test` / `make test-race` | 单元测试及 race 检测 |
| `make test-integration` | 带 integration 标签的测试，超时 300 秒 |
| `make sqlc` / `make sqlc-check` | 生成/检查查询代码 |
| `make openapi` / `make openapi-check` | 生成/检查 OpenAPI JSON |
| `make openapi-types` / `make openapi-types-check` | 生成/检查 TypeScript 类型 |
| `make migrate-up` / `make migrate-down` | 执行/回滚 goose 迁移，需要 DATABASE_URL |
| `make migrate-river` | 执行 River 迁移，需要 DATABASE_URL |
| `make seed` | 创建/更新本地演示管理员，需要 DATABASE_URL |
| `make queue-ping MSG=hello` | 向 worker 投递测试任务，需要 DATABASE_URL |
| `make compose-up` / `make compose-down` | 启动 PostgreSQL / 停止本地 Compose 服务 |

Docker 不可用时，执行非容器检查，并明确标注集成测试未经验证：

```sh
make fmt-check vet sqlc-check test test-race openapi-check openapi-types-check
```

修改 SQL 或 API 后执行：

```sh
make sqlc
make openapi
make openapi-types
make check
```

生成的 SQL 查询代码和前端契约应随相关改动一并提交，不要手工修改 `internal/store/query/`。离线 OpenAPI 生成器无需数据库。TypeScript 类型通过 `openapi-typescript@7.13.0` 生成，优先使用 Bun，回退到 `npx`。

## 部署与可观测性

```sh
make docker-build
```

Dockerfile 构建四个静态二进制：`/api`、`/worker`、`/river-migrate` 和 `/healthcheck`。最终镜像以非 root 用户运行于 distroless，默认入口为 `/api`。

API 与 worker 应作为独立进程部署，并使用一致配置。先在镜像外使用 goose 执行应用迁移，再执行 River 迁移，然后启动服务。镜像包含 River 迁移程序，但不包含 goose 和应用迁移文件。

`compose.prod.yaml` 是单机演练模板，不是可以直接投入生产的安全配置。其中包含本地数据库凭据和 Mailpit 设置；使用前应审核并补齐所有必要配置，包括 `MFA_ENCRYPTION_KEY`。

- 使用 `/healthz` 做存活探测，`/readyz` 做就绪探测；数据库故障应停止分配流量，而非引发存活探测重启循环。
- 镜像的 HTTP healthcheck 仅适用于 API，worker 和一次性迁移容器应关闭该检查。
- API 优雅关闭最多等待 `HTTP_SHUTDOWN_TIMEOUT_SECONDS`；worker 单独设置了 30 秒退出等待。
- 指标覆盖 HTTP 请求数和延迟、限流拒绝、数据库连接池、队列状态和 Go runtime。
- 设置 `OTEL_EXPORTER_OTLP_ENDPOINT` 后导出 HTTP/数据库追踪；日志包含 request ID，启用追踪时还包含 trace/span ID。

启动本地可观测性面板：

```sh
make observability-up
```

Grafana、Prometheus、Jaeger、指标及告警规则见 [可观测性指南](docs/observability.md)。

### 持续集成

| Workflow | 触发方式与行为 |
| --- | --- |
| [CI](.github/workflows/ci.yml) | main 推送和 PR：完整 `make check` |
| [Security](.github/workflows/security.yml) | main 推送、PR 和每周定时：`govulncheck` |
| [Container](.github/workflows/container.yml) | PR：构建并扫描 amd64/arm64；main 和版本标签推送在扫描成功后额外发布多架构镜像到 GHCR |
| [Smoke](.github/workflows/smoke-local.yml) | 手动触发：Newman 黑盒测试 |

Trivy 阻断**已有修复版本的 HIGH/CRITICAL** 系统和依赖库漏洞。扫描通过不代表不存在任何漏洞。容器镜像发布于 `ghcr.io/aklmans/wow-dashboard-api`，PR 构建不会发布镜像。

## 更多文档

- [前端接入](docs/frontend-integration.md)
- [CRUD 模块开发](docs/crud-module-guide.md)
- [审计策略](docs/audit-policy.md)
- [运维指南](docs/operations.md)
- [部署手册](docs/deployment.md)
- [可观测性](docs/observability.md)
- [Postman 冒烟测试](postman/README.md)

## 许可证

本项目采用 [MIT 许可证](LICENSE)。

Copyright (c) 2026 Akman.
