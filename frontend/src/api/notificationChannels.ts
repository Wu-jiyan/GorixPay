import http from './client';
import type { Paginated, PageQuery } from './types';

export interface NotificationChannel {
  id: string;
  kind: string;
  provider: string;
  status?: string;
  /** 后端只回键名，不回具体值 */
  config_keys?: string[] | null;
  created_at?: string;
  updated_at?: string;
}

export interface NotificationChannelInput {
  kind: string;
  provider: string;
  /** 键值对；更新时值为空的键表示保持原值 */
  config: Record<string, string>;
  status?: string;
}

export interface NotificationTestResult {
  sent?: boolean;
  message?: string;
}

export const notificationChannelsApi = {
  async list(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<NotificationChannel> | NotificationChannel[]>(
      '/admin/notification-channels',
      { params: query },
    );
    return data;
  },
  async create(payload: NotificationChannelInput) {
    const { data } = await http.post<NotificationChannel>('/admin/notification-channels', payload);
    return data;
  },
  async update(id: string, payload: NotificationChannelInput) {
    const { data } = await http.put<NotificationChannel>(`/admin/notification-channels/${id}`, payload);
    return data;
  },
  async remove(id: string) {
    await http.delete(`/admin/notification-channels/${id}`);
  },
  async test(id: string, target: string) {
    const { data } = await http.post<NotificationTestResult>(`/admin/notification-channels/${id}/test`, { target });
    return data;
  },
};
