import axios from 'axios';

// 纯 cookie 会话：后端登录时下发 HttpOnly 的 gorixpay_session，浏览器自动携带。
// 前端不再持久化任何 token，也不再注入 Authorization 头（审计 P0：token 双通道）。

export const http = axios.create({
  baseURL: import.meta.env.VITE_API_BASE || '/api/v1',
  timeout: 20000,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
});

export type UnauthorizedHandler = () => void | Promise<void>;

let unauthorizedHandler: UnauthorizedHandler | null = null;

/**
 * 由 router 注册：401 时统一清会话并做一次跳转。
 * 请求层不直接依赖 store/router，避免循环引用。
 */
export function setUnauthorizedHandler(handler: UnauthorizedHandler | null) {
  unauthorizedHandler = handler;
}

const STATUS_MESSAGES: Record<number, string> = {
  400: '请求参数不正确',
  401: '登录已失效，请重新登录',
  403: '没有权限执行该操作',
  404: '接口不存在或尚未开通',
  409: '数据状态已变化，请刷新后重试',
  429: '操作太频繁，请稍后再试',
  500: '服务异常，请稍后重试',
  502: '服务暂时不可用，请稍后重试',
  503: '服务暂时不可用，请稍后重试',
  504: '服务响应超时，请稍后重试',
};

http.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error.response?.status;
    const url: string = error.config?.url ?? '';
    // /auth/ 下的 401 交给调用方（登录页显示密码错误、ensureSession 判定未登录），不做全局跳转
    if (status === 401 && !url.includes('/auth/')) {
      void unauthorizedHandler?.();
    }
    return Promise.reject(error);
  },
);

export function errorMessage(error: unknown, fallback = '请求失败，请稍后重试'): string {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data;
    // Blob 响应（CSV 导出等）解析不出 JSON，直接按状态码给中文提示
    if (data && typeof data === 'object' && !(data instanceof Blob)) {
      const record = data as Record<string, unknown>;
      if (typeof record.message === 'string' && record.message) return record.message;
      if (typeof record.msg === 'string' && record.msg) return record.msg;
    }
    const status = error.response?.status;
    if (status && STATUS_MESSAGES[status]) return STATUS_MESSAGES[status];
    if (error.code === 'ECONNABORTED') return '请求超时，请稍后重试';
    if (!error.response) return '网络异常，请检查网络后重试';
    return fallback;
  }
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

export default http;
