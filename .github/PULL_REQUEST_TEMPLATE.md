## 改了什么

<!-- 一句话概括，再列要点。目录、包、文件写清路径。 -->

## 为什么

<!-- 对应 issue 编号、线上故障、协议偏差，或反复出现的重复劳动。 -->

## 怎么验证的

贴命令和结果原文，不要只写「已测试」。后端改动的三条：

```
go build ./...
go vet ./...
go test ./... -count=1
```

结果贴在这里：

```
<paste output>
```

前端有改动时再加一条 `npm --prefix frontend run build`，同样贴输出。

## 契约与迁移

- 对外接口、签名、字段、状态码的变化：
- `migrations/` 下新增或修改的文件：
- 对 V1 SDK、V2 商户、已有数据的影响：

## 检查清单

- [ ] `go build ./...`、`go vet ./...`、`go test ./... -count=1` 全部退出码 0
- [ ] 前端有改动时 `npm --prefix frontend run build` 通过
- [ ] 没有提交密钥、凭据、`.env`、本地 `.local.ps1` 文件
- [ ] 新增迁移编号不重复，SQLite、MySQL、PostgreSQL 都能执行
- [ ] 注释与文档和代码一致，命令、字段名、路径没有写错
