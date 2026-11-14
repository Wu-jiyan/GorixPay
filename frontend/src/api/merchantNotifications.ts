import http from './client';

// 商户通知偏好：三个开关 + 通知渠道（mail / sms，短信需要绑定手机号）。

export interface NotificationPrefs {
  notify_payment: boolean;
  notify_settlement: boolean;
  notify_complaint: boolean;
  /** mail 或 sms */
  notify_channel: string;
}

export const merchantNotificationApi = {
  async getPrefs() {
    const { data } = await http.get<NotificationPrefs>('/merchant/notification-prefs');
    return data;
  },
  async savePrefs(payload: NotificationPrefs) {
    await http.put('/merchant/notification-prefs', payload);
  },
};
