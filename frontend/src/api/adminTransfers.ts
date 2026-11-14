import http from './client';

export interface ManualTransferInput {
  merchant_id: string;
  amount_cents: number;
  destination: string;
  remark?: string;
  idempotency_key?: string;
}

export interface BatchTransferItem {
  amount_cents: number;
  destination: string;
}

export interface BatchTransferInput {
  merchant_id: string;
  items: BatchTransferItem[];
  remark?: string;
}

export interface BatchTransferResult {
  batch_no?: string;
  created?: number;
  count?: number;
}

export const adminTransfersApi = {
  /** 后台手动发起一笔代付 */
  async manual(payload: ManualTransferInput) {
    const { data } = await http.post<{ id?: string }>('/admin/transfers', payload);
    return data;
  },
  /** 后台批量代付 */
  async batch(payload: BatchTransferInput) {
    const { data } = await http.post<BatchTransferResult>('/admin/transfers/batch', payload);
    return data;
  },
};
