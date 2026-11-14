import http from './client';

export type SettlementBatchAction = 'approve' | 'reject';

export interface SettlementBatchResult {
  items?: unknown[];
  processed?: number;
}

export interface AutoTransferResult {
  skipped?: boolean;
  reason?: string;
  transfer_no?: string;
  transfer_status?: string;
}

export const adminSettlementsApi = {
  /** 批量审批结算单：POST /admin/settlements/batch-process */
  async batchProcess(ids: string[], action: SettlementBatchAction, reason = '') {
    const { data } = await http.post<SettlementBatchResult>('/admin/settlements/batch-process', {
      ids,
      action,
      reason,
    });
    return data;
  },
  /** 结算单审核通过后手动触发自动打款：POST /admin/settlements/:id/auto-transfer */
  async autoTransfer(id: string) {
    const { data } = await http.post<AutoTransferResult>(`/admin/settlements/${id}/auto-transfer`);
    return data;
  },
};
