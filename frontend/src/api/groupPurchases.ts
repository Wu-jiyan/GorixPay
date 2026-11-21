import http from './client';
import type { Paginated, PageQuery } from './types';

export interface GroupPurchase {
  id: string;
  merchant_id: string;
  group_id: string;
  price_cents: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface GroupPurchaseQuery extends PageQuery {
  merchant_id?: string;
}

/**
 * 用户组套餐目录：GET /admin/group-plans。
 * 字段对齐 internal/groupshop/model.go 的 Plan：
 * id / name / rate_bp / settle_mode / status / price_cents / created_at / updated_at。
 */
export interface GroupPlan {
  id: string;
  name: string;
  rate_bp: number;
  settle_mode?: string;
  status?: string;
  price_cents: number;
  created_at?: string;
  updated_at?: string;
}

export const groupPurchasesApi = {
  async list(query: GroupPurchaseQuery = {}) {
    const { data } = await http.get<Paginated<GroupPurchase> | GroupPurchase[]>('/admin/group-purchases', {
      params: query,
    });
    return data;
  },
  /** 套餐目录（只读）：GET /admin/group-plans */
  async plans() {
    const { data } = await http.get<{ items: GroupPlan[] } | GroupPlan[]>('/admin/group-plans');
    return data;
  },
  async refund(id: string) {
    const { data } = await http.post<GroupPurchase>(`/admin/group-purchases/${id}/refund`);
    return data;
  },
};
