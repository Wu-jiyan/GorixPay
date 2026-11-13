<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import { merchantApi } from '@/api';
import type { Credential, V2Credential } from '@/api/types';
import { errorMessage } from '@/api/client';
import PageHeader from '@/components/PageHeader.vue';
import { copyText, formatDateTime } from '@/utils/format';

const message = useMessage();

const md5Loading = ref(true);
const rotatingMd5 = ref(false);
const md5Credential = ref<Credential | null>(null);

const v2Loading = ref(true);
const rotatingV2 = ref(false);
const v2Enabled = ref(true);
const v2 = ref<V2Credential | null>(null);

const secretModalVisible = ref(false);
const secretValue = ref('');
const secretKeyId = ref('');

const privateModalVisible = ref(false);
const privateKey = ref('');
const privateKeyId = ref('');

async function loadMd5Credential() {
  md5Loading.value = true;
  try {
    const list = await merchantApi.credentials();
    md5Credential.value =
      list.find((item) => item.credential_type === 'md5' && !item.revoked_at) ?? null;
  } catch {
    // 密钥列表不可用时仅隐藏 key_id，不影响轮换操作
    md5Credential.value = null;
  } finally {
    md5Loading.value = false;
  }
}

async function loadV2Credential() {
  v2Loading.value = true;
  try {
    v2.value = await merchantApi.v2Credential();
    v2Enabled.value = true;
  } catch {
    v2.value = null;
    v2Enabled.value = false;
  } finally {
    v2Loading.value = false;
  }
}

async function rotateMD5() {
  if (rotatingMd5.value) return;
  rotatingMd5.value = true;
  try {
    const res = await merchantApi.rotateMD5();
    secretValue.value = res.secret;
    secretKeyId.value = res.key_id ?? '-';
    secretModalVisible.value = true;
    message.success('MD5 密钥已轮换，旧密钥立即失效');
    await loadMd5Credential();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    rotatingMd5.value = false;
  }
}

async function rotateV2() {
  if (rotatingV2.value) return;
  rotatingV2.value = true;
  try {
    const res = await merchantApi.rotateV2();
    privateKey.value = res.private_key;
    privateKeyId.value = res.key_id ?? '-';
    v2.value = {
      key_id: res.key_id,
      public_key: res.public_key,
      created_at: res.created_at,
      platform_public_key: res.platform_public_key,
    };
    v2Enabled.value = true;
    privateModalVisible.value = true;
    message.success('V2 密钥已生成，请立即保存私钥');
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    rotatingV2.value = false;
  }
}

async function handleCopy(text: string, label = '内容') {
  if (!text) return;
  const ok = await copyText(text);
  if (ok) message.success(`${label}已复制`);
  else message.error('复制失败，请手动选择文本复制');
}

function refreshAll() {
  void loadMd5Credential();
  void loadV2Credential();
}

onMounted(() => {
  void loadMd5Credential();
  void loadV2Credential();
});
</script>

<template>
  <div class="page">
    <PageHeader title="密钥管理" subtitle="V1 MD5 密钥与 V2 RSA 密钥，轮换后请立即更新到服务端">
      <template #actions>
        <n-button size="small" @click="refreshAll">刷新</n-button>
      </template>
    </PageHeader>

    <div class="credential-grid">
      <n-card :bordered="false" class="credential-card" title="MD5 密钥（V1 协议）">
        <template #header-extra>
          <n-tag size="small" :bordered="false" type="info">MD5</n-tag>
        </template>

        <p class="credential-desc">
          V1 接口（<code>submit.php</code>、<code>mapi.php</code>、<code>api.php</code>）使用商户 MD5 密钥签名：
          待签名串末尾直接拼接该密钥后取 MD5 小写。轮换后旧密钥立即失效，请先灰度更新服务端配置。
        </p>

        <n-spin :show="md5Loading">
          <n-descriptions :column="1" label-placement="left" size="small" class="credential-meta">
            <n-descriptions-item label="密钥 ID">
              <span class="mono">{{ md5Credential?.key_id || '未查询到（可先轮换生成）' }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="状态">
              <n-tag v-if="md5Credential" size="small" :bordered="false" type="success">
                {{ md5Credential.status }}
              </n-tag>
              <span v-else class="muted">-</span>
            </n-descriptions-item>
            <n-descriptions-item label="创建时间">
              {{ formatDateTime(md5Credential?.created_at) }}
            </n-descriptions-item>
          </n-descriptions>
        </n-spin>

        <n-alert type="warning" :bordered="false" class="credential-alert">
          密钥仅在轮换时展示一次，平台不保存明文。请立即写入服务端配置或密钥管理系统。
        </n-alert>

        <div class="credential-actions">
          <n-popconfirm @positive-click="rotateMD5">
            <template #trigger>
              <n-button type="primary" :loading="rotatingMd5">轮换密钥</n-button>
            </template>
            轮换后旧密钥立即失效，使用旧密钥的请求会验签失败。确认继续？
          </n-popconfirm>
        </div>
      </n-card>

      <n-card :bordered="false" class="credential-card" title="V2 RSA 密钥">
        <template #header-extra>
          <n-tag size="small" :bordered="false" :type="v2Enabled ? 'success' : 'default'">
            {{ v2Enabled ? 'RSA-SHA256' : '未启用' }}
          </n-tag>
        </template>

        <p class="credential-desc">
          V2 协议使用 RSA-SHA256 双向签名：商户用<strong>私钥</strong>签请求，平台用商户公钥验签；平台用
          <strong>平台私钥</strong>签响应与通知，商户用下方「平台公钥」验签。
        </p>

        <n-spin :show="v2Loading">
          <n-alert v-if="!v2Enabled" type="default" :bordered="false" class="credential-alert">
            V2 密钥未启用。点击「生成 / 轮换密钥」创建 RSA 密钥对，私钥只会展示一次。
          </n-alert>

          <template v-else>
            <n-descriptions :column="1" label-placement="left" size="small" class="credential-meta">
              <n-descriptions-item label="密钥 ID">
                <span class="mono">{{ v2?.key_id || '-' }}</span>
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">
                {{ formatDateTime(v2?.created_at) }}
              </n-descriptions-item>
            </n-descriptions>

            <div class="key-block">
              <div class="key-head">
                <span class="key-label">商户公钥（PEM）</span>
                <n-button text type="primary" size="tiny" @click="handleCopy(v2?.public_key ?? '', '商户公钥')">
                  复制
                </n-button>
              </div>
              <pre class="key-pre">{{ v2?.public_key || '暂无商户公钥，请先生成密钥' }}</pre>
            </div>

            <div class="key-block">
              <div class="key-head">
                <span class="key-label">平台公钥（PEM）</span>
                <n-button
                  text
                  type="primary"
                  size="tiny"
                  @click="handleCopy(v2?.platform_public_key ?? '', '平台公钥')"
                >
                  复制
                </n-button>
              </div>
              <pre class="key-pre">{{ v2?.platform_public_key }}</pre>
            </div>
          </template>
        </n-spin>

        <n-alert type="error" :bordered="false" class="credential-alert">
          私钥只在生成或轮换时展示一次，平台无法再次查看。丢失私钥只能重新生成并更新服务端配置。
        </n-alert>

        <div class="credential-actions">
          <n-popconfirm @positive-click="rotateV2">
            <template #trigger>
              <n-button type="primary" :loading="rotatingV2">
                {{ v2Enabled ? '轮换密钥' : '生成密钥' }}
              </n-button>
            </template>
            将生成新的 RSA 密钥对，旧私钥立即失效并停止验签。确认继续？
          </n-popconfirm>
        </div>
      </n-card>
    </div>

    <n-modal v-model:show="secretModalVisible" preset="card" title="新的 MD5 密钥" style="max-width: 520px">
      <n-alert type="warning" :bordered="false" class="credential-alert">
        该密钥只显示一次，关闭后无法再次查看。请立即复制并更新到服务端配置。
      </n-alert>
      <div class="key-block">
        <div class="key-head">
          <span class="key-label">密钥 ID：{{ secretKeyId }}</span>
          <n-button text type="primary" size="tiny" @click="handleCopy(secretValue, 'MD5 密钥')">复制</n-button>
        </div>
        <pre class="key-pre mono">{{ secretValue }}</pre>
      </div>
      <template #footer>
        <div class="modal-footer">
          <n-button type="primary" @click="secretModalVisible = false">我已保存，关闭</n-button>
        </div>
      </template>
    </n-modal>

    <n-modal
      v-model:show="privateModalVisible"
      preset="card"
      title="V2 商户私钥（只显示一次）"
      style="max-width: 560px"
    >
      <n-alert type="error" :bordered="false" class="credential-alert">
        私钥绝不会再次展示，请立即复制并保存到密钥管理系统。关闭后如未保存，只能重新轮换密钥。
      </n-alert>
      <div class="key-block">
        <div class="key-head">
          <span class="key-label">密钥 ID：{{ privateKeyId }}</span>
          <n-button text type="primary" size="tiny" @click="handleCopy(privateKey, '私钥')">复制私钥</n-button>
        </div>
        <pre class="key-pre mono">{{ privateKey }}</pre>
      </div>
      <template #footer>
        <div class="modal-footer">
          <n-button type="primary" @click="privateModalVisible = false">我已保存，关闭</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.credential-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  align-items: start;
}
.credential-card {
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.credential-desc {
  margin: 0 0 16px;
  font-size: 13px;
  color: #64748b;
  line-height: 1.85;
}
.credential-desc code {
  background: #f1f5f9;
  padding: 1px 6px;
  border-radius: 5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: #1d4ed8;
}
.credential-meta {
  margin-bottom: 8px;
}
.credential-alert {
  margin: 10px 0 14px;
}
.credential-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.key-block {
  margin-bottom: 14px;
}
.key-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 6px;
}
.key-label {
  font-size: 12.5px;
  color: #475569;
  font-weight: 600;
}
.key-pre {
  margin: 0;
  background: #0f172a;
  color: #dbeafe;
  border-radius: 10px;
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11.5px;
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow: auto;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
}
.muted {
  color: #94a3b8;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
@media (max-width: 1100px) {
  .credential-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
