<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import dayjs from 'dayjs';
import { NButton, NTag, NText, useMessage, type DataTableColumns } from 'naive-ui';
import { AddOutline, CopyOutline, RefreshOutline } from '@vicons/ionicons5';
import { merchantApi } from '@/api';
import type { PageQuery, Settlement } from '@/api/types';
import { errorMessage } from '@/api/client';
import AdminPage from '@/components/AdminPage.vue';
import { copyText, financeStatus, formatDateTime, formatMoney, yuanToCents } from '@/utils/format';
import { prettyJson } from '../admin/shared';
import { useNarrow } from './shared';

const message = useMessage();
const isNarrow = useNarrow();

const loading = ref(false);
const items = ref<Settlement[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const availableBalance = ref(0);
const balanceLoaded = ref(false);
const modalVisible = ref(false);
const submitting = ref(false);
const form = reactive({ amountYuan: null as number | null, destination: '' });
const generatedNo = ref('');

const showDetail = ref(false);
const detail = ref<Settlement | null>(null);
const detailLoading = ref(false);

/** 结算单的打款状态文案，和后台列表保持同一套词 */
const transferLabels: Record<string, { text: string; type: 'default' | 'info' | 'success' | 'warning' | 'error' }> = {
  none: { text: '未打款', type: 'default' },
  pending: { text: '打款中', type: 'warning' },
  done: { text: '已打款', type: 'success' },
  failed: { text: '打款失败', type: 'error' },
};

function transferMeta(status?: string) {
  return transferLabels[status ?? ''] ?? { text: status || '-', type: 'default' as const };
}

const rowKey = (row: Settlement) => row.id;

const columns: DataTableColumns<Settlement> = [
  {
    title: '结算单号',
    key: 'settlement_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono' }, () => row.settlement_no || '-'),
  },
  {
    title: '商户结算单号',
    key: 'out_settlement_no',
    width: 200,
    ellipsis: { tooltip: true },
    render: (row) => h(NText, { class: 'mono', depth: 3 }, () => row.out_settlement_no || '-'),
  },
  {
    title: '结算金额',
    key: 'amount',
    width: 130,
    align: 'right',
    render: (row) => h(NText, { class: 'amount' }, () => formatMoney(row.amount)),
  },
  { title: '币种', key: 'currency', width: 80 },
  { title: '收款账户', key: 'destination', minWidth: 180, ellipsis: { tooltip: true } },
  {
    title: '状态',
    key: 'status',
    width: 110,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false, type: financeStatus(row.status).type }, () =>
        financeStatus(row.status).text,
      ),
  },
  {
    title: '申请时间',
    key: 'created_at',
    width: 170,
    render: (row) => formatDateTime(row.created_at),
  },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right',
    render: (row) =>
      h(NButton, { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) }, () => '详情'),
  },
];

/** 结算单详情：GET /merchant/settlements/:id */
async function openDetail(row: Settlement) {
  detail.value = row;
  showDetail.value = true;
  detailLoading.value = true;
  try {
    detail.value = await merchantApi.settlement(row.id);
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    detailLoading.value = false;
  }
}

async function copyDetail() {
  const ok = await copyText(prettyJson(detail.value));
  if (ok) message.success('已复制到剪贴板');
  else message.error('复制失败，请手动选择文本复制');
}

const pagination = computed(() => ({
  page: page.value,
  pageSize: pageSize.value,
  itemCount: total.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  prefix: ({ itemCount }: { itemCount: number }) => `共 ${itemCount} 条`,
}));

function generateOutSettlementNo(): string {
  return `ST${dayjs().format('YYYYMMDDHHmmss')}${Math.floor(Math.random() * 900 + 100)}`;
}

async function load() {
  loading.value = true;
  try {
    const query: PageQuery = { page: page.value, page_size: pageSize.value };
    const res = await merchantApi.settlements(query);
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

async function loadBalance() {
  try {
    const stats = await merchantApi.dashboard();
    availableBalance.value = stats.available_balance;
    balanceLoaded.value = true;
  } catch {
    // 余额仅用于提示与前置校验，接口不可用时交给服务端校验
    balanceLoaded.value = false;
  }
}

async function openModal() {
  form.amountYuan = null;
  form.destination = '';
  generatedNo.value = generateOutSettlementNo();
  modalVisible.value = true;
  await loadBalance();
}

async function submit() {
  const cents = yuanToCents(form.amountYuan ?? 0);
  if (!Number.isFinite(cents) || cents <= 0) {
    message.error('请输入有效的结算金额');
    return;
  }
  if (balanceLoaded.value && cents > availableBalance.value) {
    message.error(`结算金额不能超过可用余额 ${formatMoney(availableBalance.value)}`);
    return;
  }
  if (!form.destination.trim()) {
    message.error('请填写收款账户信息');
    return;
  }
  submitting.value = true;
  try {
    await merchantApi.createSettlement({
      out_settlement_no: generatedNo.value,
      amount: cents,
      currency: 'CNY',
      destination: form.destination.trim(),
    });
    message.success('结算申请已提交，等待平台审核');
    modalVisible.value = false;
    page.value = 1;
    void load();
    void loadBalance();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    submitting.value = false;
  }
}

function handlePageChange(value: number) {
  page.value = value;
  void load();
}

function handlePageSizeChange(value: number) {
  pageSize.value = value;
  page.value = 1;
  void load();
}

onMounted(() => {
  void load();
  void loadBalance();
});
</script>

<template>
  <admin-page title="结算记录" subtitle="提交结算申请并跟踪审核、出款进度">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" @click="openModal">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          申请结算
        </n-button>
      </n-space>
    </template>

    <n-card size="small" :bordered="false">
      <n-data-table
        remote
        :columns="columns"
        :data="items"
        :loading="loading"
        :pagination="pagination"
        :bordered="false"
        :row-key="rowKey"
        :scroll-x="1160"
        :size="isNarrow ? 'small' : 'medium'"
        @update:page="handlePageChange"
        @update:page-size="handlePageSizeChange"
      >
        <template #empty>
          <n-empty description="暂无结算记录" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal v-model:show="modalVisible" preset="card" title="申请结算" style="width: 620px; max-width: 94vw">
      <n-space vertical :size="16">
        <n-alert type="info" :bordered="false">
          <template v-if="balanceLoaded">
            可用余额 <n-text strong class="amount">{{ formatMoney(availableBalance) }}</n-text>
            。结算申请提交后进入平台审核，审核通过后按收款账户出款。
          </template>
          <template v-else>暂时无法获取可用余额，最终可结算金额以平台审核结果为准。</template>
        </n-alert>

        <n-form :label-placement="isNarrow ? 'top' : 'left'" :label-width="isNarrow ? undefined : 160">
          <n-form-item label="结算金额（元）">
            <n-input-number
              v-model:value="form.amountYuan"
              :precision="2"
              :min="0.01"
              placeholder="0.00"
              style="width: 100%"
            >
              <template #prefix>¥</template>
            </n-input-number>
          </n-form-item>
          <n-form-item label="收款账户">
            <n-input
              v-model:value="form.destination"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              placeholder="请输入收款账户信息，例如：招商银行 6225 **** 1234 张三"
            />
          </n-form-item>
          <n-form-item label="币种">
            <n-tag :bordered="false" type="default">CNY（固定）</n-tag>
          </n-form-item>
          <n-form-item label="商户结算单号">
            <n-input :value="generatedNo" readonly class="mono" />
          </n-form-item>
        </n-form>
      </n-space>
      <template #footer>
        <n-space justify="end" :wrap="true">
          <n-button @click="modalVisible = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">提交申请</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-drawer v-model:show="showDetail" :width="isNarrow ? '92vw' : 560" placement="right">
      <n-drawer-content title="结算单详情" closable>
        <n-spin :show="detailLoading">
          <template v-if="detail">
            <n-descriptions :column="isNarrow ? 1 : 2" bordered label-placement="left" size="small">
              <n-descriptions-item label="结算单号">
                <span class="mono">{{ detail.settlement_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag size="small" round :bordered="false" :type="financeStatus(detail.status).type">
                  {{ financeStatus(detail.status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="商户结算单号">
                <span class="mono">{{ detail.out_settlement_no || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="结算金额">
                <span class="amount">{{ formatMoney(detail.amount) }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="币种">{{ detail.currency || '-' }}</n-descriptions-item>
              <n-descriptions-item label="手续费">
                <span class="amount">{{ formatMoney(detail.fee_cents ?? 0) }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="来源">{{ detail.source || '-' }}</n-descriptions-item>
              <n-descriptions-item label="打款状态">
                <n-tag size="small" round :bordered="false" :type="transferMeta(detail.transfer_status).type">
                  {{ transferMeta(detail.transfer_status).text }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="打款时间">{{ formatDateTime(detail.transfer_at) }}</n-descriptions-item>
              <n-descriptions-item label="审核时间">{{ formatDateTime(detail.processed_at) }}</n-descriptions-item>
              <n-descriptions-item label="收款账户" :span="isNarrow ? 1 : 2">
                {{ detail.destination || '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="驳回原因" :span="isNarrow ? 1 : 2">
                <span :class="{ 'danger-text': !!detail.rejection_reason }">{{ detail.rejection_reason || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDateTime(detail.created_at) }}</n-descriptions-item>
              <n-descriptions-item label="更新时间">{{ formatDateTime(detail.updated_at) }}</n-descriptions-item>
            </n-descriptions>

            <n-space justify="end" style="margin-top: 12px">
              <n-button size="small" text type="primary" @click="copyDetail">
                <template #icon>
                  <n-icon :component="CopyOutline" />
                </template>
                复制 JSON
              </n-button>
            </n-space>
            <n-code :code="prettyJson(detail)" language="json" word-wrap />
          </template>
          <n-empty v-else description="暂无数据" size="small" />
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </admin-page>
</template>

<style scoped>
.danger-text {
  color: var(--gp-danger);
}
</style>
