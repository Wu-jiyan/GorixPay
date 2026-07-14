import api from './client';

export interface Credential {
  id: string;
  merchant_id: string;
  public_key: string;
  created_at: string;
}

export interface MerchantOrder {
  id: string;
  trade_no: string;
  out_trade_no: string;
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

export const merchantApi = {
  orders: (params: Record<string, string | number | undefined>) =>
    api.get('/merchant/orders', { params }),
  order: (id: string) => api.get(`/merchant/orders/${id}`),

  refunds: (params: Record<string, string | number | undefined>) =>
    api.get('/merchant/refunds', { params }),
  createRefund: (data: { order_id: string; amount: number; reason: string }) =>
    api.post('/merchant/refunds', data),
  refund: (id: string) => api.get(`/merchant/refunds/${id}`),

  settlements: (params: Record<string, string | number | undefined>) =>
    api.get('/merchant/settlements', { params }),
  createSettlement: (data: { amount: number; bank_account: string; bank_name: string }) =>
    api.post('/merchant/settlements', data),
  settlement: (id: string) => api.get(`/merchant/settlements/${id}`),

  getCredentials: () => api.get<Credential[]>('/merchant/credentials'),
  rotateKey: (id: string) => api.post(`/merchant/credentials/${id}/rotate`),
};
