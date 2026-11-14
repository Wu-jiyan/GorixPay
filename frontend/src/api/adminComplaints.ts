import http from './client';
import type { Paginated, PageQuery } from './types';

// 运营端投诉接口。注意：`api/complaints.ts` 是商户端的只读接口（complaintApi），
// 两个入口字段不同，所以运营端单独放一份，避免互相覆盖。

export interface ComplaintMessage {
  id?: string;
  direction?: string;
  content?: string;
  operator?: string;
  created_at?: string;
}

export interface Complaint {
  id: string;
  merchant_id: string;
  order_id?: string | null;
  channel?: string;
  complaint_no?: string;
  status?: string;
  reason?: string;
  amount_cents?: number | null;
  deadline_at?: string | null;
  created_at?: string;
  updated_at?: string;
  messages?: ComplaintMessage[];
}

export interface ComplaintQuery extends PageQuery {
  channel?: string;
  merchant_id?: string;
}

export interface ComplaintInput {
  merchant_id: string;
  order_id?: string;
  channel: string;
  complaint_no: string;
  reason: string;
  amount_cents?: number;
  deadline_at?: string;
}

export const complaintsApi = {
  async list(query: ComplaintQuery = {}) {
    const { data } = await http.get<Paginated<Complaint> | Complaint[]>('/admin/complaints', { params: query });
    return data;
  },
  async detail(id: string) {
    const { data } = await http.get<Complaint>(`/admin/complaints/${id}`);
    return data;
  },
  async create(payload: ComplaintInput) {
    const { data } = await http.post<Complaint>('/admin/complaints', payload);
    return data;
  },
  async reply(id: string, content: string) {
    const { data } = await http.post<Complaint>(`/admin/complaints/${id}/messages`, { content });
    return data;
  },
  async close(id: string, reason: string) {
    const { data } = await http.post<Complaint>(`/admin/complaints/${id}/close`, { reason });
    return data;
  },
};
