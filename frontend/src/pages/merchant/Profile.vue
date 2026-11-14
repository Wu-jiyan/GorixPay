<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useMessage, type FormInst, type FormRules } from 'naive-ui';
import { merchantApi } from '@/api';
import { accountSecurityApi } from '@/api/accountSecurity';
import type { Merchant } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import { formatDateTime } from '@/utils/format';
import { useAuthStore } from '@/stores/auth';

const message = useMessage();
const auth = useAuthStore();

const loading = ref(true);
const saving = ref(false);
const profile = ref<Merchant | null>(null);
const formRef = ref<FormInst | null>(null);
const form = reactive({
  display_name: '',
  settle_type: 0,
  settle_account: '',
});

const settleOptions = [
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

onMounted(() => {
  void loadProfile();
});
</script>

<template>
  <div class="page">
    <PageHeader title="账户资料" subtitle="查看商户基本信息，修改对外展示的名称">
      <template #actions>
        <n-button size="small" :loading="loading" @click="loadProfile">刷新</n-button>
      </template>
    </PageHeader>

    <div class="profile-grid">
      <n-card :bordered="false" title="基本信息" class="profile-card">
        <n-spin :show="loading">
          <n-descriptions :column="1" label-placement="left" bordered size="small">
            <n-descriptions-item label="商户 ID">
              <span class="mono">{{ profile?.id || '-' }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="显示名称">{{ profile?.display_name || '-' }}</n-descriptions-item>
            <n-descriptions-item label="邮箱">{{ profile?.email || '未绑定' }}</n-descriptions-item>
            <n-descriptions-item label="手机号">{{ profile?.phone || '未绑定' }}</n-descriptions-item>
            <n-descriptions-item label="结算方式">{{ settleLabel(profile?.settle_type) }}</n-descriptions-item>
            <n-descriptions-item label="收款账号">{{ profile?.settle_account || '未设置' }}</n-descriptions-item>
            <n-descriptions-item label="账户状态">
              <StatusTag v-if="profile" :status="profile.status" kind="active" />
              <span v-else>-</span>
            </n-descriptions-item>
            <n-descriptions-item label="注册时间">{{ formatDateTime(profile?.created_at) }}</n-descriptions-item>
            <n-descriptions-item label="最近更新">{{ formatDateTime(profile?.updated_at) }}</n-descriptions-item>
            <n-descriptions-item label="Legacy UID">
              {{ profile?.legacy_uid ?? '-' }}
            </n-descriptions-item>
          </n-descriptions>
        </n-spin>
      </n-card>

      <n-card :bordered="false" title="修改显示名称" class="profile-card">
        <p class="profile-desc">显示名称会出现在商户中心顶部与对账单中，建议使用公司或店铺名称。</p>
        <n-form ref="formRef" :model="form" :rules="rules" label-placement="top" @submit.prevent="handleSave">
          <n-form-item label="显示名称" path="display_name">
            <n-input v-model:value="form.display_name" placeholder="请输入显示名称" :maxlength="40" show-count />
          </n-form-item>
          <n-form-item label="结算方式" path="settle_type">
            <n-select v-model:value="form.settle_type" :options="settleOptions" />
          </n-form-item>
          <n-form-item label="收款账号" path="settle_account">
            <n-input
              v-model:value="form.settle_account"
              placeholder="支付宝账号或银行卡号，留空表示未设置"
              :maxlength="128"
            />
          </n-form-item>
          <n-button type="primary" :loading="saving" attr-type="submit" @click="handleSave">保存修改</n-button>
        </n-form>
        <n-alert type="info" :bordered="false" class="profile-alert">
          邮箱与手机号用于登录与安全校验，如需变更请联系平台运营处理。
        </n-alert>
      </n-card>

      <n-card :bordered="false" title="修改密码" class="profile-card password-card">
        <p class="profile-desc">
          修改成功后当前登录状态仍然有效，下次登录请使用新密码；忘记当前密码可以用登录页的「忘记密码」重置。
        </p>
        <n-form
          ref="pwdFormRef"
          :model="pwdForm"
          :rules="pwdRules"
          label-placement="top"
          @submit.prevent="handleChangePassword"
        >
          <n-form-item label="当前密码" path="old_password">
            <n-input
              v-model:value="pwdForm.old_password"
              type="password"
              show-password-on="click"
              placeholder="请输入当前登录密码"
            />
          </n-form-item>
          <n-form-item label="新密码" path="new_password">
            <n-input
              v-model:value="pwdForm.new_password"
              type="password"
              show-password-on="click"
              placeholder="至少 6 位字符"
            />
          </n-form-item>
          <n-form-item label="确认新密码" path="confirm">
            <n-input
              v-model:value="pwdForm.confirm"
              type="password"
              show-password-on="click"
              placeholder="再次输入新密码"
              @keyup.enter="handleChangePassword"
            />
          </n-form-item>
          <n-button type="primary" :loading="pwdSaving" attr-type="submit" @click="handleChangePassword">
            修改密码
          </n-button>
        </n-form>
      </n-card>
    </div>
  </div>
</template>

<style scoped>
.profile-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.profile-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.profile-desc {
  margin: 0 0 16px;
  font-size: 13px;
  color: #64748b;
  line-height: 1.8;
}
.password-card {
  grid-column: 1 / -1;
}
.profile-alert {
  margin-top: 18px;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
}
@media (max-width: 1000px) {
  .profile-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
