import http from './client';
import type { Paginated } from './types';

// 商户资金相关接口集中在 M6：资金明细、分账接收方与流水、红包。
// 说明：这些端点由后端 M3/M4 提供，前端只按本文档约定调用；
// 路径与字段若与后端最终实现有差异，只需要改这一个文件。

export interface RecordsQuery {
  page?: number;
  page_size?: number;
  type?: string;
  created_from?: string;
  created_to?: string;
}

export interface LedgerRecord {
  id?: string;
  created_at: string;
  type: string;
  amount: number;
  direction: 'credit' | 'debit' | string;
  balance_after?: number | null;
  reference?: string | null;
}

export interface ProfitSharingReceiver {
  id: string;
  receiver_type: string;
  account: string;
  name: string;
  rate_bp: number;
  status: string;
  created_at?: string;
  updated_at?: string;
}

export interface ProfitSharingReceiverPayload {
  receiver_type: string;
  account: string;
  name: string;
  rate_bp: number;
}

export interface ProfitSharingOrder {
  id: string;
  order_id?: string;
  trade_no?: string | null;
  out_trade_no?: string | null;
  amount: number;
  status: string;
  error?: string | null;
  created_at: string;
}

export interface RedPacketBatch {
  batch_no: string;
  transfer_no?: string | null;
  merchant_id?: string | null;
  amount: number;
  count: number;
  claimed_count?: number | null;
  status: string;
  remark?: string | null;
  created_at: string;
}

export interface RedPacketPayload {
  amount: number;
  count: number;
  remark?: string;
}

export interface RedPacketCancelResult {
  batch_no: string;
  cancelled: number;
}

const clean = (params: object) => {
  const output: Record<string, unknown> = {};
  Object.entries(params as Record<string, unknown>).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') output[key] = value;
  });
  return output;
};

export const financeApi = {
  async records(query: RecordsQuery = {}) {
    const { data } = await http.get<Paginated<LedgerRecord>>('/merchant/records', { params: clean(query) });
    return data;
  },
  async exportRecords(query: Omit<RecordsQuery, 'page' | 'page_size'> = {}) {
    const response = await http.get<Blob>('/merchant/export/records.csv', {
      params: clean(query),
      responseType: 'blob',
    });
    return response.data;
  },
  async receivers() {
    const { data } = await http.get<{ items: ProfitSharingReceiver[] }>('/merchant/profit-sharing/receivers');
    return data.items;
  },
  async createReceiver(payload: ProfitSharingReceiverPayload) {
    const { data } = await http.post<ProfitSharingReceiver>('/merchant/profit-sharing/receivers', payload);
    return data;
  },
  async updateReceiver(id: string, payload: Partial<ProfitSharingReceiverPayload> & { status?: string }) {
    const { data } = await http.patch<ProfitSharingReceiver>(
      `/merchant/profit-sharing/receivers/${encodeURIComponent(id)}`,
      payload,
    );
    return data;
  },
  async deleteReceiver(id: string) {
    await http.delete(`/merchant/profit-sharing/receivers/${encodeURIComponent(id)}`);
  },
  async sharingOrders(query: RecordsQuery = {}) {
    const { data } = await http.get<Paginated<ProfitSharingOrder>>('/merchant/profit-sharing/orders', {
      params: clean(query),
    });
    return data;
  },
  async redBatches(query: RecordsQuery = {}) {
    const { data } = await http.get<Paginated<RedPacketBatch>>('/merchant/transfers/red', { params: clean(query) });
    return data;
  },
  async createRedPacket(payload: RedPacketPayload) {
    const { data } = await http.post<RedPacketBatch>('/merchant/transfers/red', payload);
    return data;
  },
  /** 整批撤销待审核红包；后端返回被撤销的个数 */
  async cancelRedBatch(batchNo: string, reason: string) {
    const { data } = await http.post<RedPacketCancelResult>(
      `/merchant/transfers/red/${encodeURIComponent(batchNo)}/cancel`,
      { reason },
    );
    return data;
  },
};
