import http from './client';

export interface ComplaintAutoRule {
  id: string;
  channel: string;
  reply_text: string;
  close_after_hours: number;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ComplaintAutoRuleInput {
  channel: string;
  reply_text: string;
  close_after_hours: number;
  status?: string;
}

export const complaintAutoRulesApi = {
  async list() {
    const { data } = await http.get<{ items: ComplaintAutoRule[] } | ComplaintAutoRule[]>(
      '/admin/complaint-auto-rules',
    );
    return data;
  },
  async create(payload: ComplaintAutoRuleInput) {
    const { data } = await http.post<ComplaintAutoRule>('/admin/complaint-auto-rules', payload);
    return data;
  },
  async update(id: string, payload: ComplaintAutoRuleInput) {
    const { data } = await http.put<ComplaintAutoRule>(`/admin/complaint-auto-rules/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/complaint-auto-rules/${id}`);
  },
};
