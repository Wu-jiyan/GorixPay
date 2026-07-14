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
      const isAdmin = window.location.pathname.startsWith('/admin');
      if (isAdmin) {
        window.location.href = '/login';
      }
    }
    return Promise.reject(err);
  },
);

export default api;
