import http from './client';
import type { Paginated, PageQuery } from './types';

export interface SettleRule {
  id: string;
  name: string;
  merchant_id?: string | null;
  min_amount_cents: number;
  /** 万分比费率：100 = 1% */
  fee_bp: number;
  period: string;
  auto_transfer: boolean;
  status?: string;
  last_run_at?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface SettleRuleInput {
  name: string;
  merchant_id: string | null;
  min_amount_cents: number;
  fee_bp: number;
  period: string;
  auto_transfer: boolean;
  status?: string;
}

/** 手动执行一次，后端返回本次生成数量（字段名待定，页面做兼容取值） */
export interface SettleRunResult {
  generated?: number;
  count?: number;
  created?: number;
  message?: string;
}

export const settleRulesApi = {
  async list(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<SettleRule> | SettleRule[]>('/admin/settle-rules', { params: query });
    return data;
  },
  async create(payload: SettleRuleInput) {
    const { data } = await http.post<SettleRule>('/admin/settle-rules', payload);
    return data;
  },
  async update(id: string, payload: SettleRuleInput) {
    const { data } = await http.put<SettleRule>(`/admin/settle-rules/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/settle-rules/${id}`);
  },
  async run(id: string) {
    const { data } = await http.post<SettleRunResult>(`/admin/settle-rules/${id}/run`);
    return data;
  },
};
