import axios from 'axios';

export const http = axios.create({
  baseURL: '/api/v1',
  timeout: 20000,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
});

http.interceptors.request.use((config) => {
  const token = localStorage.getItem('gorixpay_token');
  if (token) {
    config.headers = config.headers ?? {};
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

http.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error.response?.status;
    const url: string = error.config?.url ?? '';
    if (status === 401 && !url.includes('/auth/')) {
      const path = window.location.pathname;
      const isAdmin = path.startsWith('/admin');
      const target = isAdmin ? '/admin/login' : '/login';
      if (path !== target) {
        window.location.replace(target);
      }
    }
    return Promise.reject(error);
  },
);

export function errorMessage(error: unknown, fallback = '请求失败，请稍后重试'): string {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data as Record<string, unknown> | undefined;
    if (data && typeof data.message === 'string') return data.message;
    if (data && typeof data.msg === 'string') return data.msg;
    if (error.response?.status === 401) return '登录已失效，请重新登录';
    return error.message || fallback;
  }
  if (error instanceof Error) return error.message;
  return fallback;
}

export default http;
