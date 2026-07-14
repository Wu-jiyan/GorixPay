import axios from 'axios';

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      const pathname = window.location.pathname;
      console.log('[401] redirect from', pathname, 'request:', err.config?.url);
      if (pathname === '/login' || pathname === '/merchant/login') {
        return Promise.reject(err);
      }
      if (pathname.startsWith('/merchant')) {
        window.location.href = '/merchant/login';
      } else {
        window.location.href = '/login';
      }
    }
    return Promise.reject(err);
  },
);

export default api;
