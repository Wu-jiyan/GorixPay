import http from './client';
import type { Paginated, PageQuery } from './types';

export interface PayType {
  id: string;
  code: string;
  name: string;
  icon?: string | null;
  sort?: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface PayTypeInput {
  code: string;
  name: string;
  icon: string;
  sort: number;
  status: string;
}

export interface PayRoll {
  id: string;
  channel_id: string;
  roll_date: string;
  weight: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface PayRollInput {
  channel_id: string;
  roll_date: string;
  weight: number;
  status: string;
}

export interface PayRollGenerateInput {
  channel_ids: string[];
  from: string;
  to: string;
}

export interface PayRollGenerateResult {
  generated?: number;
  count?: number;
  created?: number;
}

export const payTypesApi = {
  /** 一次取全，后端最多返回 500 条 */
  async list() {
    const { data } = await http.get<Paginated<PayType> | PayType[]>('/admin/pay-types');
    return data;
  },
  async create(payload: PayTypeInput) {
    const { data } = await http.post<PayType>('/admin/pay-types', payload);
    return data;
  },
  async update(id: string, payload: PayTypeInput) {
    const { data } = await http.put<PayType>(`/admin/pay-types/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/pay-types/${id}`);
  },
  async setStatus(id: string, status: string) {
    await http.patch(`/admin/pay-types/${id}/status`, { status });
  },
};

export const payRollsApi = {
  async list(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<PayRoll> | PayRoll[]>('/admin/pay-rolls', { params: query });
    return data;
  },
  async create(payload: PayRollInput) {
    const { data } = await http.post<PayRoll>('/admin/pay-rolls', payload);
    return data;
  },
  async update(id: string, payload: PayRollInput) {
    const { data } = await http.put<PayRoll>(`/admin/pay-rolls/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/pay-rolls/${id}`);
  },
  async generate(payload: PayRollGenerateInput) {
    const { data } = await http.post<PayRollGenerateResult>('/admin/pay-rolls/generate', payload);
    return data;
  },
};

export interface WechatMpConfig {
  id: string;
  merchant_id?: string | null;
  app_id: string;
  token?: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

/** app_secret / aes_key 只写，后端不回显 */
export interface WechatMpInput {
  merchant_id?: string | null;
  app_id: string;
  app_secret?: string;
  token?: string;
  aes_key?: string;
  status?: string;
}

export const wechatMpApi = {
  async list(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<WechatMpConfig> | WechatMpConfig[]>('/admin/wechat-mp-configs', {
      params: query,
    });
    return data;
  },
  async create(payload: WechatMpInput) {
    const { data } = await http.post<WechatMpConfig>('/admin/wechat-mp-configs', payload);
    return data;
  },
  async update(id: string, payload: WechatMpInput) {
    const { data } = await http.put<WechatMpConfig>(`/admin/wechat-mp-configs/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/wechat-mp-configs/${id}`);
  },
};
