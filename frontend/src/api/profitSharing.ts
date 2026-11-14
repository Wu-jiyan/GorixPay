import http from './client';
import type { Paginated, PageQuery } from './types';

export interface ProfitReceiver {
  id: string;
  merchant_id: string;
  receiver_type: string;
  account: string;
  name: string;
  /** 万分比：100 = 1% */
  rate_bp: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ProfitReceiverInput {
  merchant_id: string;
  receiver_type: string;
  account: string;
  name: string;
  rate_bp: number;
  status?: string;
}

export interface ProfitSharingOrder {
  id: string;
  order_id: string;
  merchant_id: string;
  receiver_id: string;
  /** 分 */
  amount: number;
  status: string;
  ledger_transaction_id?: string | null;
  channel_batch_no?: string | null;
  error?: string | null;
  created_at?: string;
  updated_at?: string;
}

export const profitSharingApi = {
  async receivers(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<ProfitReceiver> | ProfitReceiver[]>(
      '/admin/profit-sharing/receivers',
      { params: query },
    );
    return data;
  },
  async createReceiver(payload: ProfitReceiverInput) {
    const { data } = await http.post<ProfitReceiver>('/admin/profit-sharing/receivers', payload);
    return data;
  },
  async updateReceiver(id: string, payload: ProfitReceiverInput) {
    const { data } = await http.patch<ProfitReceiver>(`/admin/profit-sharing/receivers/${id}`, payload);
    return data;
  },
  async deleteReceiver(id: string) {
    await http.delete(`/admin/profit-sharing/receivers/${id}`);
  },
  async orders(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<ProfitSharingOrder> | ProfitSharingOrder[]>(
      '/admin/profit-sharing/orders',
      { params: query },
    );
    return data;
  },
  async retryOrder(id: string) {
    const { data } = await http.post<ProfitSharingOrder>(`/admin/profit-sharing/orders/${id}/retry`);
    return data;
  },
};
