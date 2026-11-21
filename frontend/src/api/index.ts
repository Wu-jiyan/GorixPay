import http from './client';
import type {
  AdminDashboard,
  AdminSummary,
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
  MerchantDailyStat,
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
  /**
   * 补发异步通知：POST /merchant/orders/:id/notify。
   * 回 {requeued}，0 表示这条订单的通知商户都已经确认过，没有可重发的。
   */
  async resendOrderNotify(id: string) {
    const { data } = await http.post<{ requeued: number }>(`/merchant/orders/${encodeURIComponent(id)}/notify`);
    return data;
  },
  async refunds(query: PageQuery = {}) {
    const { data } = await http.get<Paginated<Refund>>('/merchant/refunds', { params: clean(query) });
    return data;
  },
  /** 退款单详情：GET /merchant/refunds/:id */
  async refund(id: string) {
    const { data } = await http.get<Refund>(`/merchant/refunds/${encodeURIComponent(id)}`);
    return data;
  },
  /** 商户按日统计：GET /merchant/stats/daily，后端固定回 {items, days} */
  async dailyStats(days = 7) {
    const { data } = await http.get<{ items: MerchantDailyStat[]; days?: number }>('/merchant/stats/daily', {
      params: { days },
    });
    return data.items;
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
  /** 结算单详情：GET /merchant/settlements/:id */
  async settlement(id: string) {
    const { data } = await http.get<Settlement>(`/merchant/settlements/${encodeURIComponent(id)}`);
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
  /** 汇总卡片：GET /admin/stats/summary（internal/admin/httpapi/extras.go 的 summaryStat） */
  async statsSummary() {
    const { data } = await http.get<AdminSummary>('/admin/stats/summary');
    return data;
  },
  async merchants(query: PageQuery & { settle_mode?: string } = {}) {
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
  /**
   * 订单列表：GET /admin/orders。
   * frozen=true 只看已冻结、false 只看未冻结、不传=全部（非法值后端 400）。
   */
  async orders(query: PageQuery & { frozen?: 'true' | 'false' } = {}) {
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
  /** 批量关单：POST /admin/orders/batch-close，只关 pending/processing，一次最多 200 笔 */
  async batchCloseOrders(ids: string[]) {
    const { data } = await http.post<{ closed: number; requested: number }>('/admin/orders/batch-close', { ids });
    return data;
  },
  /**
   * 手动补单：POST /admin/orders/:id/complete。
   * 走的是和渠道回调完全相同的完成路径（账本 + 钩子 + 通知）；
   * applied=false 表示这笔订单此前已经完成，没有重复记账。
   */
  async completeOrder(id: string, channelTradeNo = '') {
    const { data } = await http.post<{ order: Order; applied: boolean }>(
      `/admin/orders/${encodeURIComponent(id)}/complete`,
      { channel_trade_no: channelTradeNo },
    );
    return data;
  },
  /** 补发异步通知：POST /admin/orders/:id/notify，回 {requeued} */
  async resendOrderNotify(id: string) {
    const { data } = await http.post<{ requeued: number }>(`/admin/orders/${encodeURIComponent(id)}/notify`);
    return data;
  },
  /**
   * 冻结订单：POST /admin/orders/:id/freeze。
   * 把该订单已入账的金额从商户可用余额挪到冻结余额，订单仍是已支付（争议订单用）。
   * 返回订单对象（含 frozen_at）；非已支付订单回 409 order_not_freezable。
   */
  async freezeOrder(id: string) {
    const { data } = await http.post<Order>(`/admin/orders/${encodeURIComponent(id)}/freeze`);
    return data;
  },
  /** 解冻订单：POST /admin/orders/:id/unfreeze，金额退回可用余额，frozen_at 清空 */
  async unfreezeOrder(id: string) {
    const { data } = await http.post<Order>(`/admin/orders/${encodeURIComponent(id)}/unfreeze`);
    return data;
  },
  // 退款：先在账务里落一条退款单，execute=true 时再让捕获这笔钱的那条通道退回去。
  async refundOrder(
    id: string,
    payload: { amount?: number; reason?: string; execute?: boolean },
  ) {
    const { data } = await http.post<{
      refund: Record<string, unknown>;
      channel: string;
      message?: string;
      channel_refund_no?: string;
    }>(`/admin/orders/${id}/refund`, payload);
    return data;
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
  /** 退款单详情：GET /admin/refunds/:id */
  async refund(id: string) {
    const { data } = await http.get<Refund>(`/admin/refunds/${encodeURIComponent(id)}`);
    return data;
  },
  async settlements(query: PageQuery & { merchant_id?: string } = {}) {
    const { data } = await http.get<Paginated<Settlement>>('/admin/settlements', { params: clean(query) });
    return data;
  },
  /** 结算单详情：GET /admin/settlements/:id */
  async settlement(id: string) {
    const { data } = await http.get<Settlement>(`/admin/settlements/${encodeURIComponent(id)}`);
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
  /** 代付单详情：GET /admin/transfers/:id */
  async transfer(id: string) {
    const { data } = await http.get<Transfer>(`/admin/transfers/${encodeURIComponent(id)}`);
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
