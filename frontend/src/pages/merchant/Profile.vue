<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useMessage, type FormInst, type FormRules, type SelectOption } from 'naive-ui';
import {
  BusinessOutline,
  LockClosedOutline,
  PersonCircleOutline,
  RefreshOutline,
  SaveOutline,
  ShieldCheckmarkOutline,
} from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import { accountSecurityApi } from '@/api/accountSecurity';
import { merchantOauthApi, type OAuthBinding } from '@/api/merchantOauth';
import { merchantNotificationApi, type NotificationPrefs } from '@/api/merchantNotifications';
import type { Merchant } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { activeStatus, formatDateTime } from '@/utils/format';
import { useAuthStore } from '@/stores/auth';
import { useNarrow } from './shared';

const message = useMessage();
const auth = useAuthStore();
const isNarrow = useNarrow();

const loading = ref(true);
const saving = ref(false);
const profile = ref<Merchant | null>(null);
const formRef = ref<FormInst | null>(null);
const form = reactive({
  display_name: '',
  settle_type: 0,
  settle_account: '',
});

const settleOptions: SelectOption[] = [
  { label: '未设置', value: 0 },
  { label: '支付宝', value: 1 },
  { label: '银行卡', value: 2 },
];

function settleLabel(value?: number) {
  const option = settleOptions.find((item) => item.value === (value ?? 0));
  return option ? option.label : '未设置';
}

const rules: FormRules = {
  display_name: [
    { required: true, message: '请输入显示名称', trigger: ['blur', 'input'] },
    {
      validator: (_rule, value: string) => {
        if (!value || value.length <= 40) return true;
        return new Error('显示名称不能超过 40 个字符');
      },
      trigger: ['blur', 'input'],
    },
  ],
};

async function loadProfile() {
  loading.value = true;
  try {
    const data = await merchantApi.profile();
    profile.value = data;
    form.display_name = data.display_name;
    form.settle_type = data.settle_type ?? 0;
    form.settle_account = data.settle_account ?? '';
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function handleSave() {
  if (saving.value) return;
  saving.value = true;
  try {
    await formRef.value?.validate();
  } catch {
    saving.value = false;
    return;
  }
  try {
    const updated = await merchantApi.updateProfile({
      display_name: form.display_name.trim(),
      settle_type: form.settle_type,
      settle_account: form.settle_account.trim(),
    });
    profile.value = updated;
    form.display_name = updated.display_name;
    form.settle_type = updated.settle_type ?? 0;
    form.settle_account = updated.settle_account ?? '';
    if (auth.merchant) {
      auth.merchant.display_name = updated.display_name;
      auth.persist();
    }
    message.success('资料已更新');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    saving.value = false;
  }
}

// 修改密码
const pwdFormRef = ref<FormInst | null>(null);
const pwdSaving = ref(false);
const pwdForm = reactive({ old_password: '', new_password: '', confirm: '' });

const pwdRules: FormRules = {
  old_password: [{ required: true, message: '请输入当前密码', trigger: ['blur', 'input'] }],
  new_password: [
    { required: true, message: '请输入新密码', trigger: ['blur', 'input'] },
    {
      validator: (_rule, value: string) => {
        if (!value || value.length >= 6) return true;
        return new Error('新密码至少 6 位');
      },
      trigger: ['blur', 'input'],
    },
  ],
  confirm: [
    { required: true, message: '请再次输入新密码', trigger: ['blur', 'input'] },
    {
      validator: (_rule, value: string) => {
        if (!value || value === pwdForm.new_password) return true;
        return new Error('两次输入的新密码不一致');
      },
      trigger: ['blur', 'input'],
    },
  ],
};

async function handleChangePassword() {
  if (pwdSaving.value) return;
  pwdSaving.value = true;
  try {
    await pwdFormRef.value?.validate();
  } catch {
    pwdSaving.value = false;
    return;
  }
  if (pwdForm.new_password !== pwdForm.confirm) {
    message.error('两次输入的新密码不一致');
    pwdSaving.value = false;
    return;
  }
  try {
    await accountSecurityApi.changePassword({
      old_password: pwdForm.old_password,
      new_password: pwdForm.new_password,
    });
    message.success('密码已修改，下次登录请使用新密码');
    pwdForm.old_password = '';
    pwdForm.new_password = '';
    pwdForm.confirm = '';
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    pwdSaving.value = false;
  }
}

// 第三方账号绑定
const PROVIDER_LABELS: Record<string, string> = {
  wechat: '微信',
  qq: 'QQ',
  douyin: '抖音',
  weibo: '微博',
  github: 'GitHub',
};

const oauthLoading = ref(true);
const bindings = ref<OAuthBinding[]>([]);
const unbinding = ref('');

function providerText(provider?: string): string {
  return PROVIDER_LABELS[provider ?? ''] ?? (provider || '-');
}

async function loadBindings() {
  oauthLoading.value = true;
  try {
    bindings.value = await merchantOauthApi.bindings();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    oauthLoading.value = false;
  }
}

async function handleUnbind(provider: string) {
  if (unbinding.value) return;
  unbinding.value = provider;
  try {
    await merchantOauthApi.unbind(provider);
    message.success('已解除绑定');
    await loadBindings();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    unbinding.value = '';
  }
}

// 通知偏好
const prefsLoading = ref(true);
const prefsSaving = ref(false);
const prefs = reactive<NotificationPrefs>({
  notify_payment: true,
  notify_settlement: true,
  notify_complaint: true,
  notify_channel: 'mail',
});

function handleChannelChange(value: string) {
  if (value === 'sms' && !profile.value?.phone) {
    message.warning('未绑定手机号，只发邮件');
    prefs.notify_channel = 'mail';
    return;
  }
  prefs.notify_channel = value;
}

async function loadPrefs() {
  prefsLoading.value = true;
  try {
    const data = await merchantNotificationApi.getPrefs();
    prefs.notify_payment = data.notify_payment ?? true;
    prefs.notify_settlement = data.notify_settlement ?? true;
    prefs.notify_complaint = data.notify_complaint ?? true;
    prefs.notify_channel = data.notify_channel === 'sms' ? 'sms' : 'mail';
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    prefsLoading.value = false;
  }
}

async function savePrefs() {
  if (prefsSaving.value) return;
  if (prefs.notify_channel === 'sms' && !profile.value?.phone) {
    message.warning('未绑定手机号，只发邮件');
    prefs.notify_channel = 'mail';
  }
  prefsSaving.value = true;
  try {
    await merchantNotificationApi.savePrefs({ ...prefs });
    message.success('通知偏好已保存');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    prefsSaving.value = false;
  }
}

function reload() {
  void loadProfile();
  void loadBindings();
  void loadPrefs();
}

onMounted(reload);
</script>

<template>
  <admin-page title="账户资料" subtitle="查看商户基本信息，修改对外展示的名称">
    <template #actions>
      <n-button :loading="loading" @click="loadProfile">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
    </template>

    <n-card :bordered="false" title="账户资料">
      <n-spin :show="loading">
        <n-form
          ref="formRef"
          :model="form"
          :rules="rules"
          :label-placement="isNarrow ? 'top' : 'left'"
          :label-width="isNarrow ? undefined : 160"
          @submit.prevent="handleSave"
        >
          <n-grid cols="1 m:2" responsive="screen" item-responsive :x-gap="16">
            <n-gi>
              <n-form-item label="显示名称" path="display_name">
                <n-input v-model:value="form.display_name" placeholder="请输入显示名称" :maxlength="40" show-count />
              </n-form-item>
            </n-gi>
            <n-gi>
              <n-form-item label="结算方式" path="settle_type">
                <n-select v-model:value="form.settle_type" :options="settleOptions" />
              </n-form-item>
            </n-gi>
            <n-gi span="1 m:2">
              <n-form-item label="收款账号" path="settle_account">
                <n-input
                  v-model:value="form.settle_account"
                  placeholder="支付宝账号或银行卡号，留空表示未设置"
                  :maxlength="128"
                />
              </n-form-item>
            </n-gi>
          </n-grid>
          <n-form-item>
            <n-space>
              <n-button type="primary" :loading="saving" attr-type="submit" @click="handleSave">
                <template #icon>
                  <n-icon :component="SaveOutline" />
                </template>
                保存修改
              </n-button>
            </n-space>
          </n-form-item>
        </n-form>
        <n-alert type="info" :bordered="false">
          显示名称会出现在商户中心顶部与对账单中，建议使用公司或店铺名称；邮箱与手机号用于登录与安全校验，如需变更请联系平台运营处理。
        </n-alert>
      </n-spin>
    </n-card>

    <n-card :bordered="false" title="基本信息" class="gp-section-gap">
      <n-spin :show="loading">
        <n-descriptions :column="isNarrow ? 1 : 2" bordered size="small" :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 120">
          <n-descriptions-item label="商户 ID">
            <n-text class="mono" style="word-break: break-all">{{ profile?.id || '-' }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="显示名称">{{ profile?.display_name || '-' }}</n-descriptions-item>
          <n-descriptions-item label="邮箱">{{ profile?.email || '未绑定' }}</n-descriptions-item>
          <n-descriptions-item label="手机号">{{ profile?.phone || '未绑定' }}</n-descriptions-item>
          <n-descriptions-item label="结算方式">{{ settleLabel(profile?.settle_type) }}</n-descriptions-item>
          <n-descriptions-item label="收款账号">{{ profile?.settle_account || '未设置' }}</n-descriptions-item>
          <n-descriptions-item label="账户状态">
            <n-tag v-if="profile" size="small" :bordered="false" :type="activeStatus(profile.status).type">
              {{ activeStatus(profile.status).text }}
            </n-tag>
            <span v-else>-</span>
          </n-descriptions-item>
          <n-descriptions-item label="Legacy UID">{{ profile?.legacy_uid ?? '-' }}</n-descriptions-item>
          <n-descriptions-item label="注册时间">{{ formatDateTime(profile?.created_at) }}</n-descriptions-item>
          <n-descriptions-item label="最近更新">{{ formatDateTime(profile?.updated_at) }}</n-descriptions-item>
        </n-descriptions>
      </n-spin>
    </n-card>

    <n-card :bordered="false" title="修改密码" class="gp-section-gap">
      <n-form
        ref="pwdFormRef"
        :model="pwdForm"
        :rules="pwdRules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
        @submit.prevent="handleChangePassword"
      >
        <n-form-item label="当前密码" path="old_password">
          <n-input
            v-model:value="pwdForm.old_password"
            type="password"
            show-password-on="click"
            placeholder="请输入当前登录密码"
          >
            <template #prefix>
              <n-icon :component="LockClosedOutline" />
            </template>
          </n-input>
        </n-form-item>
        <n-form-item label="新密码" path="new_password">
          <n-input
            v-model:value="pwdForm.new_password"
            type="password"
            show-password-on="click"
            placeholder="至少 6 位字符"
          >
            <template #prefix>
              <n-icon :component="LockClosedOutline" />
            </template>
          </n-input>
        </n-form-item>
        <n-form-item label="确认新密码" path="confirm">
          <n-input
            v-model:value="pwdForm.confirm"
            type="password"
            show-password-on="click"
            placeholder="再次输入新密码"
            @keyup.enter="handleChangePassword"
          >
            <template #prefix>
              <n-icon :component="ShieldCheckmarkOutline" />
            </template>
          </n-input>
        </n-form-item>
        <n-form-item>
          <n-space align="center" :wrap="true">
            <n-button type="primary" :loading="pwdSaving" attr-type="submit" @click="handleChangePassword">
              修改密码
            </n-button>
            <n-text depth="3" class="gp-hint">
              修改成功后当前登录状态仍然有效，下次登录请使用新密码；忘记当前密码可以用登录页的「忘记密码」重置。
            </n-text>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>

    <n-card :bordered="false" title="通知偏好" class="gp-section-gap">
      <n-spin :show="prefsLoading">
        <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
          <n-form-item label="支付成功通知">
            <n-switch v-model:value="prefs.notify_payment" />
          </n-form-item>
          <n-form-item label="结算通知">
            <n-switch v-model:value="prefs.notify_settlement" />
          </n-form-item>
          <n-form-item label="投诉通知">
            <n-switch v-model:value="prefs.notify_complaint" />
          </n-form-item>
          <n-form-item label="通知渠道">
            <n-space align="center" :wrap="true">
              <n-radio-group :value="prefs.notify_channel" @update:value="handleChannelChange">
                <n-radio-button value="mail">邮件</n-radio-button>
                <n-radio-button value="sms">短信</n-radio-button>
              </n-radio-group>
              <n-text v-if="!profile?.phone" depth="3" class="gp-hint">未绑定手机号，只发邮件</n-text>
            </n-space>
          </n-form-item>
          <n-form-item>
            <n-button type="primary" :loading="prefsSaving" @click="savePrefs">保存通知偏好</n-button>
          </n-form-item>
        </n-form>
      </n-spin>
      <n-text depth="3" class="gp-hint">选择接收哪些业务通知，以及通知发送渠道。短信需要先绑定手机号。</n-text>
    </n-card>

    <n-card :bordered="false" title="第三方账号绑定" class="gp-section-gap">
      <n-spin :show="oauthLoading">
        <n-list v-if="bindings.length" bordered>
          <n-list-item v-for="item in bindings" :key="item.provider">
            <n-thing :title="providerText(item.provider)">
              <template #description>
                <n-space :size="8" align="center">
                  <n-icon :component="PersonCircleOutline" />
                  <n-text depth="3">{{ item.nickname || '已绑定' }}</n-text>
                  <n-text depth="3" class="gp-hint">{{ formatDateTime(item.created_at) }}</n-text>
                </n-space>
              </template>
            </n-thing>
            <template #suffix>
              <n-popconfirm @positive-click="handleUnbind(item.provider)">
                <template #trigger>
                  <n-button secondary type="error" size="small" :loading="unbinding === item.provider">解绑</n-button>
                </template>
                解绑后该平台账号将不能再用于登录，确认解绑？
              </n-popconfirm>
            </template>
          </n-list-item>
        </n-list>
        <n-empty v-else description="还没有绑定第三方账号" size="small" />
      </n-spin>
      <n-alert type="info" :bordered="false" style="margin-top: 12px">
        <template #icon>
          <n-icon :component="BusinessOutline" />
        </template>
        绑定后可以直接用第三方账号登录商户中心；解绑后该平台账号将不能再用于登录。绑定入口在登录页的第三方登录按钮：首次授权成功后会自动绑定当前商户账号。
      </n-alert>
    </n-card>
  </admin-page>
</template>
