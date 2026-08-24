<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import { track } from '../analytics/track'
import { ApiError } from '../api/client'
import * as walletApi from '../api/wallet'
import type { TipRecord, WalletSummary } from '../api/wallet'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import UserAvatar from './UserAvatar.vue'

const props = defineProps<{
  videoId: number
  authorUsername?: string
  isAuthor?: boolean
}>()

const emit = defineEmits<{
  tipped: [coins: number]
}>()

const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()

const presets = [
  { yuan: 1, coins: 10 },
  { yuan: 2, coins: 20 },
  { yuan: 5, coins: 50 },
  { yuan: 10, coins: 100 },
]

const mode = ref<'give' | 'inbox' | ''>('')
const busy = ref(false)
const custom = ref('')
const selected = ref(10)
const summary = ref<WalletSummary | null>(null)
const inbox = reactive({
  loading: false,
  items: [] as TipRecord[],
})

const open = computed(() => mode.value !== '')

/**
 * 不用 type=number + min=10：浏览器会在输入「1」「5」时把值清掉，
 * 15、50 这种自定义金额永远输不进去。
 */
function parseCustomCoins(raw: string) {
  const text = raw.trim()
  if (text === '') return null
  if (!/^[0-9]+$/.test(text)) return null
  const n = Number(text)
  if (!Number.isSafeInteger(n)) return null
  return n
}

const customCoins = computed(() => parseCustomCoins(custom.value))
const customError = computed(() => {
  if (custom.value.trim() === '') return ''
  if (customCoins.value === null) return '请输入整数积分'
  if (customCoins.value < 10) return '最少打赏 10 积分'
  return ''
})
const selectedCoins = computed(() => customCoins.value ?? selected.value)
const authorGets = computed(() => {
  const coins = selectedCoins.value
  if (!Number.isInteger(coins) || coins < 10) return 0
  return Math.floor((coins * 70) / 100)
})
const inboxTotal = computed(() => inbox.items.reduce((sum, item) => sum + item.coins, 0))

async function needLogin() {
  toast.error('请先登录')
  await router.push('/account')
}

async function loadSummary() {
  try {
    const res = await walletApi.summary()
    summary.value = res.summary
  } catch {
    summary.value = null
  }
}

async function openGive() {
  if (!auth.isLoggedIn) {
    await needLogin()
    return
  }
  custom.value = ''
  selected.value = 10
  mode.value = 'give'
  void loadSummary()
}

async function openInbox() {
  if (!auth.isLoggedIn) {
    await needLogin()
    return
  }
  mode.value = 'inbox'
  inbox.loading = true
  try {
    const res = await walletApi.listVideoTips(props.videoId)
    inbox.items = res.tips ?? []
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
    inbox.items = []
  } finally {
    inbox.loading = false
  }
}

function close() {
  mode.value = ''
}

function pickPreset(coins: number) {
  selected.value = coins
  custom.value = ''
}

function onCustomInput(event: Event) {
  const el = event.target as HTMLInputElement
  custom.value = el.value.replace(/\D/g, '')
}

function formatTime(raw: string) {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  return d.toLocaleString()
}

async function confirm() {
  if (customError.value) {
    toast.error(customError.value)
    return
  }
  const coins = selectedCoins.value
  if (!Number.isInteger(coins) || coins < 10) {
    toast.error('最少打赏 10 积分')
    return
  }
  if (busy.value) return
  busy.value = true
  try {
    await walletApi.tip(props.videoId, coins)
    track('wallet_tip', { video_id: props.videoId, coins })
    toast.success(`已打赏 ${coins} 积分`)
    emit('tipped', coins)
    close()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

defineExpose({ openGive, openInbox, close })
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="mask" @click.self="close">
      <div class="sheet" role="dialog" aria-modal="true">
        <div class="head">
          <div>
            <p class="title">{{ mode === 'inbox' ? (props.isAuthor ? '本视频打赏' : '我的打赏') : '打赏' }}</p>
            <p v-if="mode === 'give' && authorUsername" class="subtle">送给 {{ authorUsername }}</p>
            <p v-else-if="mode === 'inbox'" class="subtle">
              {{ inbox.items.length }} 笔 · 共 {{ inboxTotal }} 积分
            </p>
          </div>
          <button class="x" type="button" aria-label="关闭" @click="close">×</button>
        </div>

        <template v-if="mode === 'give'">
          <p v-if="summary" class="balance">可用 {{ summary.available_coins }} 积分</p>
          <div class="presets">
            <button
              v-for="preset in presets"
              :key="preset.coins"
              class="preset"
              type="button"
              :class="{ on: custom === '' && selected === preset.coins }"
              :disabled="busy"
              @click="pickPreset(preset.coins)"
            >
              <div class="preset-yuan">{{ preset.yuan }} 元</div>
              <div class="preset-coin">{{ preset.coins }} 积分</div>
            </button>
          </div>
          <label class="custom-lab" for="tip-custom">自定义积分</label>
          <input
            id="tip-custom"
            class="custom"
            type="text"
            inputmode="numeric"
            autocomplete="off"
            placeholder="最少 10 积分"
            :value="custom"
            :disabled="busy"
            @input="onCustomInput"
            @keydown.stop
          />
          <p v-if="customError" class="subtle bad">{{ customError }}</p>
          <p v-else class="subtle">作者实收 {{ authorGets }} 积分，优先扣除快过期的积分。</p>
          <button class="primary confirm" type="button" :disabled="busy || !!customError" @click="confirm">确认打赏</button>
        </template>

        <template v-else>
          <div v-if="inbox.loading" class="empty">加载中…</div>
          <div v-else-if="inbox.items.length === 0" class="empty">
            {{ props.isAuthor ? '还没有人打赏这支视频' : '你还没有打赏过这支视频' }}
          </div>
          <div v-for="item in inbox.items" :key="item.id" class="row">
            <UserAvatar :username="item.from_username" :id="item.from_account_id" :size="40" />
            <div class="row-meta">
              <div class="row-name">{{ item.from_username }}</div>
              <div class="subtle">{{ formatTime(item.created_at) }}</div>
            </div>
            <div class="row-amt">{{ item.coins }} 积分</div>
          </div>
        </template>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 140;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgba(0, 0, 0, 0.58);
  backdrop-filter: blur(10px);
}

.sheet {
  width: min(420px, 100%);
  max-height: min(80vh, 720px);
  overflow: auto;
  padding: 18px;
  border-radius: 20px;
  border: 1px solid rgba(var(--fg), 0.12);
  background: var(--surface);
  display: grid;
  gap: 12px;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.title {
  margin: 0;
  font-size: 18px;
  font-weight: 800;
}

.subtle {
  margin: 4px 0 0;
  color: rgba(var(--fg), 0.62);
  font-size: 13px;
}

.subtle.bad {
  color: rgba(254, 44, 85, 0.92);
}

.x {
  width: 34px;
  height: 34px;
  border-radius: 12px;
  border: 1px solid rgba(var(--fg), 0.14);
  background: rgba(var(--fg), 0.06);
  color: rgba(var(--fg), 0.9);
  cursor: pointer;
  font-size: 20px;
  line-height: 1;
}

.balance {
  margin: 0;
  font-weight: 700;
}

.presets {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.preset {
  text-align: left;
  padding: 12px;
  border-radius: 14px;
  border: 1px solid rgba(var(--fg), 0.12);
  background: rgba(var(--fg), 0.05);
  color: inherit;
  cursor: pointer;
}

.preset.on {
  border-color: rgba(254, 44, 85, 0.7);
  background: rgba(254, 44, 85, 0.16);
}

.preset-yuan {
  font-weight: 800;
}

.preset-coin {
  margin-top: 4px;
  font-size: 13px;
  color: rgba(var(--fg), 0.64);
}

.custom-lab {
  font-size: 13px;
  color: rgba(var(--fg), 0.7);
}

.custom {
  width: 100%;
}

.confirm {
  width: 100%;
}

.empty {
  padding: 28px 8px;
  text-align: center;
  color: rgba(var(--fg), 0.62);
}

.row {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: 10px;
  align-items: center;
  padding: 10px;
  border-radius: 14px;
  background: rgba(var(--fg), 0.05);
}

.row-name {
  font-weight: 800;
}

.row-amt {
  font-weight: 800;
  color: #fbbf24;
}
</style>
