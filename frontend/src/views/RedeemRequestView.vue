<template>
  <AppLayout title="兑换申请" subtitle="审核通过后选择一个启用档位">
    <div v-if="issuedCode" class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">已发放兑换码</div>
          <div class="muted">复制后到 {{ branding.title }} 中使用。</div>
        </div>
        <el-button :icon="CopyDocument" type="primary" @click="copyIssued">复制兑换码</el-button>
      </div>
      <el-alert :title="issuedCode.code" type="success" :closable="false" />
    </div>

    <div class="surface section compact-form" style="margin-bottom: 16px">
      <el-form :model="form" label-position="top" @submit.prevent="submit">
        <el-form-item label="余额档位">
          <el-select v-model="form.tierId" placeholder="请选择档位" style="width: 100%">
            <el-option
              v-for="tier in enabledTiers"
              :key="tier.id"
              :label="formatTierDisplay(tier)"
              :value="tier.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.note" type="textarea" :rows="3" maxlength="500" show-word-limit />
        </el-form-item>
        <el-button type="primary" :icon="Tickets" :loading="loading" native-type="submit">
          提交兑换申请
        </el-button>
      </el-form>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">可选档位</div>
          <div class="muted">仅启用的档位可以选择。</div>
        </div>
        <el-button :icon="Refresh" @click="loadAll">刷新</el-button>
      </div>
      <el-table :data="tiers" stripe size="small" style="width: 100%">
        <el-table-column prop="label" label="标签" width="140">
          <template #default="{ row }">{{ row.label || '-' }}</template>
        </el-table-column>
        <el-table-column prop="amount" label="金额" width="120">
          <template #default="{ row }">{{ Number(row.amount).toFixed(0) }} 美元</template>
        </el-table-column>
        <el-table-column prop="pay_amount_cny" label="实付金额" width="120">
          <template #default="{ row }">{{ Number(row.pay_amount_cny).toFixed(0) }} 人民币</template>
        </el-table-column>
        <el-table-column prop="enabled" label="启用" width="120">
          <template #default="{ row }"><StatusTag :status="row.enabled ? 'enabled' : 'disabled'" /></template>
        </el-table-column>
      </el-table>
    </div>

    <div class="surface section" style="margin-bottom: 16px">
      <div class="toolbar">
        <div>
          <div style="font-weight: 700">兑换申请记录</div>
          <div class="muted">若上游处理失败，修复后可以重新提交。</div>
        </div>
      </div>
      <el-table :data="requests" stripe size="small" style="width: 100%; margin-bottom: 16px">
        <el-table-column prop="id" label="编号" width="80" />
        <el-table-column label="档位" min-width="180">
          <template #default="{ row }">{{ tierNameById(row.tier_id) }}</template>
        </el-table-column>
        <el-table-column label="档位到账金额" width="130">
          <template #default="{ row }">{{ formatTierAmount(tierById(row.tier_id)) }}</template>
        </el-table-column>
        <el-table-column label="档位实付金额" width="130">
          <template #default="{ row }">{{ formatTierPayAmount(tierById(row.tier_id)) }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }"><StatusTag :status="row.status" /></template>
        </el-table-column>
        <el-table-column prop="value" label="到账金额" width="110">
          <template #default="{ row }">{{ Number(row.value).toFixed(0) }} 美元</template>
        </el-table-column>
        <el-table-column prop="upstream_code" label="兑换码" min-width="220">
          <template #default="{ row }">
            <div class="code-cell">
              <code>{{ row.upstream_code || '-' }}</code>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="error_message" label="错误信息" min-width="200" />
      </el-table>

      <CodeTable :codes="codes" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { CopyDocument, Refresh, Tickets } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import CodeTable from '@/components/CodeTable.vue'
import StatusTag from '@/components/StatusTag.vue'
import { createRedeemRequest, listBalanceTiers, listRedeemCodes, listRedeemRequests } from '@/api/redeem'
import type { BalanceTier, RedeemCode, RedeemRequest } from '@/api/types'
import { formatTierAmount, formatTierDisplay, formatTierPayAmount } from '@/utils/tiers'
import { copyText } from '@/utils/clipboard'
import { useBrandingStore } from '@/stores/branding'

const loading = ref(false)
const branding = useBrandingStore()
const tiers = ref<BalanceTier[]>([])
const requests = ref<RedeemRequest[]>([])
const codes = ref<RedeemCode[]>([])
const issuedCode = ref<RedeemCode | null>(null)

const form = reactive({
  tierId: 0,
  note: '',
})

const enabledTiers = computed(() => tiers.value.filter((tier) => tier.enabled))
const tierMap = computed(() => new Map(tiers.value.map((tier) => [tier.id, tier])))

async function loadAll() {
  try {
    const [tierData, requestData, codeData] = await Promise.all([
      listBalanceTiers(),
      listRedeemRequests(),
      listRedeemCodes(),
    ])
    tiers.value = tierData
    requests.value = requestData
    codes.value = codeData
    if (!form.tierId && enabledTiers.value[0]) {
      form.tierId = enabledTiers.value[0].id
    }
  } catch (error: any) {
    ElMessage.error(error?.message ?? '加载数据失败')
  }
}

function tierById(id: number) {
  return tierMap.value.get(id)
}

function tierNameById(id: number) {
  const tier = tierById(id)
  if (!tier) return `#${id}`
  return tier.label?.trim() || `#${id}`
}

async function submit() {
  if (!form.tierId) {
    ElMessage.warning('请先选择一个档位')
    return
  }
  loading.value = true
  try {
    const result = await createRedeemRequest(form.tierId, form.note)
    issuedCode.value = result.code ?? null
    ElMessage.success('兑换申请已提交')
    await loadAll()
  } catch (error: any) {
    ElMessage.error(error?.message ?? '提交兑换申请失败')
  } finally {
    loading.value = false
  }
}

async function copyIssued() {
  if (!issuedCode.value) return
  const copied = await copyText(issuedCode.value.code)
  if (copied) {
    ElMessage.success('已复制')
  }
}

onMounted(loadAll)
</script>
