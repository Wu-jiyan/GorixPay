# 登录无限刷新调试

- Session ID: `login-refresh-loop`
- Status: `[OPEN]`
- Symptom: 管理员登录页点击登录后浏览器无限刷新。

## Hypotheses

1. 登录 Cookie 未保存，进入管理端后接口返回 401。
2. 登录请求自身返回 401，被全局拦截器重定向到当前登录页。
3. Cookie 属性与本地 HTTP/Vite 代理不兼容。
4. 管理端布局初始化请求持续返回 401。
5. 登录成功跳转与 401 重定向产生路由循环。

## Evidence

- 直接请求 `POST http://localhost:8080/api/v1/auth/administrator/login`，提交 `admin/admin123`，返回 200。
- 通过 Vite 代理请求 `POST http://localhost:3000/api/v1/auth/administrator/login`，提交相同数据，返回 200。
- 登录成功响应包含管理员 `admin` 和有效会话 Token，排除管理员记录、密码哈希、后端认证和 Vite 代理异常。
- 用户当前收到 `invalid_credentials`，说明浏览器实际提交的账号或密码与 `admin/admin123` 不一致。
- 401 拦截器此前会在 `/login` 收到 401 后再次导航 `/login`，确认这是无限刷新的直接原因；当前已增加登录页重定向保护。

## Current conclusion

- 假设 1、3、4、5 已排除。
- 假设 2 已确认并修复：登录请求返回 401 时不应重定向到当前登录页。
- 当前剩余的 `invalid_credentials` 是凭据输入不匹配，不是刷新循环或服务端账号损坏。
