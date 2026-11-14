import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import { setPageTitle } from '@/stores/site';

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('@/pages/public/Landing.vue'),
    meta: { title: '' },
  },
  {
    path: '/register',
    component: () => import('@/pages/public/Register.vue'),
    meta: { title: '商户注册' },
  },
  {
    path: '/login',
    component: () => import('@/pages/public/Login.vue'),
    meta: { title: '商户登录' },
  },
  {
    path: '/forgot-password',
    component: () => import('@/pages/public/ForgotPassword.vue'),
    meta: { title: '找回密码' },
  },
  {
    path: '/qr-login',
    component: () => import('@/pages/public/QrLogin.vue'),
    meta: { title: '扫码登录确认' },
  },
  {
    path: '/admin/login',
    component: () => import('@/pages/public/AdminLogin.vue'),
    meta: { title: '运营登录' },
  },
  {
    path: '/doc',
    component: () => import('@/pages/public/Docs.vue'),
    meta: { title: '开发文档' },
  },
  {
    path: '/cashier.php',
    component: () => import('@/pages/cashier/Cashier.vue'),
    meta: { title: '收银台' },
  },
  {
    path: '/pay/:tradeNo',
    redirect: (to) => ({ path: '/cashier.php', query: { trade_no: String(to.params.tradeNo) } }),
  },
  {
    path: '/pay/sandbox/:tradeNo',
    component: () => import('@/pages/cashier/Sandbox.vue'),
    meta: { title: '沙箱支付' },
  },
  {
    path: '/merchant',
    component: () => import('@/layouts/MerchantLayout.vue'),
    meta: { requiresAuth: 'merchant' },
    children: [
      { path: '', component: () => import('@/pages/merchant/Dashboard.vue'), meta: { title: '商户概览' } },
      { path: 'orders', component: () => import('@/pages/merchant/Orders.vue'), meta: { title: '我的订单' } },
      { path: 'refunds', component: () => import('@/pages/merchant/Refunds.vue'), meta: { title: '退款记录' } },
      { path: 'complaints', component: () => import('@/pages/merchant/Complaints.vue'), meta: { title: '我的投诉' } },
      {
        path: 'settlements',
        component: () => import('@/pages/merchant/Settlements.vue'),
        meta: { title: '结算记录' },
      },
      { path: 'transfers', component: () => import('@/pages/merchant/Transfers.vue'), meta: { title: '代付申请' } },
      { path: 'records', component: () => import('@/pages/merchant/Records.vue'), meta: { title: '资金明细' } },
      { path: 'profit-sharing', component: () => import('@/pages/merchant/ProfitSharing.vue'), meta: { title: '分账管理' } },
      { path: 'red-packet', component: () => import('@/pages/merchant/RedPacket.vue'), meta: { title: '红包' } },
      {
        path: 'certification',
        component: () => import('@/pages/merchant/Certification.vue'),
        meta: { title: '实名认证' },
      },
      { path: 'domains', component: () => import('@/pages/merchant/Domains.vue'), meta: { title: '支付域名' } },
      { path: 'invite', component: () => import('@/pages/merchant/Invite.vue'), meta: { title: '邀请返佣' } },
      { path: 'recharge', component: () => import('@/pages/merchant/Recharge.vue'), meta: { title: '余额充值' } },
      { path: 'group-plans', component: () => import('@/pages/merchant/GroupPlans.vue'), meta: { title: '用户组' } },
      {
        path: 'credentials',
        component: () => import('@/pages/merchant/Credentials.vue'),
        meta: { title: '密钥管理' },
      },
      { path: 'profile', component: () => import('@/pages/merchant/Profile.vue'), meta: { title: '账户资料' } },
    ],
  },
  {
    path: '/admin',
    component: () => import('@/layouts/AdminLayout.vue'),
    meta: { requiresAuth: 'administrator' },
    children: [
      { path: '', component: () => import('@/pages/admin/Dashboard.vue'), meta: { title: '数据概览' } },
      { path: 'merchants', component: () => import('@/pages/admin/Merchants.vue'), meta: { title: '商户管理' } },
      { path: 'groups', component: () => import('@/pages/admin/Groups.vue'), meta: { title: '用户组与费率' } },
      { path: 'orders', component: () => import('@/pages/admin/Orders.vue'), meta: { title: '订单管理' } },
      { path: 'channels', component: () => import('@/pages/admin/Channels.vue'), meta: { title: '支付渠道' } },
      { path: 'test-payment', component: () => import('@/pages/admin/TestPayment.vue'), meta: { title: '测试支付' } },
      { path: 'devices', component: () => import('@/pages/admin/Devices.vue'), meta: { title: '语音设备与打印机' } },
      { path: 'group-purchases', component: () => import('@/pages/admin/GroupPurchases.vue'), meta: { title: '用户组购买' } },
      { path: 'pay-types', component: () => import('@/pages/admin/PayTypes.vue'), meta: { title: '支付方式' } },
      { path: 'pay-rolls', component: () => import('@/pages/admin/PayRolls.vue'), meta: { title: '按日轮询' } },
      { path: 'wechat-mp', component: () => import('@/pages/admin/WechatMp.vue'), meta: { title: '公众号配置' } },
      { path: 'refunds', component: () => import('@/pages/admin/Refunds.vue'), meta: { title: '退款管理' } },
      { path: 'complaints', component: () => import('@/pages/admin/Complaints.vue'), meta: { title: '投诉管理' } },
      {
        path: 'settlements',
        component: () => import('@/pages/admin/Settlements.vue'),
        meta: { title: '结算管理' },
      },
      { path: 'transfers', component: () => import('@/pages/admin/Transfers.vue'), meta: { title: '代付管理' } },
      { path: 'settle-rules', component: () => import('@/pages/admin/SettleRules.vue'), meta: { title: '结算规则' } },
      { path: 'profit-sharing', component: () => import('@/pages/admin/ProfitSharing.vue'), meta: { title: '分账' } },
      {
        path: 'certifications',
        component: () => import('@/pages/admin/Certifications.vue'),
        meta: { title: '实名审核' },
      },
      { path: 'domains', component: () => import('@/pages/admin/Domains.vue'), meta: { title: '域名审核' } },
      { path: 'risk', component: () => import('@/pages/admin/Risk.vue'), meta: { title: '风控与黑名单' } },
      { path: 'plugins', component: () => import('@/pages/admin/Plugins.vue'), meta: { title: '插件与渠道' } },
      { path: 'invites', component: () => import('@/pages/admin/Invites.vue'), meta: { title: '返佣记录' } },
      { path: 'ledger', component: () => import('@/pages/admin/Ledger.vue'), meta: { title: '资金账本' } },
      { path: 'outbox', component: () => import('@/pages/admin/Outbox.vue'), meta: { title: '消息队列' } },
      { path: 'notification-channels', component: () => import('@/pages/admin/NotificationChannels.vue'), meta: { title: '通知通道' } },
      {
        path: 'announcements',
        component: () => import('@/pages/admin/Announcements.vue'),
        meta: { title: '公告管理' },
      },
      { path: 'security', component: () => import('@/pages/admin/Security.vue'), meta: { title: '安全设置' } },
      { path: 'settings', component: () => import('@/pages/admin/Settings.vue'), meta: { title: '系统设置' } },
      { path: 'audit', component: () => import('@/pages/admin/Audit.vue'), meta: { title: '审计日志' } },
      { path: 'maintenance', component: () => import('@/pages/admin/Maintenance.vue'), meta: { title: '数据清理' } },
      { path: 'reports', component: () => import('@/pages/admin/Reports.vue'), meta: { title: '报表' } },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    component: () => import('@/pages/NotFound.vue'),
    meta: { title: '页面不存在' },
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
});

router.beforeEach((to) => {
  const auth = useAuthStore();
  const required = to.meta.requiresAuth as string | undefined;
  if (required && !auth.isLoggedIn) {
    return required === 'administrator' ? '/admin/login' : '/login';
  }
  if (required === 'administrator' && auth.principalType === 'merchant') {
    return '/merchant';
  }
  if (required === 'merchant' && auth.principalType === 'administrator') {
    return '/admin';
  }
  if (to.path === '/login' && auth.principalType === 'merchant' && auth.isLoggedIn) {
    return '/merchant';
  }
  if (to.path === '/admin/login' && auth.principalType === 'administrator' && auth.isLoggedIn) {
    return '/admin';
  }
  return true;
});

router.afterEach((to) => {
  const title = to.meta.title as string | undefined;
  setPageTitle(title ?? '');
});

export default router;
