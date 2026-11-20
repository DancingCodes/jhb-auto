# jhb-auto 开发说明

## 项目用途

这是一个 Go HTTP 服务，自动登录聚合吧服务商系统并查询代理列表。

## 目录结构

```text
cmd/api/main.go               服务启动入口
internal/client.go            上游接口客户端及登录加密
internal/server.go            HTTP 路由
internal/service.go           登录和代理列表流程编排
config/clients.example.json   客户到账号文件的映射示例
config/clients.json           本机客户映射（不提交）
config/accounts/client.example.json  客户账号文件示例
config/accounts/*.json        各客户账号配置（真实账号不提交）
```

## 上游接口

- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/auth/public-key`
- `POST https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/auth/login`
- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/query/agent`
- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/query/agent/{id}/descendants`
- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/query/merchant`
- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/query/merchant/{id}/detail`
- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/query/terminal`（按所属代理的 `recommendCode` 和起止时间查询）
- `GET https://jhb-gateway.haiyuebiz.com/pagent/agent-portal/query/trade`

上游接口只由内部客户端调用，不单独暴露为本项目的 HTTP 路由。
GET 请求遇到上游 HTTP 429 时会按 `Retry-After` 等待并重试；未提供该响应头时使用递增等待，最多重试 5 次。

## 启动与测试

```bash
go run ./cmd/api
go test ./...
```

服务监听 `9105`。

- `GET /health`：健康检查
- `GET /api/agents/merged?client=client-yimi&date=YYYY-MM-DD`：仅使用指定客户的账号。先逐页查询全部代理及每条代理的下级，再按各自的推荐码取完指定日期终端；按终端 SN 取完商户及详情，最后取完指定日期交易。返回合并后的 `auto_terminals` 和 `auto_trades`；每条终端包含 `auto_merchants`，每条商户包含 `auto_detail`。终端和交易均使用指定日期 `00:00:00` 至 `23:59:59`，省略 `date` 时默认昨天；商户按 SN 查询，不受 `date` 限制。所有上游列表每页请求 100 条

## 配置

复制 `config/clients.example.json` 为 `config/clients.json`，将请求中的 `client` 映射到 `config/accounts/` 下的独立账号文件。每个账号文件的 `account` 是单个对象，填写用户名、密码和顶级代理 `recommendCode`。程序每次请求只读取并登录指定客户的账号。真实配置及含会话信息的 `1.txt` 保持在 `.gitignore` 中，不得提交、写入日志或放入 Docker 镜像。对外部署时应通过防火墙或反向代理限制接口来源。

## 开发约定

- `main.go` 只启动 HTTP 服务；路由放在 `internal/server.go`；业务编排放在 `internal/service.go`。
- 外部请求放在 `internal/client.go`，不直接写进 HTTP handler。
- 不在代码、日志、提交记录或接口响应中暴露账号、密码、会话 token。
- 修改 Go 代码后运行 `gofmt -w .` 和 `go test ./...`。
