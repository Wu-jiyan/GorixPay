import http from './client';

// 商户通知偏好：三个场景开关 + 一个通知渠道。
// 渠道取值对齐后端 internal/notifyprefs/service.go 的常量：
//   mail（邮件）、sms（短信）、wechat（公众号模板消息）。
// 选了短信但账号没绑手机号时，后端会降级成邮件发送，不会报错。

export interface NotificationPrefs {
  /** 支付成功通知 */
  notify_payment: boolean;
  /** 结算结果通知 */
  notify_settlement: boolean;
  /** 投诉进展通知 */
  notify_complaint: boolean;
  /** mail / sms / wechat */
  notify_channel: string;
  /** 后端回包里的最后保存时间，提交时不回传 */
  updated_at?: string;
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