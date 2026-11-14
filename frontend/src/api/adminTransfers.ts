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

export interface RedBatch {
  batch_no: string;
  transfer_no?: string;
  merchant_id: string;
  amount: number;
  count?: number;
  claimed_count?: number;
  status?: string;
  remark?: string;
  created_at?: string;
}

export interface RedBatchQuery {
  page?: number;
  page_size?: number;
  merchant_id?: string;
}

export interface RedCancelResult {
  batch_no?: string;
  cancelled?: number;
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
  /** 红包批次列表（GET /admin/transfers/red） */
  async redBatches(query: RedBatchQuery = {}) {
    const { data } = await http.get<{
      items?: RedBatch[];
      total?: number;
      page?: number;
      page_size?: number;
    }>('/admin/transfers/red', { params: query });
    return data;
  },
  /** 撤销红包批次（POST /admin/transfers/red/:batch_no/cancel） */
  async cancelRed(batchNo: string, reason = '') {
    const { data } = await http.post<RedCancelResult>(
      `/admin/transfers/red/${encodeURIComponent(batchNo)}/cancel`,
      { reason },
    );
    return data;
  },
};
