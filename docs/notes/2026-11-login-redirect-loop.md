# 登录后无限刷新：401 拦截器把自己转进死循环

2026-11 的一次排查记录。结论很短，现象很吓人：管理员点「登录」之后浏览器一直刷新，登录页反复重载，服务端日志里全是 401。

## 现象

- 管理端登录页提交账号密码，页面白屏并持续刷新。
- 直接 `POST /api/v1/auth/administrator/login` 用 `admin/admin123` 返回 200，响应里带有效会话。
- 走 Vite 代理 `POST http://localhost:3000/api/v1/auth/administrator/login` 同样 200。

后端能登录，浏览器却在刷，问题在前端拿到响应之后这一步。

## 根因

`frontend/src/api/client.ts` 的响应拦截器只做一件事：见到 401 就 `window.location.replace` 到登录页。它没有判断两件事。

第一，被拒的请求是不是登录接口本身。密码输错时登录接口回 401，拦截器照跳不误，页面重新挂载，登录页的 `catch` 分支再也没机会把「账号或密码错误」显示出来。

第二，当前是不是已经在登录页。`location.replace` 到同一个地址仍然会重新加载文档，应用重新启动，路由守卫再放行一次，于是进入「加载 → 一个请求 401 → replace → 加载」的循环。触发循环的那个请求通常来自 `localStorage` 里残留的旧 token：请求拦截器把它塞进 `Authorization`，服务端当然拒。

三种情况叠在一起才够触发：拦截器不区分登录接口、不比对当前路径、旧 token 还在本地。

## 修复

`client.ts` 里三行改动：

```ts
if (status === 401 && !url.includes('/auth/')) {   // 登录接口的 401 交给页面提示
  const path = window.location.pathname;
  const target = path.startsWith('/admin') ? '/admin/login' : '/login';
  if (path !== target) {                           // 已经在登录页就不再跳
    window.location.replace(target);
  }
}
```

用 `replace` 而不是路由 `push`，避免历史记录里堆一串失效页，后退键会退回到一个立刻又跳走的页面。

## 排除掉的假设

Cookie 未保存、Cookie 属性与本地 HTTP 代理不兼容、管理端布局初始化请求异常、登录成功跳转与路由守卫互相打架。四条都在验证中被否掉：会话 Cookie 能拿到，同一个账号在 curl 下能登录，布局初始化请求在登录成功后是 200。

排查剩下的 `invalid_credentials` 是另一个问题：浏览器提交的账号或密码确实不匹配 `admin/admin123`。它和刷新循环被同一个现象盖住了。

这类循环的特征是每一层单看都合理，合起来发散。给拦截器加一个路径判断是一行代码，但这行代码要有人先意识到循环的边界在哪里。
