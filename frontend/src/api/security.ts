import http from './client';
import type { Paginated, PageQuery } from './types';

export interface TotpStatus {
  enabled: boolean;
  enabled_at?: string | null;
}

export interface TotpSetup {
  secret: string;
  otpauth_url: string;
}

export interface LoginAttempt {
  id?: string;
  identifier?: string;
  client_ip?: string;
  success?: boolean;
  created_at?: string;
}

export interface LoginAttemptQuery extends PageQuery {
  identifier?: string;
  success?: boolean;
}

export const securityApi = {
  async totp() {
    const { data } = await http.get<TotpStatus>('/admin/security/totp');
    return data;
  },
  async setupTotp() {
    const { data } = await http.post<TotpSetup>('/admin/security/totp/setup');
    return data;
  },
  async enableTotp(code: string) {
    const { data } = await http.post<TotpStatus>('/admin/security/totp/enable', { code });
    return data;
  },
  async disableTotp(code: string) {
    const { data } = await http.post<TotpStatus>('/admin/security/totp/disable', { code });
    return data;
  },
  async loginAttempts(query: LoginAttemptQuery = {}) {
    const { data } = await http.get<Paginated<LoginAttempt> | LoginAttempt[]>(
      '/admin/security/login-attempts',
      { params: query },
    );
    return data;
  },
};
