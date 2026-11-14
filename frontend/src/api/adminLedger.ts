import http from './client';
import type { Paginated, PageQuery } from './types';

/** 账本账户详情：后端返回 {account, balance} 视图 */
export type LedgerAccountDetail = Record<string, unknown>;

export interface LedgerEntryRow {
  id?: string;
  transaction_id?: string;
  account_id?: string;
  sequence?: number;
  direction?: string;
  bucket?: string;
  amount?: number;
  balance_after?: number;
  created_at?: string;
}

export interface LedgerTransactionDetail {
  transaction?: Record<string, unknown>;
  entries?: LedgerEntryRow[];
}

export interface LedgerOperationDetail {
  id: string;
  operation_type?: string;
  status?: string;
  account_id?: string;
  counterparty_account_id?: string;
  amount?: number;
  reference_type?: string;
  reference_id?: string;
  description?: string;
  metadata?: string;
  created_by?: string;
  processed_by?: string | null;
  transaction_id?: string | null;
  rejection_reason?: string | null;
  created_at?: string;
  processed_at?: string | null;
}

export const adminLedgerApi = {
  async account(id: string) {
    const { data } = await http.get<LedgerAccountDetail>(`/admin/ledger/accounts/${id}`);
    return data;
  },
  async entries(accountId: string, query: PageQuery = {}) {
    const { data } = await http.get<Paginated<LedgerEntryRow>>(`/admin/ledger/accounts/${accountId}/entries`, {
      params: query,
    });
    return data;
  },
  async transaction(id: string) {
    const { data } = await http.get<LedgerTransactionDetail>(`/admin/ledger/transactions/${id}`);
    return data;
  },
  async operation(id: string) {
    const { data } = await http.get<LedgerOperationDetail>(`/admin/ledger/operations/${id}`);
    return data;
  },
  async processOperation(id: string, action: 'approve' | 'reject', rejectionReason = '') {
    const { data } = await http.post<LedgerOperationDetail>(`/admin/ledger/operations/${id}/process`, {
      action,
      rejection_reason: rejectionReason,
    });
    return data;
  },
};
