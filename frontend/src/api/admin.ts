import api from './client';

export interface AdminLoginRequest {
  identifier: string;
  password: string;
}

export interface DashboardData {
  merchant_count: number;
  order_count: number;
  paid_order_count: number;
  paid_amount: number;
}

export interface Merchant {
  id: string;
  display_name: string;
  email: string;
  phone: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface Order {
  id: string;
  trade_no: string;
  out_trade_no: string;
  merchant_id: string;
  name: string;
  amount: number;
  status: string;
  payment_type: string;
  client_ip: string;
  notify_url: string;
  return_url: string;
  paid_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface Channel {
  id: string;
  name: string;
  plugin_code: string;
  payment_type: string;
  priority: number;
  weight: number;
  status: string;
  configured: boolean;
  created_at: string;
  updated_at: string;
}

export interface ChannelRequest {
  name: string;
  plugin_code: string;
  payment_type: string;
  priority: number;
  weight: number;
  status: string;
  api_url: string;
  pid: string;
  key: string;
}

export interface SystemSetting {
  key: string;
  value: string;
  updated_at: string;
}

export interface AuditEvent {
  id: string;
  method: string;
  path: string;
  status: number;
  resource_id: string;
  request_id: string;
  principal_id: string;
  created_at: string;
}

export interface Refund {
  id: string;
  merchant_id: string;
  order_id: string;
  trade_no: string;
  amount: number;
  status: string;
  reason: string;
  rejection_reason: string;
  processed_by: string;
  processed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface Settlement {
  id: string;
  merchant_id: string;
  amount: number;
  status: string;
  bank_account: string;
  bank_name: string;
  rejection_reason: string;
  processed_by: string;
  processed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export const adminApi = {
  login: (data: AdminLoginRequest) => api.post('/auth/administrator/login', data),

  dashboard: () => api.get<DashboardData>('/admin/dashboard'),

  merchants: (params: { page?: number; page_size?: number; keyword?: string; status?: string }) =>
    api.get<PaginatedResponse<Merchant>>('/admin/merchants', { params }),
  updateMerchantStatus: (id: string, status: string) =>
    api.patch(`/admin/merchants/${id}/status`, { status }),

  orders: (params: Record<string, string | number | undefined>) =>
    api.get<PaginatedResponse<Order>>('/admin/orders', { params }),
  order: (id: string) => api.get<Order>(`/admin/orders/${id}`),
  closeOrder: (id: string) => api.post(`/admin/orders/${id}/close`),

  channels: () => api.get<{ items: Channel[] }>('/admin/channels'),
  createChannel: (data: ChannelRequest) => api.post('/admin/channels', data),
  updateChannel: (id: string, data: ChannelRequest) => api.put(`/admin/channels/${id}`, data),
  updateChannelStatus: (id: string, status: string) => api.patch(`/admin/channels/${id}/status`, { status }),

  settings: () => api.get<SystemSetting[]>('/admin/settings'),
  updateSetting: (key: string, value: string) => api.put(`/admin/settings/${key}`, { value }),

  auditEvents: (params: { page?: number; page_size?: number }) =>
    api.get<PaginatedResponse<AuditEvent>>('/admin/audit-events', { params }),

  refunds: (params: Record<string, string | number | undefined>) =>
    api.get<PaginatedResponse<Refund>>('/admin/refunds', { params }),
  refund: (id: string) => api.get<Refund>(`/admin/refunds/${id}`),
  processRefund: (id: string, data: { action: string; rejection_reason?: string }) =>
    api.post(`/admin/refunds/${id}/process`, data),

  settlements: (params: Record<string, string | number | undefined>) =>
    api.get<PaginatedResponse<Settlement>>('/admin/settlements', { params }),
  settlement: (id: string) => api.get<Settlement>(`/admin/settlements/${id}`),
  processSettlement: (id: string, data: { action: string; rejection_reason?: string }) =>
    api.post(`/admin/settlements/${id}/process`, data),
};
