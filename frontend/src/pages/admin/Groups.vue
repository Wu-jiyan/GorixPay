<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NIcon, NPopconfirm, NSpace, NTag, NText, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import {
  AddOutline,
  CheckmarkCircleOutline,
  PeopleOutline,
  RefreshOutline,
  SearchOutline,
} from '@vicons/ionicons5';
import AdminPage from '@/components/AdminPage.vue';
import { adminApi } from '@/api';
import { groupsApi } from '@/api/groups';
import type { MerchantGroup } from '@/api/groups';
import { errorMessage } from '@/api/client';
import { activeStatus, formatDateTime, paymentTypeText } from '@/utils/format';
import {
  asRow,
  httpStatus,
  itemsOf,
  numberOr,
  stringList,
  textOf,
  useNarrow,
  usePendingAction,
  useServerPaging,
} from './shared';

interface GroupForm {
  name: string;
  rate_percent: number | null;
  pay_types: string[];
  settle_mode: string;
  price_yuan: number | null;
}

/** 归组弹窗里挑商户用：后端 merchants 列表序列化的是 GroupID（结构体没有 json tag） */
interface MerchantOption {
  id: string;
  name: string;
  email: string;
  groupId: string | null;
}

const message = useMessage();
const isNarrow = useNarrow();

const rows = ref<MerchantGroup[]>([]);
const loading = ref(false);
const submitting = ref(false);
const showForm = ref(false);
const editingId = ref<string | null>(null);
const editingStatus = ref('active');
const formRef = ref<FormInst | null>(null);

const actions = usePendingAction();

const showAssign = ref(false);
const assignGroupId = ref<string | null>(null);
const assignMerchantId = ref('');
const assignSubmitting = ref(false);
const assignResult = ref('');
const assignLoading = ref(false);
const merchantKeyword = ref('');
const merchantOptions = ref<MerchantOption[]>([]);
const allGroups = ref<MerchantGroup[]>([]);

const { page, pageSize, total, pagination, onPage, onPageSize } = useServerPaging(load);

const form = reactive<GroupForm>({
  name: '',
  rate_percent: 0,
  pay_types: [],
  settle_mode: 'normal',
  price_yuan: 0,
});

const payTypeOptions: SelectOption[] = [
  { label: '支付宝', value: 'alipay' },
  { label: '微信支付', value: 'wxpay' },
  { label: 'QQ 钱包', value: 'qqpay' },
  { label: '银联', value: 'unionpay' },
  { label: '网银', value: 'bank' },
  { label: 'USDT', value: 'usdt' },
  { label: '收银台', value: 'cashier' },
];

const settleModeOptions: SelectOption[] = [
  { label: '普通（normal）', value: 'normal' },
  { label: '直连（direct）', value: 'direct' },
];

const settleModeLabels: Record<string, string> = {
  normal: '普通',
  direct: '直连',
};

const rules: FormRules = {
  name: { required: true, message: '请输入用户组名称', trigger: ['blur', 'input'] },
  rate_percent: { required: true, type: 'number', message: '请输入费率', trigger: ['blur', 'change'] },
  settle_mode: { required: true, message: '请选择结算模式', trigger: ['change', 'blur'] },
};

const isEditing = computed(() => editingId.value !== null);

/** 录入的是百分比，落库是万分比：0.60% → 60bp */
const rateBpPreview = computed(() => Math.round(numberOr(form.rate_percent, 0) * 100));

function groupOptionOf(group: MerchantGroup): SelectOption {
  return { label: `${group.name || group.id}（${(numberOr(group.rate_bp, 0) / 100).toFixed(2)}%）`, value: group.id };
}

const assignGroupOptions = computed<SelectOption[]>(() =>
  (allGroups.value.length > 0 ? allGroups.value : rows.value).map(groupOptionOf),
);

const groupNameById = computed(() => {
  const map = new Map<string, string>();
  const source = allGroups.value.length > 0 ? allGroups.value : rows.value;
  source.forEach((group) => map.set(group.id, group.name || group.id));
  return map;
});

const filteredMerchants = computed<MerchantOption[]>(() => {
  const keyword = merchantKeyword.value.trim().toLowerCase();
  if (keyword === '') return merchantOptions.value;
  return merchantOptions.value.filter(
    (item) =>
      item.name.toLowerCase().includes(keyword) ||
      item.id.toLowerCase().includes(keyword) ||
      item.email.toLowerCase().includes(keyword),
  );
});

const selectedMerchant = computed<MerchantOption | null>(
  () => merchantOptions.value.find((item) => item.id === assignMerchantId.value) ?? null,
);

const currentGroupText = computed(() => {
  const merchant = selectedMerchant.value;
  if (!merchant) return '';
  if (!merchant.groupId) return '未分组';
  return groupNameById.value.get(merchant.groupId) ?? merchant.groupId;
});

function groupLabelOf(groupId: string | null): string {
  if (!groupId) return '未分组';
  return groupNameById.value.get(groupId) ?? groupId;
}

function selectMerchant(row: MerchantOption) {
  assignMerchantId.value = row.id;
  // 选中后把目标组预置成它当前的组，用户仍可改
  if (row.groupId) assignGroupId.value = row.groupId;
  assignResult.value = '';
}

function merchantRowProps(row: MerchantOption) {
  return {
    style: `cursor: pointer;${row.id === assignMerchantId.value ? ' background: var(--gp-accent-soft);' : ''}`,
    onClick: () => selectMerchant(row),
  };
}

const merchantColumns = computed<DataTableColumns<MerchantOption>>(() => [
  {
    title: '选择',
    key: 'selected',
    width: 60,
    align: 'center',
    render: (row) =>
      row.id === assignMerchantId.value
        ? h(NIcon, { component: CheckmarkCircleOutline, size: 18, color: 'var(--n-success-color)' })
        : h('span', { class: 'muted' }, '—'),
  },
  {
    title: '商户',
    key: 'name',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) =>
      h('div', null, [
        h('div', row.name || row.id),
        h('div', { class: 'mono muted gp-hint' }, row.id),
      ]),
  },
  {
    title: '邮箱',
    key: 'email',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) => row.email || '-',
  },
  {
    title: '所属组',
    key: 'groupId',
    width: 170,
    render: (row) =>
      row.groupId
        ? h(NTag, { size: 'small', round: true, bordered: false, type: 'success' }, {
            default: () => groupLabelOf(row.groupId),
          })
        : h('span', { class: 'muted' }, '未分组'),
  },
]);

async function load() {
  loading.value = true;
  try {
    const data = await groupsApi.list({ page: page.value, page_size: pageSize.value });
    const items = itemsOf<MerchantGroup>(data);
    rows.value = items;
    total.value = numberOr(asRow(data).total, items.length);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  form.name = '';
  form.rate_percent = 0;
  form.pay_types = [];
  form.settle_mode = 'normal';
  form.price_yuan = 0;
}

function openCreate() {
  editingId.value = null;
  editingStatus.value = 'active';
  resetForm();
  showForm.value = true;
}

function openEdit(row: MerchantGroup) {
  editingId.value = row.id;
  editingStatus.value = row.status || 'active';
  form.name = row.name || '';
  form.rate_percent = numberOr(row.rate_bp, 0) / 100;
  form.pay_types = stringList(row.pay_types);
  form.settle_mode = row.settle_mode || 'normal';
  form.price_yuan = numberOr(row.price_cents, 0) / 100;
  showForm.value = true;
}

async function submit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const payload = {
      name: form.name.trim(),
      // 页面按百分比录入，后端存万分比
      rate_bp: Math.round(numberOr(form.rate_percent, 0) * 100),
      pay_types: form.pay_types,
      settle_mode: form.settle_mode,
      status: editingStatus.value,
    };
    const priceCents = Math.round(numberOr(form.price_yuan, 0) * 100);
    if (editingId.value) {
      await groupsApi.update(editingId.value, payload);
      message.success('用户组已更新');
      try {
        // 买断价走独立接口，单位分
        await groupsApi.setPrice(editingId.value, priceCents);
      } catch (error) {
        message.error(`买断价保存失败：${errorMessage(error)}`);
      }
    } else {
      const created = await groupsApi.create(payload);
      message.success('用户组已创建');
      const newId = textOf(asRow(created).id, '');
      if (newId) {
        try {
          await groupsApi.setPrice(newId, priceCents);
        } catch (error) {
          message.error(`买断价保存失败：${errorMessage(error)}`);
        }
      }
    }
    showForm.value = false;
    await load();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: MerchantGroup) {
  const next = row.status === 'active' ? 'disabled' : 'active';
  await actions.run(`toggle:${row.id}`, async () => {
    try {
      await groupsApi.setStatus(row.id, next);
      message.success(next === 'active' ? '用户组已启用' : '用户组已禁用');
      await load();
    } catch (error) {
      message.error(errorMessage(error));
    }
  });
}

async function remove(row: MerchantGroup) {
  await actions.run(`remove:${row.id}`, async () => {
    try {
      await groupsApi.remove(row.id);
      message.success('用户组已删除');
      await load();
    } catch (error) {
      if (httpStatus(error) === 409) {
        message.error('该用户组下还有商户在使用，先把商户移出组再删除');
        return;
      }
      message.error(errorMessage(error));
    }
  });
}

function openAssign(row: MerchantGroup | null) {
  assignGroupId.value = row ? row.id : null;
  assignMerchantId.value = '';
  assignResult.value = '';
  merchantKeyword.value = '';
  showAssign.value = true;
  void loadAssignData();
}

async function loadAssignData() {
  assignLoading.value = true;
  try {
    const [merchantPage, groupPage] = await Promise.all([
      adminApi.merchants({ page: 1, page_size: 100 }),
      groupsApi.list({ page: 1, page_size: 100 }),
    ]);
    merchantOptions.value = itemsOf<Record<string, unknown>>(merchantPage).map((item) => {
      const raw = asRow(item);
      // 后端 Merchant 结构体没有 json tag，组字段序列化出来是 GroupID；这里同时兼容 snake_case
      const rawGroupId = raw.GroupID ?? raw.group_id;
      return {
        id: textOf(raw.id, ''),
        name: textOf(raw.display_name, ''),
        email: textOf(raw.email, ''),
        groupId: typeof rawGroupId === 'string' && rawGroupId !== '' ? rawGroupId : null,
      };
    });
    allGroups.value = itemsOf<MerchantGroup>(groupPage);
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    assignLoading.value = false;
  }
}

/** 按 group_id 归组；选「不分组」时传 null 表示移出所有组 */
async function submitAssign(moveOut = false) {
  const merchantId = assignMerchantId.value.trim();
  if (merchantId === '') {
    message.warning('请先选择商户');
    return;
  }
  assignSubmitting.value = true;
  try {
    const result = await groupsApi.assignMerchant(merchantId, moveOut ? null : assignGroupId.value);
    const data = asRow(result);
    const groupId = textOf(data.group_id, '');
    assignResult.value =
      groupId === ''
        ? `商户 ${merchantId} 已移出用户组，结算模式 settle_mode=${textOf(data.settle_mode, '-')}`
        : `商户 ${merchantId} 已归入「${groupNameById.value.get(groupId) ?? groupId}」`;
    message.success(moveOut ? '商户已移出用户组' : '商户已归组');
    await load();
    // 归组会改变商户的 group_id，重新拉一次列表让「所属组」列立刻更新
    await loadAssignData();
  } catch (error) {
    message.error(errorMessage(error));
  } finally {
    assignSubmitting.value = false;
  }
}

function rateText(rateBp?: number): string {
  return `${(numberOr(rateBp, 0) / 100).toFixed(2)}%`;
}

function payTypeCell(row: MerchantGroup) {
  const list = stringList(row.pay_types);
  if (list.length === 0) return h('span', { class: 'muted' }, '全部');
  return h(NSpace, { size: 4, wrap: true }, {
    default: () =>
      list.map((item) =>
        h(NTag, { size: 'small', round: true, bordered: false }, { default: () => paymentTypeText(item) }),
      ),
  });
}

const columns = computed<DataTableColumns<MerchantGroup>>(() => {
  const pendingSet = actions.pending.value;
  return [
    {
      title: '名称',
      key: 'name',
      minWidth: 160,
      ellipsis: { tooltip: true },
      render: (row) => row.name || '-',
    },
    {
      title: '费率',
      key: 'rate_bp',
      width: 100,
      align: 'right',
      render: (row) => h(NText, { class: 'amount' }, { default: () => rateText(row.rate_bp) }),
    },
    { title: '可用支付方式', key: 'pay_types', minWidth: 240, render: (row) => payTypeCell(row) },
    {
      title: '结算模式',
      key: 'settle_mode',
      width: 110,
      render: (row) => settleModeLabels[row.settle_mode || ''] ?? row.settle_mode ?? '-',
    },
    {
      title: '商户数',
      key: 'merchant_count',
      width: 90,
      align: 'right',
      render: (row) => (typeof row.merchant_count === 'number' ? row.merchant_count : '-'),
    },
    {
      title: '状态',
      key: 'status',
      width: 90,
      render: (row) =>
        h(NTag, { size: 'small', round: true, bordered: false, type: activeStatus(row.status).type }, {
          default: () => activeStatus(row.status).text,
        }),
    },
    {
      title: '创建时间',
      key: 'created_at',
      width: 170,
      render: (row) => formatDateTime(row.created_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 250,
      fixed: 'right',
      render: (row) => {
        const toggling = pendingSet.has(`toggle:${row.id}`);
        const removing = pendingSet.has(`remove:${row.id}`);
        const busy = toggling || removing;
        return h(NSpace, { size: 12, wrap: false, align: 'center' }, {
          default: () => [
            h(
              NButton,
              { text: true, size: 'small', type: 'primary', onClick: () => openEdit(row) },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              { text: true, size: 'small', onClick: () => openAssign(row) },
              { default: () => '商户归组' },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => toggleStatus(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      text: true,
                      size: 'small',
                      type: row.status === 'active' ? 'warning' : 'primary',
                      loading: toggling,
                      disabled: busy,
                    },
                    { default: () => (row.status === 'active' ? '禁用' : '启用') },
                  ),
                default: () => (row.status === 'active' ? '确认禁用该用户组？' : '确认启用该用户组？'),
              },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => remove(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { text: true, size: 'small', type: 'error', loading: removing, disabled: busy },
                    { default: () => '删除' },
                  ),
                default: () => '确认删除该用户组？删除后不可恢复。',
              },
            ),
          ],
        });
      },
    },
  ];
});

onMounted(load);
</script>

<template>
  <admin-page title="用户组与费率" subtitle="按用户组设置商户费率、可用支付方式与结算模式">
    <template #actions>
      <n-space :wrap="true">
        <n-button :loading="loading" @click="load">
          <template #icon>
            <n-icon :component="RefreshOutline" />
          </template>
          刷新
        </n-button>
        <n-button type="primary" @click="openCreate">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          新建用户组
        </n-button>
      </n-space>
    </template>

    <n-card :bordered="false">
      <n-data-table
        :bordered="false"
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: MerchantGroup) => row.id"
        :scroll-x="1350"
        :size="isNarrow ? 'small' : 'medium'"
        remote
        @update:page="onPage"
        @update:page-size="onPageSize"
      >
        <template #empty>
          <n-empty description="暂无用户组，点击右上角新建" size="small" />
        </template>
      </n-data-table>
    </n-card>

    <n-modal
      v-model:show="showForm"
      preset="card"
      :title="isEditing ? '编辑用户组' : '新建用户组'"
      style="width: 640px; max-width: 94vw"
    >
      <n-form
        ref="formRef"
        :model="form"
        :rules="rules"
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
      >
        <n-form-item label="用户组名称" path="name">
          <n-input v-model:value="form.name" placeholder="例如：普通商户 / 大客户" />
        </n-form-item>
        <n-form-item label="费率" path="rate_percent">
          <n-space align="center" :wrap="true" :size="10">
            <n-input-number
              v-model:value="form.rate_percent"
              :min="0"
              :max="100"
              :step="0.01"
              :precision="2"
              style="width: 100%; max-width: 180px"
            />
            <n-text depth="3" class="gp-hint">
              按百分比填（100 = 1%），保存为万分比 {{ rateBpPreview }}bp
            </n-text>
          </n-space>
        </n-form-item>
        <n-form-item label="可用支付方式">
          <n-select
            v-model:value="form.pay_types"
            multiple
            :options="payTypeOptions"
            placeholder="留空表示全部支付方式"
          />
        </n-form-item>
        <n-form-item label="结算模式" path="settle_mode">
          <n-select v-model:value="form.settle_mode" :options="settleModeOptions" />
        </n-form-item>
        <n-form-item label="买断价（元）">
          <n-space align="center" :wrap="true" :size="10">
            <n-input-number
              v-model:value="form.price_yuan"
              :min="0"
              :step="10"
              :precision="2"
              style="width: 100%; max-width: 180px"
            />
            <n-text depth="3" class="gp-hint">
              0 表示不单独售卖；保存走 PATCH /admin/groups/:id/price
            </n-text>
          </n-space>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">取消</n-button>
          <n-button type="primary" :loading="submitting" @click="submit">保存</n-button>
        </n-space>
      </template>
    </n-modal>

    <n-modal v-model:show="showAssign" preset="card" title="商户归组" style="width: 780px; max-width: 94vw">
      <n-alert type="info" :bordered="false" style="margin-bottom: 14px">
        归组后该商户的费率、可用支付方式与结算模式按用户组生效，会覆盖商户原有设置。
      </n-alert>

      <n-grid cols="1 s:2" responsive="screen" :x-gap="12" :y-gap="12" style="margin-bottom: 10px">
        <n-gi>
          <n-input
            v-model:value="merchantKeyword"
            placeholder="按商户名称 / ID / 邮箱过滤"
            clearable
            size="small"
            style="width: 100%"
          >
            <template #prefix>
              <n-icon :component="SearchOutline" />
            </template>
          </n-input>
        </n-gi>
        <n-gi>
          <n-text depth="3" class="gp-hint">共 {{ filteredMerchants.length }} 个商户</n-text>
        </n-gi>
      </n-grid>

      <n-data-table
        :bordered="false"
        :columns="merchantColumns"
        :data="filteredMerchants"
        :loading="assignLoading"
        :row-props="merchantRowProps"
        :max-height="280"
        :pagination="false"
        :row-key="(row: MerchantOption) => row.id"
        :scroll-x="700"
        :size="isNarrow ? 'small' : 'medium'"
      >
        <template #empty>
          <n-empty description="没有匹配到商户" size="small" />
        </template>
      </n-data-table>

      <n-form
        :label-placement="isNarrow ? 'top' : 'left'"
        :label-width="isNarrow ? undefined : 160"
        style="margin-top: 16px"
        :show-feedback="false"
      >
        <n-form-item label="当前所属组">
          <n-space align="center" :wrap="true" :size="8">
            <n-tag v-if="assignMerchantId" size="small" round :bordered="false" :type="currentGroupText === '未分组' ? 'default' : 'success'">
              {{ currentGroupText }}
            </n-tag>
            <n-text v-else depth="3" class="gp-hint">先在上表点一个商户</n-text>
            <span v-if="assignMerchantId" class="mono muted gp-hint">{{ assignMerchantId }}</span>
          </n-space>
        </n-form-item>
        <n-form-item label="目标用户组">
          <n-select
            v-model:value="assignGroupId"
            :options="assignGroupOptions"
            placeholder="选择要归入的用户组"
            style="width: 100%; max-width: 320px"
          />
        </n-form-item>
      </n-form>

      <n-text v-if="assignResult" depth="3" class="gp-hint">{{ assignResult }}</n-text>

      <template #footer>
        <n-space justify="space-between" align="center" :wrap="true">
          <n-space :size="6" align="center" :wrap="true">
            <n-icon :component="PeopleOutline" />
            <n-text depth="3" class="gp-hint">
              {{ assignMerchantId ? `已选商户 ${assignMerchantId}` : '未选择商户' }}
            </n-text>
          </n-space>
          <n-space :wrap="true">
            <n-button @click="showAssign = false">取消</n-button>
            <n-button
              type="error"
              secondary
              :loading="assignSubmitting"
              :disabled="assignMerchantId === ''"
              @click="submitAssign(true)"
            >
              移出用户组
            </n-button>
            <n-button
              type="primary"
              :loading="assignSubmitting"
              :disabled="assignMerchantId === '' || assignGroupId === null"
              @click="submitAssign(false)"
            >
              确认归组
            </n-button>
          </n-space>
        </n-space>
      </template>
    </n-modal>
  </admin-page>
</template>
