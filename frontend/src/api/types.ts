export interface Paginated<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface Administrator {
  id: string;
  username: string;
  display_name: string;
}

export interface Merchant {
  id: string;
  legacy_uid?: number | null;
  email?: string | null;
  phone?: string | null;
  display_name: string;
  status: 'active' | 'disabled' | string;
  /** 结算方式，对应上游 pre_user.settle_id：0 未设置 / 1 支付宝 / 2 银行卡 */
  settle_type?: number;
  settle_account?: string | null;
  /** 所属用户组名，未归组时为空串（列表接口 GET /admin/merchants 额外返回） */
  group_name?: string | null;
  /** 结算模式：normal（普通结算）/ direct（商户直清）（列表接口额外返回） */
  settle_mode?: string | null;
  created_at: string;
  updated_at?: string;
}

export interface LoginResult {
  token: string;
  expires_at: string;
  merchant?: { id: string; display_name: string; legacy_uid?: number | null; status?: string };
  administrator?: Administrator;
}

export interface SessionInfo {
  principal_type: 'administrator' | 'merchant' | string;
  principal_id: string;
  session_id: string;
  expires_at: string;
}

export type OrderStatus = 'pending' | 'processing' | 'paid' | 'closed' | 'failed';

export interface Order {
  id: string;
  trade_no: string;
  out_trade_no: string;
  payment_type: string;
  name: string;
  amount: number;
  currency: string;
  status: OrderStatus;
  kind?: 'sale' | 'recharge' | 'deposit' | string;
  notify_url: string;
  return_url?: string | null;
  client_ip?: string | null;
  attach?: string | null;
  channel_trade_no?: string | null;
  paid_at?: string | null;
  /** 有值时表示该订单已入账金额被转入商户冻结余额（订单仍是已支付） */
  frozen_at?: string | null;
  expires_at: string;
  created_at: string;
  updated_at: string;
}

export interface OrderStatistics {
  total_orders?: number;
  total_amount?: number;
  paid_orders?: number;
  paid_amount?: number;
  pending_orders?: number;
  [key: string]: number | undefined;
}

export type FinanceStatus = 'pending' | 'approved' | 'rejected' | string;

/** 字段对齐 internal/finance/model/model.go 的 Refund json tag */
export interface Refund {
  id: string;
  refund_no: string;
  merchant_id: string;
  payment_order_id: string;
  out_refund_no: string;
  amount: number;
  currency: string;
  reason: string;
  status: FinanceStatus;
  ledger_transaction_id?: string | null;
  rejection_reason?: string | null;
  processed_by?: string | null;
  processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

/** 字段对齐 internal/finance/model/model.go 的 Settlement json tag */
export interface Settlement {
  id: string;
  settlement_no: string;
  merchant_id: string;
  out_settlement_no: string;
  amount: number;
  currency: string;
  destination: string;
  status: FinanceStatus;
  /** manual / auto，表示结算单是商户手提交还是规则自动生成 */
  source?: string;
  fee_cents?: number;
  /** none / pending / done / failed */
  transfer_status?: string;
  transfer_at?: string | null;
  ledger_transaction_id?: string | null;
  rejection_reason?: string | null;
  processed_by?: string | null;
  processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

/** 字段对齐 internal/transfer/http.go 的 transferResponse */
export interface Transfer {
  id: string;
  transfer_no: string;
  merchant_id: string;
  out_transfer_no: string;
  amount: number;
  currency: string;
  account_name: string;
  account_no: string;
  bank_name: string;
  branch_name?: string | null;
  status: FinanceStatus;
  /** bank / alipay / red 等 */
  kind?: string | null;
  batch_no?: string | null;
  remark?: string | null;
  ledger_transaction_id?: string | null;
  rejection_reason?: string | null;
  processed_by?: string | null;
  processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface Credential {
  id: string;
  credential_type: 'md5' | 'rsa' | string;
  key_id?: string | null;
  status: string;
  created_at: string;
  revoked_at?: string | null;
}

export interface V2Credential {
  key_id?: string | null;
  public_key?: string | null;
  created_at?: string | null;
  platform_public_key: string;
}

export interface Channel {
  id: string;
  name: string;
  plugin_code: string;
  payment_type: string;
  priority: number;
  weight: number;
  status: string;
  api_url?: string;
  pid?: string;
  created_at?: string;
  updated_at?: string;
}

export interface Announcement {
  id: string;
  title: string;
  content: string;
  status: string;
  published_at?: string | null;
  created_at: string;
  updated_at?: string;
}

export interface SiteInfo {
  name: string;
  logo_url: string;
  footer_text?: string;
  skin?: string;
  announcement?: Announcement | null;
  payment_types: string[];
  /** 平台是否要求登录图形验证码（security.captcha_required） */
  captcha_required?: boolean;
  /** 注册门禁：open / closed / invite */
  register_mode?: string;
  /** 首页显示模式：landing / login / url */
  homepage_mode?: string;
  homepage_url?: string;
  contact_email?: string;
  contact_qq?: string;
  icp_number?: string;
}

export interface AdminDashboard {
  merchant_count: number;
  order_count: number;
  paid_order_count: number;
  paid_amount: number;
}

export interface MerchantDashboard {
  today_order_count: number;
  today_paid_amount: number;
  total_paid_amount: number;
  available_balance: number;
  frozen_balance: number;
  pending_refunds: number;
  pending_settlements: number;
}

export interface DailyStat {
  date: string;
  order_count: number;
  paid_count: number;
  paid_amount: number;
}

/** GET /admin/stats/summary：internal/admin/httpapi/extras.go 的 summaryStat */
export interface AdminSummary {
  merchant_count: number;
  order_count: number;
  paid_order_count: number;
  paid_amount: number;
  today_paid_amount: number;
  pending_refunds: number;
  pending_settlements: number;
}

/**
 * GET /merchant/stats/daily：internal/reporting/stats.go 的 merchantDailyStat。
 * 注意商户版没有 paid_count，只有订单数与成交金额。
 */
export interface MerchantDailyStat {
  date: string;
  order_count: number;
  paid_amount: number;
}

export interface CashierOrder {
  trade_no: string;
  name: string;
  amount: number;
  currency: string;
  payment_type: string;
  status: OrderStatus;
  kind?: string;
  return_url?: string | null;
  created_at?: string;
  expires_at: string;
  pay_type?: string;
  pay_info?: string | Record<string, unknown>;
  /** 站内支付页（/pay/{func}/{trade_no}/），手机端交给它渲染二维码与复制链接 */
  pay_page?: string;
  channel?: { id: string; name: string; plugin_code: string };
}

export interface StartPaymentResult {
  pay_type: string;
  pay_info: string | Record<string, unknown>;
  channel?: { id: string; name: string; plugin_code: string };
  status: string;
}

export interface LedgerAccount {
  id: string;
  owner_type: string;
  owner_id: string;
  purpose: string;
  category: string;
  currency: string;
  status: string;
  available?: number;
  frozen?: number;
  created_at: string;
}

export interface PageQuery {
  page?: number;
  page_size?: number;
  status?: string;
  keyword?: string;
  created_from?: string;
  created_to?: string;
  payment_type?: string;
}

export interface Certification {
  status: 'none' | 'pending' | 'approved' | 'rejected' | string;
  cert_type?: 'person' | 'corp';
  real_name_masked?: string;
  id_no_masked?: string;
  corp_name?: string;
  corp_no?: string;
  fee_cents?: number;
  required?: boolean;
  submitted_at?: string | null;
  reviewed_at?: string | null;
  rejection_reason?: string | null;
}

export interface MerchantDomain {
  id: string;
  merchant_id?: string;
  domain: string;
  status: 'pending' | 'approved' | 'rejected' | string;
  rejection_reason?: string | null;
  created_at: string;
  reviewed_at?: string | null;
}

export interface InviteInfo {
  enabled: boolean;
  rate_bp: number;
  invite_code: string;
  invite_url: string;
  invited_count: number;
  today_income: number;
  total_income: number;
}

export interface InviteRecord {
  id: string;
  inviter_id: string;
  invitee_id: string;
  invitee_name?: string;
  order_id: string;
  trade_no: string;
  order_amount: number;
  rebate_amount: number;
  rate_bp: number;
  created_at: string;
}

export interface BlacklistEntry {
  id: string;
  entry_type: 'ip' | 'domain' | 'keyword' | 'email' | string;
  value: string;
  remark?: string | null;
  created_at: string;
}

export interface RiskEvent {
  id: string;
  merchant_id?: string | null;
  rule_code: string;
  detail: string;
  client_ip?: string | null;
  created_at: string;
}

export interface DepositInfo {
  deposit_balance: number;
  required_cents: number;
  enabled: boolean;
}

/** GET /merchant/printers：internal/merchant/httpapi/printers.go 的列表项（不回 secret） */
export interface MerchantPrinter {
  id: string;
  name: string;
  status: string;
  created_at: string;
  updated_at?: string;
}

/** 打印任务：internal/device/model.go 的 PrintJob */
export interface MerchantPrintJob {
  id: string;
  printer_id: string;
  order_id?: string | null;
  content: string;
  status: string;
  attempts?: number;
}

export interface PluginConfigField {
  key: string;
  label: string;
  type: string;
  required: boolean;
  secret: boolean;
}

export interface PluginMeta {
  code: string;
  name: string;
  capabilities: string[];
  config_fields: PluginConfigField[];
}
