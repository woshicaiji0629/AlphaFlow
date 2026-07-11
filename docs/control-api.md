# Control API 与量化控制台

`backend/go-service/control-api` 是基于 Gin 的控制面 API，使用 PostgreSQL 保存用户、Session、交易账户、权限、策略目录、策略版本、订阅、公开表现和审计日志。`frontend` 是 React 19、TypeScript、Vite 和 TanStack Query 构建的控制台。

当前角色为 `admin` 和 `user`。业务数据按当前 Session 用户隔离；管理员也可以交易，同时额外看到后台模块。普通用户只能查看已发布且有权访问的策略和官方表现，不能任意发起回测。

## 分层

```text
internal/api/router.go              集中声明路由和中间件顺序
internal/api/controller/            请求解析、Service 调用、HTTP 响应
internal/api/requestcontext/        当前 Session 上下文
internal/api/response/              统一错误响应
internal/service/                   业务规则
internal/repository/                持久化接口
internal/infrastructure/postgres/   PostgreSQL 实现和 migration
```

依赖方向为 `router -> controller -> service -> repository`。写接口依次经过来源、Session、角色和 CSRF 校验；管理员策略写入与审计日志使用同一事务。完整 API 表以 `control-api/internal/api/router.go` 为准。

## 策略管理

策略算法以测试过的 Go 代码存在于 `pkg/strategies/<name>`，并在 `pkg/strategyregistry` 注册。后台不能编辑或执行任意代码，只维护名称、说明、参数、风险等级、可见范围、版本和发布状态。

每个策略版本是独立记录，唯一键为 `(code, version)`。已发布版本只读；变更配置需要复制为新草稿版本。当前只允许发布到虚拟交易，`live_enabled` 强制关闭。

## 验证

```sh
cd backend/go-service
GOCACHE=/private/tmp/alphaflow-go-cache GO111MODULE=on go test ./control-api/...

cd frontend
npm run typecheck
npm run build
```
