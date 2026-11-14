import http from './client';

// 商户端用户组购买（M1 用户组 + 费率）。

export interface GroupPlan {
  group_id: string;
  name: string;
  /** 费率，万分比 */
  rate_bp: number;
  pay_types?: string[];
  /** 购买价格（分），0 表示免费 */
  price_cents: number;
}

export const merchantGroupPlanApi = {
  async plans() {
    const { data } = await http.get<{ items: GroupPlan[] }>('/merchant/group-plans');
    return data.items;
  },
  async purchase(groupId: string) {
    const { data } = await http.post<Record<string, unknown>>('/merchant/group-purchases', { group_id: groupId });
    return data;
  },
};
