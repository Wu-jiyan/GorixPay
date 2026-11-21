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

/**
 * 新建账本账户：POST /admin/ledger/accounts。
 * handler 的必填项是 owner_type / owner_id / purpose / category / currency
 * （internal/admin/httpapi/handler.go createLedgerAccount）。
 */
export interface LedgerAccountInput {
  owner_type: string;
  owner_id: string;
  purpose: string;
  /** asset / liability / equity / revenue / expense */
  category: string;
  currency: string;
}

/**
 * 手工记账：POST /admin/ledger/operations。
 * 后端是"两腿"操作：一个账户 + 一个对手账户 + 金额，方向由 operation_type 决定
 * （credit 记在 account_id 上，debit 反过来），落库后是 pending，还需要审 post。
 */
export interface LedgerOperationInput {
  idempotency_key: string;
  /** credit / debit / transfer */
  operation_type: string;
  account_id: string;
  counterparty_account_id: string;
  amount: number;
  reference_type: string;
  reference_id: string;
  description?: string;
  /** 必须是合法 JSON 字符串，留空为 {} */
  metadata?: string;
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
  /** 手工建账户：返回 backend 的 Account（含 id / status / category） */
  async createAccount(payload: LedgerAccountInput) {
    const { data } = await http.post<Record<string, unknown>>('/admin/ledger/accounts', payload);
    return data;
  },
  /** 手工记账：返回 backend 的 Operation；已存在同名幂等键时返回 200 + 旧单 */
  async createOperation(payload: LedgerOperationInput) {
    const { data } = await http.post<LedgerOperationDetail>('/admin/ledger/operations', payload);
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
  /**
   * 审批资金操作：后端只认 action = post | reject
   * （internal/ledger/service/service.go ProcessOperation）。
   */
  async processOperation(id: string, action: 'post' | 'reject', rejectionReason = '') {
    const { data } = await http.post<LedgerOperationDetail>(`/admin/ledger/operations/${id}/process`, {
      action,
      rejection_reason: rejectionReason,
    });
    return data;
  },
};
