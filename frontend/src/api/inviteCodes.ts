import http from './client';

export interface InviteCode {
  id: string;
  code: string;
  owner_merchant_id?: string | null;
  /** 0 表示不限次 */
  max_uses: number;
  used_count?: number;
  expires_at?: string | null;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface InviteCodeInput {
  max_uses: number;
  owner_merchant_id?: string | null;
  expires_at?: string | null;
}

export const inviteCodesApi = {
  async list() {
    const { data } = await http.get<{ items: InviteCode[] } | InviteCode[]>('/admin/invite-codes');
    return data;
  },
  async create(payload: InviteCodeInput) {
    const { data } = await http.post<InviteCode>('/admin/invite-codes', payload);
    return data;
  },
  async update(id: string, payload: InviteCodeInput) {
    const { data } = await http.put<InviteCode>(`/admin/invite-codes/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/invite-codes/${id}`);
  },
};
