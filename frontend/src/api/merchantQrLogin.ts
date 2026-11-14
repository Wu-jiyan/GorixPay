import http from './client';

// 扫码登录的手机端确认：必须是已登录的商户会话。

export const merchantQrApi = {
  async confirm(ticket: string) {
    await http.post('/merchant/qr/confirm', { ticket });
  },
};
