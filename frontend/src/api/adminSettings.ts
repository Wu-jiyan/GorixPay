import http from './client';

/**
 * 系统设置项。凭据类键（oauth.*.app_secret、notify.smtp_password、notify.sms_token 等）
 * 后端不回显值，只返回 secret/configured；非凭据键返回 value。
 */
export interface AdminSettingItem {
  key: string;
  value?: unknown;
  secret?: boolean;
  configured?: boolean;
  updated_at?: string;
}

export const adminSettingsApi = {
  async list() {
    const { data } = await http.get<{ items: AdminSettingItem[] }>('/admin/settings');
    return data.items;
  },
  /** 凭据键传空串时后端保留原值（刻意设计），调用方应避免把「留空」当清空 */
  async update(key: string, value: unknown) {
    const { data } = await http.put<AdminSettingItem>(`/admin/settings/${encodeURIComponent(key)}`, { value });
    return data;
  },
};
