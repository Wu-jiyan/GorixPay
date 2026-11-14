import http from './client';
import type { Paginated, PageQuery } from './types';

export interface MerchantGroup {
  id: string;
  name: string;
  /** 万分比费率：100 = 1% */
  rate_bp: number;
  /** 后端可能给数组，也可能给 JSON 字符串 */
  pay_types?: string[] | string;
  settle_mode?: string;
  status?: string;
  merchant_count?: number;
  /** 买断价（分），0 表示不卖 */
  price_cents?: number;
  created_at?: string;
  updated_at?: string;
}

export interface MerchantGroupInput {
  name: string;
  rate_bp: number;
  pay_types: string[];
  settle_mode: string;
  status?: string;
}

export const groupsApi = {
  async list(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<MerchantGroup> | MerchantGroup[]>('/admin/groups', { params: query });
    return data;
  },
  async create(payload: MerchantGroupInput) {
    const { data } = await http.post<MerchantGroup>('/admin/groups', payload);
    return data;
  },
  async update(id: string, payload: MerchantGroupInput) {
    const { data } = await http.put<MerchantGroup>(`/admin/groups/${id}`, payload);
    return data;
  },
  async setStatus(id: string, status: string) {
    await http.patch(`/admin/groups/${id}/status`, { status });
  },
  /** 买断价单独一个接口，单位分 */
  async setPrice(id: string, priceCents: number) {
    await http.patch(`/admin/groups/${id}/price`, { price_cents: priceCents });
  },
  /** 把商户归到某个组；groupId 传 null 表示移出所有组 */
  async assignMerchant(merchantId: string, groupId: string | null) {
    const { data } = await http.patch<{
      merchant_id?: string;
      group_id?: string | null;
      settle_mode?: string;
    }>(`/admin/merchants/${merchantId}/group`, { group_id: groupId });
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/groups/${id}`);
  },
};
