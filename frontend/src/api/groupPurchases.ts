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

export const groupPurchasesApi = {
  async list(query: GroupPurchaseQuery = {}) {
    const { data } = await http.get<Paginated<GroupPurchase> | GroupPurchase[]>('/admin/group-purchases', {
      params: query,
    });
    return data;
  },
  async refund(id: string) {
    const { data } = await http.post<GroupPurchase>(`/admin/group-purchases/${id}/refund`);
    return data;
  },
};
