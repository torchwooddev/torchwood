# Appwrite 核心功能迁移清单（Go + PostgreSQL）

> [!WARNING] 已作废归档（ARCHIVED）
> 归档日期：2026-08-09
> 归档原因：迁移清单 P0/P1 核心模块（Account/Users/Teams/Databases/Storage/Functions/Health/Project settings 等）已落地，P2/P3 规划已由 `docs/roadmap.md` 接管；本文档为迁移立项时的全景清单，属历史资料。
> 后续信息源：`docs/roadmap.md`、`docs/tech-decision.md`。

> 本文档用于评审：列出 Appwrite 中需要迁移到 Go + PostgreSQL 的功能模块及其详细功能点。  
> 源码根目录：`D:\Codes\baas\appwrite`

## 1. 图例与约定

| 标记 | 含义 |
|------|------|
| `[R]` | 必需服务（`app/config/services.php` 中 `optional => false`） |
| `[O]` | 可选服务（`app/config/services.php` 中 `optional => true`） |
| `[NL]` | 未在 `app/config/services.php` 中注册，但代码中存在 |
| **P0** | 底座能力，必须先实现 |
| **P1** | 核心业务能力，MVP 阶段实现 |
| **P2** | 增强能力，第二阶段实现 |
| **P3** | 可选/周边能力，按需求排期 |

## 2. 迁移阶段总览

### P0 — 底座（必须先有）
- 统一 HTTP API 框架、路由、中间件（CORS / 认证 / 限流 / 审计 / 错误处理）
- Project / Console / Organization 上下文与请求注入
- 基于 PostgreSQL 的 Document Store 抽象（Collection / Attribute / Index / Document / Query）
- 多租户机制（shared tables + `_tenant`、namespace/schema）
- 权限与角色体系（Role / Permission / Authorization）
- 异步队列与任务调度（可纯 PG，也可保留 Redis）
- 缓存抽象（Redis / 内存）
- 日志、链路追踪、Health 基础
- 测试/模拟基础设施（Mock）

### P1 — 核心业务（MVP）
- Projects / Console / Organization
- Account / Auth（含 legacy `account.php` 全部能力）
- Users（含 legacy `users.php`）
- Teams / Memberships
- Databases（Legacy / DocumentsDB / TablesDB；VectorsDB 视需求）
- Storage
- Functions
- Health
- Project settings

### P2 — 增强能力
- Realtime
- Webhooks / Events
- Messaging
- Migrations
- Tokens（文件令牌）
- Presences

### P3 — 可选/周边
- Sites
- Proxy
- VCS
- Avatars
- Locale
- Advisor
- GraphQL

---

## 3. 模块详细清单

### 3.1 Core（平台启动与任务/Worker 注册）

| 属性 | 内容 |
|------|------|
| 优先级 | P0 |
| 状态 | `[NL]` |
| 源码 | `src/Appwrite/Platform/Modules/Core.php` |
| 作用 | 注册平台级 Task / Worker 服务，无直接 HTTP 接口 |

**功能点：**
- 平台启动时注册所有 Task 与 Worker
- 依赖注入容器初始化（DB、Cache、Queue、Event Bus 等）

**迁移备注：** 在 Go 实现中对应 `cmd/server` 与 `cmd/worker` 的初始化代码，以及依赖注入框架。

---

### 3.2 Console（控制台 SPA & 引导 API）

| 属性 | 内容 |
|------|------|
| 优先级 | P0/P1 |
| 状态 | `[R]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Console/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Console/` |

**功能点：**
- Console API init hook
- Console Web init hook
- 获取 Console 变量（`variables`）
- 获取邮件模板
- 列出 OAuth2  providers
- 列出 key scopes
- 列出 organization scopes
- 创建 assistant 查询
- 获取资源可用性
- Web 重定向：root、auth、invite、login、MFA、card、recover、register

**依赖：** Project、OAuth2 配置、邮件模板、权限 scope 配置。

---

### 3.3 Projects（多项目管理）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[R]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Projects/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Projects/` |
| 遗留控制器 | `app/controllers/api/projects.php` |

**功能点：**
- Dev keys：创建、获取、更新、删除、列表
- Projects：创建、更新、列表、更新所属 team
- Project-level action endpoint
- Schedules：创建、获取、列表
- 遗留控制器能力：OAuth2 设置、mock 手机号、邮件模板相关接口

**数据实体：** `projects`、`teams`、`devKeys`、`schedules`。

---

### 3.4 Organization（组织级项目管理）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[R]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Organization/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Organization/` |

**功能点：**
- Init hook
- Organization projects：创建、获取、更新、删除、列表、action

**依赖：** Projects、Teams。

---

### 3.5 Account / Auth（认证与当前用户管理）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 模块源码 | `src/Appwrite/Platform/Modules/Account/`（仅 MFA） |
| 遗留主控制器 | `app/controllers/api/account.php` |

**功能点：**
- 用户注册 / 创建账号
- 获取 / 删除当前账号
- Session 管理：列出、删除全部、获取、删除单个、更新
- 登录方式：
  - 邮箱 + 密码
  - 匿名登录
  - Token 登录
  - OAuth2 登录（授权、回调、重定向、token）
  - Magic URL（创建 + 确认）
  - Email OTP（创建 + 确认）
  - Phone OTP（创建 + 确认）
- MFA：
  - 更新 MFA 设置
  - 列出 MFA factors
  - Authenticators：创建、更新、删除
  - Challenges：创建、更新
  - Recovery codes：创建、获取、更新
- 创建 JWT
- 账号偏好：获取、更新
- 账号日志
- 更新姓名、密码、邮箱、手机、状态、偏好
- 密码找回（创建 + 确认）
- 邮箱验证（创建 + 确认）
- 手机验证（创建 + 确认）
- Push targets：创建、更新、删除
- Identities：列出、删除

**数据实体：** `users`、`sessions`、`tokens`、`identities`、`targets`。

**迁移备注：** 这是最大的遗留控制器之一，Go 实现需要完整重写；密码哈希、session secret、JWT、OTP、OAuth2 均需重新实现。

---

### 3.6 Users（服务端用户管理）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 遗留主控制器 | `app/controllers/api/users.php` |

**功能点：**
- 创建用户（支持多种密码哈希：bcrypt、MD5、Argon2、SHA、PHPass、Scrypt、ScryptModified）
- 列表 / 获取 / 更新 / 删除用户
- 用户 sessions / tokens 管理
- 用户 targets：创建、获取、更新、删除、列表
- 用户 identities：创建、获取、删除、列表
- MFA 管理
- Labels、status、impersonator、usage

**数据实体：** `users`。

**迁移备注：** 与 Account 共享 users collection，注意权限模型差异（server key 可管理所有用户）。

---

### 3.7 Teams / Memberships（团队与成员）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Teams/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Teams/` |

**功能点：**
- Teams：创建、获取、更新名称、删除、列表
- Preferences：获取、更新
- Memberships：创建、获取、更新、删除、列表、更新状态（接受/拒绝邀请）
- Logs：列表

**数据实体：** `teams`、`memberships`。

**迁移备注：** Membership 确认后会生成 `team:*` 和 `member:*` role，影响 Authorization。

---

### 3.8 Databases（数据库服务）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]`（`databases`、`tablesdb`） |
| 服务注册 | `src/Appwrite/Platform/Modules/Databases/Services/Http.php` |
| 注册器 | `Legacy.php`、`DocumentsDB.php`、`TablesDB.php`、`VectorsDB.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Databases/` |

#### 3.8.1 Legacy Databases
- Databases：创建、获取、更新、删除、列表、日志、用量
- Collections：创建、获取、更新、删除、列表、日志、用量
- Documents：创建、获取、更新、删除、upsert、列表、日志
- Bulk documents：更新、删除、upsert
- 文档属性自增 / 自减
- Attributes（按类型）：BigInt、Boolean、Datetime、Email、Enum、Float、Integer、IP、Line、Longtext、Mediumtext、Point、Polygon、Relationship、String、Text、URL、Varchar；获取、删除、列表
- Indexes：创建、获取、删除、列表
- Transactions：创建、获取、更新、删除、列表、operations

#### 3.8.2 DocumentsDB
- Databases：创建、获取、更新、删除、列表、用量
- Collections：创建、获取、更新、删除、列表、用量
- Documents：创建、获取、更新、删除、upsert、列表
- Bulk documents：更新、删除、upsert
- 文档属性自增 / 自减
- Indexes：创建、获取、删除、列表
- Transactions：创建、获取、更新、删除、列表、operations

#### 3.8.3 TablesDB
- Databases：创建、获取、更新、删除、列表、用量
- Tables：创建、获取、更新、删除、列表、日志、用量
- Columns（按类型）：BigInt、Boolean、Datetime、Email、Enum、Float、Integer、IP、Line、Longtext、Mediumtext、Point、Polygon、Relationship、String、Text、URL、Varchar；获取、删除、列表
- Rows：创建、获取、更新、删除、upsert、列表、日志
- Bulk rows：更新、删除、upsert
- Row column 自增 / 自减
- Indexes：创建、获取、删除、列表
- Transactions：创建、获取、更新、删除、列表、operations

#### 3.8.4 VectorsDB（可选，P1/P2 边界）
- Vector databases：创建、获取、更新、删除、列表、用量
- Collections：创建、获取、更新、删除、列表、用量
- Documents：创建、获取、更新、删除、upsert、列表
- Bulk documents：更新、删除、upsert
- Indexes：创建、获取、删除、列表
- Text embeddings 创建
- Transactions：创建、获取、更新、删除、列表、operations

#### 3.8.5 Init
- Database timeout init action

**迁移备注：** 这是迁移工作量最大的模块。需要在 Go 中实现 Utopia Database 的 PostgreSQL adapter：动态 schema、shared tables、权限表 `_perms`、索引、关系、向量扩展（pgvector）、空间扩展（PostGIS）、全文搜索（pg_trgm / to_tsvector）。

---

### 3.9 Storage（对象存储）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Storage/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Storage/` |

**功能点：**
- Buckets：创建、获取、更新、删除、列表
- Files：创建、获取、更新、删除、列表
- 文件交付：download、preview、view、push
- Usage：获取、列表

**数据实体：** `buckets`、`files`。

**迁移备注：** 文件元数据存 PostgreSQL，二进制可放本地磁盘或 S3/MinIO；预览/转换需要图像处理库。

---

### 3.10 Functions（云函数）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Functions/Services/Http.php` |
| Worker 注册 | `src/Appwrite/Platform/Modules/Functions/Services/Workers.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Functions/` |
| 执行器 | `src/Executor/` |

**HTTP 功能点：**
- Functions：创建、获取、更新、删除、列表
- Runtimes：列表
- Specifications：列表
- Deployments：创建、获取、更新、删除、列表、下载、复制、状态更新、模板创建、VCS 创建
- Executions：创建、获取、删除、列表
- Usage：获取、列表
- Variables：创建、获取、更新、删除、列表
- Templates：获取、列表

**Worker：**
- Builds
- Screenshots

**迁移备注：** 需要复用或重写 Docker executor；Go 中可用 `github.com/docker/docker` 调用 Docker API 构建/运行容器；调度可用 cron + PG 任务表。

---

### 3.11 Health（健康检查）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Health/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Health/` |

**功能点：**
- Overall health
- Version
- DB health
- Cache health
- Pub/Sub health
- Time health
- Certificate health
- Storage health / Local storage health
- Antivirus health
- Queue health：audits、webhooks、logs、certificates、builds、databases、deletes、mails、messaging、migrations、functions、stats resources、stats usage、failed jobs
- Stats health

**迁移备注：** 若队列改为 PG 实现，queue health 需改为检查 PG 队列表或 worker 状态。

---

### 3.12 Project（单项目设置）

| 属性 | 内容 |
|------|------|
| 优先级 | P1 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Project/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Project/` |
| 遗留控制器 | `app/controllers/api/project.php` |

**功能点：**
- Init hook
- Project：获取、删除、更新 labels、更新 protocols、更新 services
- Auth methods：更新
- API keys：创建、获取、更新、删除、列表、临时创建（ephemeral）
- Platforms：Android、Apple、Linux、Web、Windows（创建/更新），以及获取/删除/列表
- OAuth2 providers：列出、获取、更新（约 30+ 提供商：Amazon、Apple、Auth0、Authentik、Autodesk、Bitbucket、Bitly、Box、Dailymotion、Discord、Disqus、Dropbox、Etsy、Facebook、Figma、FusionAuth、GitHub、Gitlab、Google、Keycloak、Kick、LinkedIn、Microsoft、Notion、OIDC、Okta、Paypal、PaypalSandbox、Podio、Salesforce、Slack、Spotify、Stripe、Tradeshift、TradeshiftSandbox、Twitch、WordPress、X、Yahoo、Yandex、Zoho、Zoom）
- Mock phone numbers：创建、获取、更新、删除、列表
- Policies：获取/列表 + 更新（membership privacy、password dictionary、password history、password personal data、password strength、session alert、session duration、session invalidation、session limit、user limit）
- SMTP：更新、创建测试
- Email templates：获取、更新、列表
- Variables：创建、获取、更新、删除、列表
- 遗留控制器：project usage endpoint

**迁移备注：** OAuth2 provider 配置数量庞大，可先做常用 provider，其余按需补齐。

---

### 3.13 Realtime（实时订阅）

| 属性 | 内容 |
|------|------|
| 优先级 | P2 |
| 状态 | `[NL]`（核心基础设施） |
| 源码目录 | `src/Appwrite/Realtime/` |
| 消息处理器 | `src/Appwrite/Realtime/Message/Handlers/` |

**功能点：**
- WebSocket 连接管理
- Authentication 握手
- Subscribe / Unsubscribe 频道
- Presence
- Ping
- 事件广播（来自数据库/存储/函数等变更）

**迁移备注：** Go 可用 Gorilla WebSocket；事件广播可通过 PG `LISTEN/NOTIFY` 或保留 Redis Pub/Sub。

---

### 3.14 Webhooks / Events

| 属性 | 内容 |
|------|------|
| 优先级 | P2 |
| 状态 | `[NL]` |
| Webhook 服务注册 | `src/Appwrite/Platform/Modules/Webhooks/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Webhooks/` |
| 事件定义 | `app/config/events.php` |
| 事件发布 | `src/Appwrite/Event/` |

**HTTP 功能点：**
- Init hook
- Webhooks：创建、获取、更新、删除、列表、更新 secret

**功能点：**
- 事件定义与订阅
- Webhook 投递、签名、重试、失败处理
- 与 Queue worker 集成

**迁移备注：** 若队列改为 PG，webhook worker 从 PG 取任务并执行 HTTP 投递。

---

### 3.15 Messaging（消息推送）

| 属性 | 内容 |
|------|------|
| 优先级 | P2 |
| 状态 | `[O]` |
| 遗留主控制器 | `app/controllers/api/messaging.php` |

**功能点：**
- Providers：Mailgun、Sendgrid、SES、Resend、SMTP、Msg91、Telesign、Textmagic、Twilio、Vonage、FCM、APNS 等
- Topics：创建、获取、更新、删除、列表
- Subscribers：创建、获取、更新、删除、列表
- Messages：创建（email / SMS / push）、获取、删除、列表
- Logs

**迁移备注：** 属于独立能力，可复用外部服务 SDK；建议 P2 实现。

---

### 3.16 Migrations（数据迁移）

| 属性 | 内容 |
|------|------|
| 优先级 | P2 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Migrations/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Migrations/` |

**HTTP 功能点：**
- Migrations：创建、获取、更新、删除、列表
- Appwrite source：创建 migration、获取 report
- Firebase source：创建 migration、获取 report
- Supabase source：创建 migration、获取 report
- NHost source：创建 migration、获取 report
- CSV import / export
- JSON import / export

**迁移备注：** 需要大量外部 API/SDK 适配，建议作为独立 worker 实现。

---

### 3.17 Tokens（文件令牌）

| 属性 | 内容 |
|------|------|
| 优先级 | P2 |
| 状态 | `[NL]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Tokens/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Tokens/` |

**功能点：**
- Tokens：创建、获取、更新、删除
- File tokens：创建、获取、更新、删除、列表
- File token action

**依赖：** Storage。

---

### 3.18 Presences（在线状态）

| 属性 | 内容 |
|------|------|
| 优先级 | P2/P3 |
| 状态 | `[NL]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Presences/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Presences/` |

**功能点：**
- Upsert presence
- Get / Update / Delete presence
- List presences
- Get usage

**依赖：** Realtime / session。

---

### 3.19 Avatars（头像与图片辅助）

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Avatars/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Avatars/` |

**功能点：**
- 通用 avatar action
- 获取浏览器图标
- 获取信用卡图标
- 获取 favicon
- 获取国旗
- 获取/处理图片
- 获取姓名首字母头像
- 获取 QR 码
- 获取截图
- Cloud cards：front、back、OG image

**迁移备注：** 主要是图像生成/抓取，依赖图像库和外部服务。

---

### 3.20 Locale（地区与语言）

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 遗留主控制器 | `app/controllers/api/locale.php` |

**功能点：**
- 获取 locale 信息
- 国家列表、EU 国家、电话代码、洲、货币、语言、locale codes

**迁移备注：** 数据 mostly static，可做成配置文件或 PG 静态表。

---

### 3.21 Advisor（项目诊断建议）

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Advisor/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Advisor/` |

**功能点：**
- Reports：获取、列表、删除
- Insights：获取、列表

---

### 3.22 Sites（静态/SSR 站点托管）

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Sites/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Sites/` |

**功能点：**
- Sites：创建、获取、更新、删除、列表、更新 active deployment
- Frameworks：列表
- Deployments：创建、获取、更新、删除、列表、下载、复制、状态更新、模板创建、VCS 创建
- Logs：获取、列表、删除
- Variables：创建、获取、更新、删除、列表
- Templates：获取、列表
- Usage：获取、列表
- Specifications：列表

**依赖：** Functions/Executor、VCS、Storage、Proxy。

---

### 3.23 Proxy（域名与路由规则）

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/Proxy/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/Proxy/` |

**功能点：**
- Rules：创建 API rule、site rule、function rule、redirect rule
- Rules：获取、更新状态、删除、列表

**依赖：** Sites、Functions、Certificates。

---

### 3.24 VCS（Git 提供商集成）

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 服务注册 | `src/Appwrite/Platform/Modules/VCS/Services/Http.php` |
| 源码目录 | `src/Appwrite/Platform/Modules/VCS/` |

**功能点：**
- GitHub authorization & callback
- GitHub deployment endpoint
- GitHub events：create
- Installations：创建、获取、删除、列表
- Repositories：创建、获取、列表、列出 branches、获取 contents、创建 detections

**依赖：** Functions、Sites。

---

### 3.25 GraphQL

| 属性 | 内容 |
|------|------|
| 优先级 | P3 |
| 状态 | `[O]` |
| 遗留主控制器 | `app/controllers/api/graphql.php` |

**功能点：**
- GraphQL query / mutation endpoint
- 复用 REST 底层服务

**迁移备注：** 可在 REST 稳定后通过 schema 生成包装层实现。

---

### 3.26 Compute（共享计算基类）

| 属性 | 内容 |
|------|------|
| 优先级 | P0/P1（作为 Functions/Sites 基础） |
| 状态 | `[NL]` |
| 源码目录 | `src/Appwrite/Platform/Modules/Compute/` |

**功能点：**
- 共享 compute base class
- Specification / Specification validator

**迁移备注：** 无独立 HTTP，作为 Functions 和 Sites 的公共代码抽象。

---

### 3.27 Mock（测试基础设施）

| 属性 | 内容 |
|------|------|
| 优先级 | P0（用于测试） |
| 状态 | `[R]` |
| 控制器 | `app/controllers/mock.php` |

**功能点：**
- 测试用 mock endpoint
- 用于 SDK/E2E 测试

---

## 4. 平台级 Task 与 Worker

### 4.1 Tasks（命令行/定时任务）

| 来源 | `src/Appwrite/Platform/Services/Tasks.php` |
|------|-------------------------------------------|

**功能点：**
- Doctor
- Install
- Interval
- Maintenance
- Migrate
- QueueRetry
- SDKs
- SSL
- Screenshot
- ScheduleFunctions
- ScheduleExecutions
- ScheduleMessages
- Specs
- Upgrade
- Vars
- Version
- StatsResources
- TimeTravel

### 4.2 Workers（后台队列消费者）

| 来源 | `src/Appwrite/Platform/Services/Workers.php` |
|------|---------------------------------------------|

**功能点：**
- Audits
- Certificates
- Deletes
- Executions
- Functions
- Mails
- Messaging
- Webhooks
- StatsUsage
- Migrations
- StatsResources

**模块专属 Worker：**
- Functions 模块：`Builds`、`Screenshots`

---

## 5. 迁移优先级建议表

| 优先级 | 模块 |
|--------|------|
| P0 | Core、API 框架、Console（部分）、权限/角色、Document DB Adapter、Queue、Cache、Compute、Mock |
| P1 | Projects、Organization、Account、Users、Teams、Databases、Storage、Functions、Health、Project settings |
| P2 | Realtime、Webhooks/Events、Messaging、Migrations、Tokens、Presences |
| P3 | Sites、Proxy、VCS、Avatars、Locale、Advisor、GraphQL |

---

## 6. 关键依赖与技术映射

| Appwrite 组件 | Go 替代建议 |
|---------------|------------|
| Utopia HTTP / Swoole | Echo / Fiber / Gin 或标准库 |
| Utopia Database + Postgres adapter | 自行实现 `internal/database/adapter/postgres` |
| Utopia Auth / Proofs | `golang.org/x/crypto` + `golang-jwt/jwt` + `pquerna/otp` |
| Utopia Queue（Redis） | PG 队列表 + `SKIP LOCKED` / `LISTEN/NOTIFY`，或保留 Redis |
| Utopia Cache（Redis） | `go-redis` / `ristretto` |
| Executor（Docker） | `github.com/docker/docker` |
| ImageMagick 预览 | `disintegration/imaging` 或调用外部服务 |
| S3 存储 | `aws-sdk-go-v2` / minio-go |

---

## 7. 评审后下一步

1. 确认每个模块的优先级（尤其是 P1/P2/P3 边界）。
2. 确认是否保留 Redis 还是纯 PostgreSQL 栈。
3. 确认是否需要与现有 Appwrite SDK/Console 保持 API 兼容。
4. 确认 Functions/Sites 的执行环境（Docker / Kubernetes / 其他）。
5. 确认 Storage 后端（本地 / S3 / MinIO）。
6. 基于确认结果，输出下一阶段详细设计：
   - 数据库 schema（系统 collections + 动态 collections）
   - 模块接口与路由设计
   - 队列 schema 与 worker 设计
   - 开发里程碑与测试策略
