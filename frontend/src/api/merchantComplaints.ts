import http from './client';
import type { Paginated } from './types';

// 商户投诉（M-投诉）：只读，回复入口在运营后台，前端不提供发送消息的接口。

export interface ComplaintMessage {
  id?: string;
  sender?: string | null;
  role?: string | null;
  content?: string | null;
  created_at?: string | null;
}

export interface Complaint {
  id: string;
  channel?: string | null;
  complaint_no?: string | null;
  status?: string | null;
  reason?: string | null;
  amount_cents?: number | null;
  deadline_at?: string | null;
  created_at: string;
}

export interface ComplaintDetail extends Complaint {
  messages?: ComplaintMessage[];
}

export interface ComplaintQuery {
  page?: number;
  page_size?: number;
}

const clean = (params: object) => {
  const output: Record<string, unknown> = {};
  Object.entries(params as Record<string, unknown>).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') output[key] = value;
  });
  return output;
};

export const complaintApi = {
  async list(query: ComplaintQuery = {}) {
    const { data } = await http.get<Paginated<Complaint>>('/merchant/complaints', { params: clean(query) });
    return data;
  },
  async detail(id: string) {
    const { data } = await http.get<ComplaintDetail>(`/merchant/complaints/${encodeURIComponent(id)}`);
    return data;
  },
};
