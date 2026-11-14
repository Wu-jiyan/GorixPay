import http from './client';

// 商户端第三方登录绑定。授权入口 /auth/oauth/:provider/authorize 是后端 302，前端直接跳转，不需要 API 函数。

export interface OAuthBinding {
  provider: string;
  nickname?: string | null;
  created_at?: string | null;
}

export const merchantOauthApi = {
  async bindings() {
    const { data } = await http.get<{ items: OAuthBinding[] }>('/merchant/oauth/bindings');
    return data.items;
  },
  async unbind(provider: string) {
    await http.delete(`/merchant/oauth/bindings/${encodeURIComponent(provider)}`);
  },
};
