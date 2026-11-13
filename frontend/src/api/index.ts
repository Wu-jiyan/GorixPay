import http from './client';
import type {
  AdminDashboard,
  Announcement,
  BlacklistEntry,
  CashierOrder,
  Certification,
  Channel,
  Credential,
  DailyStat,
  DepositInfo,
  InviteInfo,
  InviteRecord,
  LedgerAccount,
  LoginResult,
  Merchant,
  MerchantDashboard,
  MerchantDomain,
  Order,
  OrderStatistics,
  PageQuery,
  Paginated,
  PluginMeta,
  Refund,
  RiskEvent,
  SessionInfo,
  Settlement,
  SiteInfo,
  StartPaymentResult,
  Transfer,
  V2Credential,
} from './types';

const clean = (params: object) => {
  const output: Record<string, unknown> = {};
  Object.entries(params as Record<string, unknown>).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') output[key] = value;
  });
  return output;
};

export const authApi = {
  async loginMerchant(identifier: string, password: string) {
    const { data } = await http.post<LoginResult>('/auth/merchant/login', { identifier, password });
    return data;
  },
  async loginAdministrator(identifier: string, password: string) {
    const { data } = await http.post<LoginResult>('/auth/administrator/login', { identifier, password });
    return data;
  },
  async register(identifier: string, password: string, invite?: string) {
    const { data } = await http.post<LoginResult>('/auth/register', { identifier, password, invite });
    return data;
  },
  async me() {
    const { data } = await http.get<SessionInfo>('/auth/me');
    return data;
  },
  async logout() {
    await http.post('/auth/logout');
  },
};

export const publicApi = {
  async site() {
    const { data } = await http.get<SiteInfo>('/public/site');
    return data;
  },
  async announcements() {
    const { data } = await http.get<{ items: Announcement[] }>('/public/announcements');
    return data.items;
  },
};

export const cashierApi = {
  async order(tradeNo: string) {
    const { data } = await http.get<CashierOrder>(`/cashier/orders/${encodeURIComponent(tradeNo)}`);
    return data;
  },
  async start(tradeNo: string) {
    const { data } = await http.post<StartPaymentResult>(`/cashier/orders/${encodeURIComponent(tradeNo)}/start`);
    return data;
  },
  async status(tradeNo: string) {
    const { data } = await http.get<{ status: string; paid_at?: string | null }>(
      `/cashier/orders/${encodeURIComponent(tradeNo)}/status`,
    );
    return data;
  },
  async sandboxComplete(tradeNo: string) {
    const { data } = await http.post<{ status: string }>(`/sandbox/orders/${encodeURIComponent(tradeNo)}/complete`);
    return data;
  },
};

export const merchantApi = {
  async profile() {
    const { data } = await http.get<Merchant>('/merchant/profile');
    return data;
  },
  async updateProfile(payload: { display_name: string; settle_type?: number; settle_account?: string }) {
    const { data } = await http.put<Merchant>('/merchant/profile', payload);
    return data;
  },
  async dashboard() {
    const { data } = await http.get<MerchantDashboard>('/merchant/dashboard');
    return data;
  },
  async orders(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Order>>('/merchant/orders', { params: clean(query) });
    return data;
  },
  async order(id: string) {
    const { data } = await http.get<Order>(`/merchant/orders/${id}`);
    return data;
  },
  async orderStatistics(query: PageQuery = {}) {
    const { data } = await http.get<OrderStatistics>('/merchant/orders/statistics', { params: clean(query) });
    return data;
  },
  async refunds(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Refund>>('/merchant/refunds', { params: clean(query) });
    return data;
  },
  async createRefund(payload: {
    payment_order_id: string;
    out_refund_no: string;
    amount: number;
    reason: string;
  }) {
    const { data } = await http.post<Refund>('/merchant/refunds', payload);
    return data;
  },
  async settlements(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Settlement>>('/merchant/settlements', { params: clean(query) });
    return data;
  },
  async createSettlement(payload: {
    out_settlement_no: string;
    amount: number;
    currency: string;
    destination: string;
  }) {
    const { data } = await http.post<Settlement>('/merchant/settlements', payload);
    return data;
  },
  async transfers(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Transfer>>('/merchant/transfers', { params: clean(query) });
    return data;
  },
  async createTransfer(payload: {
    out_transfer_no: string;
    amount: number;
    account_name: string;
    account_no: string;
    bank_name: string;
    branch_name?: string;
  }) {
    const { data } = await http.post<Transfer>('/merchant/transfers', payload);
    return data;
  },
  async transfer(id: string) {
    const { data } = await http.get<Transfer>(`/merchant/transfers/${id}`);
    return data;
  },
  async credentials() {
    const { data } = await http.get<{ items: Credential[] }>('/merchant/credentials');
    return data.items;
  },
  async rotateMD5() {
    const { data } = await http.post<Credential & { secret: string }>('/merchant/credentials/md5/rotate');
    return data;
  },
  async v2Credential() {
    const { data } = await http.get<V2Credential>('/merchant/credentials/v2');
    return data;
  },
  async rotateV2() {
    const { data } = await http.post<V2Credential & { private_key: string }>('/merchant/credentials/v2/rotate');
    return data;
  },
  async announcements() {
    const { data } = await http.get<{ items: Announcement[] }>('/merchant/announcements');
    return data.items;
  },
  async certification() {
    const { data } = await http.get<Certification>('/merchant/certification');
    return data;
  },
  async submitCertification(payload: {
    cert_type: 'person' | 'corp';
    real_name: string;
    id_no: string;
    corp_name?: string;
    corp_no?: string;
  }) {
    const { data } = await http.post<Certification>('/merchant/certification', payload);
    return data;
  },
  async domains() {
    const { data } = await http.get<{ items: MerchantDomain[] }>('/merchant/domains');
    return data.items;
  },
  async addDomain(domain: string) {
    const { data } = await http.post<MerchantDomain>('/merchant/domains', { domain });
    return data;
  },
  async deleteDomain(id: string) {
    await http.delete(`/merchant/domains/${id}`);
  },
  async invite() {
    const { data } = await http.get<InviteInfo>('/merchant/invite');
    return data;
  },
  async inviteRecords(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<InviteRecord>>('/merchant/invite/records', { params: clean(query) });
    return data;
  },
  async recharge(amount: number) {
    const { data } = await http.post<{ trade_no: string; amount: number; cashier_url: string }>(
      '/merchant/recharge',
      { amount },
    );
    return data;
  },
  async rechargeOrders(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Order>>('/merchant/recharge', { params: clean(query) });
    return data;
  },
  async deposit() {
    const { data } = await http.get<DepositInfo>('/merchant/deposit');
    return data;
  },
  async rechargeDeposit(amount: number) {
    const { data } = await http.post<{ trade_no: string; amount: number; cashier_url: string }>(
      '/merchant/deposit',
      { amount },
    );
    return data;
  },
  async depositOrders(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Order>>('/merchant/deposit/orders', { params: clean(query) });
    return data;
  },
  async riskEvents(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<RiskEvent>>('/merchant/risk-events', { params: clean(query) });
    return data;
  },
};

export const adminApi = {
  async dashboard() {
    const { data } = await http.get<AdminDashboard>('/admin/dashboard');
    return data;
  },
  async dailyStats(days = 14) {
    const { data } = await http.get<{ items: DailyStat[] }>('/admin/stats/daily', { params: { days } });
    return data.items;
  },
  async merchants(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Merchant>>('/admin/merchants', { params: clean(query) });
    return data;
  },
  async merchant(id: string) {
    const { data } = await http.get<Merchant & { credentials?: Credential[]; accounts?: LedgerAccount[] }>(
      `/admin/merchants/${id}`,
    );
    return data;
  },
  async setMerchantStatus(id: string, status: string) {
    await http.patch(`/admin/merchants/${id}/status`, { status });
  },
  async orders(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Order>>('/admin/orders', { params: clean(query) });
    return data;
  },
  async order(id: string) {
    const { data } = await http.get<Order>(`/admin/orders/${id}`);
    return data;
  },
  async closeOrder(id: string) {
    await http.post(`/admin/orders/${id}/close`);
  },
  async channels() {
    const { data } = await http.get<{ items: Channel[] }>('/admin/channels');
    return data.items;
  },
  async createChannel(payload: {
    name: string;
    plugin_code: string;
    payment_type: string;
    priority: number;
    weight: number;
    status: string;
    api_url: string;
    pid: string;
    key: string;
    config?: Record<string, string>;
  }) {
    const { data } = await http.post<{ id: string }>('/admin/channels', payload);
    return data;
  },
  async updateChannel(
    id: string,
    payload: {
      name: string;
      plugin_code: string;
      payment_type: string;
      priority: number;
      weight: number;
      status: string;
      api_url: string;
      pid: string;
      key: string;
      config?: Record<string, string>;
    },
  ) {
    await http.put(`/admin/channels/${id}`, payload);
  },
  async setChannelStatus(id: string, status: string) {
    await http.patch(`/admin/channels/${id}/status`, { status });
  },
  async refunds(query: PageQuery & { merchant_id?: string } = {}) {
    const { data } = await http.get<Paginated<Refund>>('/admin/refunds', { params: clean(query) });
    return data;
  },
  async processRefund(id: string, action: 'approve' | 'reject', rejectionReason = '') {
    const { data } = await http.post<Refund>(`/admin/refunds/${id}/process`, {
      action,
      rejection_reason: rejectionReason,
    });
    return data;
  },
  async settlements(query: PageQuery & { merchant_id?: string } = {}) {
    const { data } = await http.get<Paginated<Settlement>>('/admin/settlements', { params: clean(query) });
    return data;
  },
  async processSettlement(id: string, action: 'approve' | 'reject', rejectionReason = '') {
    const { data } = await http.post<Settlement>(`/admin/settlements/${id}/process`, {
      action,
      rejection_reason: rejectionReason,
    });
    return data;
  },
  async transfers(query: PageQuery & { merchant_id?: string } = {}) {
    const { data } = await http.get<Paginated<Transfer>>('/admin/transfers', { params: clean(query) });
    return data;
  },
  async processTransfer(id: string, action: 'approve' | 'reject', rejectionReason = '') {
    const { data } = await http.post<Transfer>(`/admin/transfers/${id}/process`, {
      action,
      rejection_reason: rejectionReason,
    });
    return data;
  },
  async announcements() {
    const { data } = await http.get<{ items: Announcement[] }>('/admin/announcements');
    return data.items;
  },
  async createAnnouncement(payload: { title: string; content: string; status: string }) {
    const { data } = await http.post<Announcement>('/admin/announcements', payload);
    return data;
  },
  async updateAnnouncement(id: string, payload: { title: string; content: string; status: string }) {
    const { data } = await http.put<Announcement>(`/admin/announcements/${id}`, payload);
    return data;
  },
  async deleteAnnouncement(id: string) {
    await http.delete(`/admin/announcements/${id}`);
  },
  async settings() {
    const { data } = await http.get<{ items: { key: string; value: unknown; updated_at: string }[] }>('/admin/settings');
    return data.items;
  },
  async updateSetting(key: string, value: unknown) {
    await http.put(`/admin/settings/${encodeURIComponent(key)}`, { value });
  },
  async auditEvents(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<{ id: string; event_type: string; actor_type: string; actor_id?: string; payload: string; created_at: string }>>(
      '/admin/audit-events',
      { params: clean(query) },
    );
    return data;
  },
  async ledgerAccounts(query: PageQuery & { owner_type?: string; owner_id?: string; purpose?: string } = {}) {
    const { data } = await http.get<Paginated<LedgerAccount>>('/admin/ledger/accounts', { params: clean(query) });
    return data;
  },
  async ledgerTransactions(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Record<string, unknown>>>('/admin/ledger/transactions', {
      params: clean(query),
    });
    return data;
  },
  async ledgerOperations(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Record<string, unknown>>>('/admin/ledger/operations', {
      params: clean(query),
    });
    return data;
  },
  async outboxMessages(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Record<string, unknown>>>('/admin/outbox/messages', {
      params: clean(query),
    });
    return data;
  },
  async outboxStats() {
    const { data } = await http.get<Record<string, number>>('/admin/outbox/stats');
    return data;
  },
  async requeueDead() {
    const { data } = await http.post<{ requeued: number }>('/admin/outbox/requeue-dead');
    return data;
  },
  async certifications(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Merchant & { certification?: Certification }>>(
      '/admin/certifications',
      { params: clean(query) },
    );
    return data;
  },
  async reviewCertification(merchantId: string, action: 'approve' | 'reject', reason = '') {
    const { data } = await http.post<Certification>(`/admin/certifications/${merchantId}/review`, {
      action,
      reason,
    });
    return data;
  },
  async domains(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<MerchantDomain>>('/admin/domains', { params: clean(query) });
    return data;
  },
  async reviewDomain(id: string, action: 'approve' | 'reject', reason = '') {
    const { data } = await http.post<MerchantDomain>(`/admin/domains/${id}/review`, { action, reason });
    return data;
  },
  async blacklist(query: PageQuery & { entry_type?: string } = {}) {
    const { data } = await http.get<{ items: BlacklistEntry[] } | BlacklistEntry[]>('/admin/blacklist', {
      params: clean(query),
    });
    return Array.isArray(data) ? data : data.items;
  },
  async createBlacklist(payload: { entry_type: string; value: string; remark?: string }) {
    const { data } = await http.post<BlacklistEntry>('/admin/blacklist', payload);
    return data;
  },
  async deleteBlacklist(id: string) {
    await http.delete(`/admin/blacklist/${id}`);
  },
  async riskEvents(query: PageQuery & { merchant_id?: string; rule_code?: string } = {}) {
    const { data } = await http.get<Paginated<RiskEvent>>('/admin/risk-events', { params: clean(query) });
    return data;
  },
  async invites(query: PageQuery & { inviter_id?: string } = {}) {
    const { data } = await http.get<Paginated<InviteRecord>>('/admin/invites', { params: clean(query) });
    return data;
  },
  async plugins() {
    const { data } = await http.get<{ items: PluginMeta[] } | PluginMeta[]>('/admin/plugins');
    return Array.isArray(data) ? data : data.items;
  },
};

export * from './types';
