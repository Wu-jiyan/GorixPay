<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useMessage } from 'naive-ui';
import { RefreshOutline, SaveOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import { merchantNotificationApi } from '@/api/merchantNotifications';
import type { NotificationPrefs } from '@/api/merchantNotifications';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { formatDateTime } from '@/utils/format';
import { useNarrow } from './shared';

/**
 * 商户通知偏好：GET / PUT /merchant/notification-prefs。
 * 字段对齐 internal/notifyprefs：notify_payment / notify_settlement / notify_complaint /
 * notify_channel（mail | sms | wechat）。
 */
const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(false);
const saving = ref(false);
const savedAt = ref('');
const phone = ref('');
const phoneLoaded = ref(false);

const form = reactive<NotificationPrefs>({
  notify_payment: true,
  notify_settlement: true,
  notify_complaint: true,
  notify_channel: 'mail',
});

interface ChannelOption {
  label: string;
  value: string;
}

const channelOptions: ChannelOption[] = [
  { label: '邮件', value: 'mail' },
  { label: '短信', value: 'sms' },
  { label: '公众号', value: 'wechat' },
];

const hasPhone = computed(() => phone.value.trim() !== '');
/** 选短信但没绑手机号：后端会降级发邮件，先在这儿说清楚 */
const smsWithoutPhone = computed(() => form.notify_channel === 'sms' && phoneLoaded.value && !hasPhone.value);

const channelSummary = computed(() => {
  if (form.notify_channel === 'sms') {
    return hasPhone.value
      ? `短信会发到已绑定的手机号 ${phone.value}`
      : '账号还没有绑定手机号，短信会降级为邮件发送';
  }
  if (form.notify_channel === 'wechat') {
    return '通过公众号模板消息推送；未绑定公众号或模板未配置时自动退回邮件';
  }
  return '通知会发到账号绑定的邮箱';
});

async function loadPrefs() {
  loading.value = true;
  try {
    const prefs = await merchantNotificationApi.getPrefs();
    form.notify_payment = prefs.notify_payment;
    form.notify_settlement = prefs.notify_settlement;
    form.notify_complaint = prefs.notify_complaint;
    form.notify_channel = prefs.notify_channel || 'mail';
    // 从未保存过的商户后端回的是零值时间（0001-01-01），直接显示会像个 bug。
    const updated = prefs.updated_at || '';
    savedAt.value = updated.startsWith('0001-') ? '' : updated;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

/** 手机号只用于提示短信能否真的发出去，拿不到就当没绑，按降级提示处理 */
async function loadPhone() {
  try {
    const profile = await merchantApi.profile();
    phone.value = (profile.phone ?? '').toString();
    phoneLoaded.value = true;
  } catch {
    phoneLoaded.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    await merchantNotificationApi.savePrefs({
      notify_payment: form.notify_payment,
      notify_settlement: form.notify_settlement,
      notify_complaint: form.notify_complaint,
      notify_channel: form.notify_channel,
    });
    message.success('通知偏好已保存');
    // 保存成功后重取一次，把后端回写的 updated_at 带上
    await loadPrefs();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    saving.value = false;
  }
}

function reload() {
  void loadPrefs();
  void loadPhone();
}

onMounted(reload);
</script>

<template>
  <admin-page title="通知设置" subtitle="选择哪些场景通知你，以及用哪个渠道送达">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="reload">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" :loading="saving" @click="save">
          <template #icon>
            <n-icon :component="SaveOutline" />
          </template>
          保存
        </n-button>
      </n-space>
    </template>

    <n-spin :show="loading">
      <n-card :bordered="false">
        <template #header>通知场景</template>
        <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
          <n-form-item label="支付成功">
            <n-space vertical :size="4">
              <n-switch v-model:value="form.notify_payment" />
              <n-text depth="3" class="gp-hint">订单支付成功时通知，含订单号与金额</n-text>
            </n-space>
          </n-form-item>
          <n-form-item label="结算结果">
            <n-space vertical :size="4">
              <n-switch v-model:value="form.notify_settlement" />
              <n-text depth="3" class="gp-hint">结算审核通过或驳回时通知</n-text>
            </n-space>
          </n-form-item>
          <n-form-item label="投诉进展">
            <n-space vertical :size="4">
              <n-switch v-model:value="form.notify_complaint" />
              <n-text depth="3" class="gp-hint">买家提交投诉、平台回复时通知</n-text>
            </n-space>
          </n-form-item>
        </n-form>
      </n-card>

      <n-card :bordered="false" class="gp-section-gap">
        <template #header>通知渠道</template>
        <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
          <n-form-item label="送达方式">
            <n-space vertical :size="8">
              <n-radio-group v-model:value="form.notify_channel">
                <n-radio v-for="option in channelOptions" :key="String(option.value)" :value="option.value">
                  {{ option.label }}
                </n-radio>
              </n-radio-group>
              <n-text depth="3" class="gp-hint">{{ channelSummary }}</n-text>
            </n-space>
          </n-form-item>
        </n-form>

        <n-alert v-if="smsWithoutPhone" type="warning" :bordered="false" style="margin-top: 8px">
          当前账号还没有绑定手机号。选短信时后端会自动降级为邮件发送，不会丢通知；想真正收到短信请先到
          <n-text strong>账户资料</n-text> 绑定手机号。
        </n-alert>
        <n-alert
          v-else-if="form.notify_channel === 'wechat'"
          type="info"
          :bordered="false"
          style="margin-top: 8px"
        >
          公众号通知需要先绑定公众号并配置模板消息，否则会退回邮件发送。
        </n-alert>
      </n-card>

      <n-text v-if="savedAt" depth="3" class="gp-hint" style="display: block; margin-top: 12px">
        上次保存：{{ formatDateTime(savedAt) }}
      </n-text>
    </n-spin>
  </admin-page>
</template>