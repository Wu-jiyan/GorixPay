import http from './client';
import type { Paginated, PageQuery } from './types';

/**
 * 资金单据（充值 / 保证金）只读列表。
 * 字段对齐 internal/funding/http.go 的 fundingOrders：
 * id / trade_no / out_trade_no / payment_type / name / amount / currency / status / kind
 * / notify_url / return_url / client_ip / attach / channel_trade_no / paid_at / expires_at
 * / created_at / updated_at。
 */
export interface FundingOrder {
  id: string;
  trade_no: string;
  out_trade_no: string;
  payment_type: string;
  name: string;
  amount: number;
  currency: string;
  status: string;
  kind: 'recharge' | 'deposit' | string;
  notify_url?: string | null;
  return_url?: string | null;
  client_ip?: string | null;
  attach?: string | null;
  channel_trade_no?: string | null;
  paid_at?: string | null;
  expires_at?: string | null;
  created_at: string;
  updated_at?: string | null;
}

export interface FundingQuery extends PageQuery {
  merchant_id?: string;
}

const clean = (params: object) => {
  const output: Record<string, unknown> = {};
  Object.entries(params as Record<string, unknown>).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') output[key] = value;
  });
  return output;
};

export const adminFundingApi = {
  /** 充值单据：GET /admin/funding/recharge（只回 page / page_size / merchant_id，无 status 过滤） */
  async recharge(query: FundingQuery = {}) {
    const { data } = await http.get<Paginated<FundingOrder>>('/admin/funding/recharge', {
      params: clean(query),
    });
    return data;
  },
  /** 保证金单据：GET /admin/funding/deposit */
  async deposit(query: FundingQuery = {}) {
    const { data } = await http.get<Paginated<FundingOrder>>('/admin/funding/deposit', {
      params: clean(query),
    });
    return data;
  },
};