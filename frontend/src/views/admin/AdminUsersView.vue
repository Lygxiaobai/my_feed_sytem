<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../../api/client'
import * as adminApi from '../../api/admin'
import type { AdminAccount, AdminAccountBoard, AdminVideo } from '../../api/admin'
import { AUDIT_STATUS_LABEL } from '../../api/admin'
import { useToastStore } from '../../stores/toast'
import VideoTags from '../../components/VideoTags.vue'

const route = useRoute()
const router = useRouter()
const toast = useToastStore()

const query = ref('')
const loading = ref(false)
const board = ref<AdminAccountBoard | null>(null)
const account = ref<AdminAccount | null>(null)
const videos = ref<AdminVideo[]>([])

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN')
}

async function load(reset = true) {
  loading.value = true
  try {
    const offset = reset ? 0 : board.value?.accounts.length || 0
    const next = await adminApi.listAdminAccounts({
      query: query.value.trim() || undefined,
      limit: 20,
      offset,
    })
    if (reset || !board.value) {
      board.value = next
    } else {
      board.value = {
        ...next,
        accounts: [...board.value.accounts, ...next.accounts],
      }
    }
    await router.replace({
      path: '/admin/users',
      query: {
        ...(query.value.trim() ? { q: query.value.trim() } : {}),
        ...(route.query.id || route.query.username || route.query.email
          ? {
              ...(route.query.id ? { id: String(route.query.id) } : {}),
              ...(route.query.username ? { username: String(route.query.username) } : {}),
              ...(route.query.email ? { email: String(route.query.email) } : {}),
            }
          : {}),
      },
    })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载用户失败')
  } finally {
    loading.value = false
  }
}

function parseQuery(raw: string) {
  const text = raw.trim()
  if (!text) return null
  if (text.includes('@')) return { email: text.toLowerCase() }
  if (/^\d+$/.test(text)) return { id: Number(text) }
  return { username: text }
}

async function openAccount(input: { id?: number; username?: string; email?: string }) {
  loading.value = true
  try {
    const result = await adminApi.lookupAdminAccount(input)
    account.value = result.account
    videos.value = result.videos
    await router.replace({
      path: '/admin/users',
      query: {
        ...(query.value.trim() ? { q: query.value.trim() } : {}),
        ...(input.id
          ? { id: String(input.id) }
          : input.username
            ? { username: input.username }
            : { email: input.email || '' }),
      },
    })
  } catch (e) {
    account.value = null
    videos.value = []
    toast.error(e instanceof ApiError ? e.message : '查询失败')
  } finally {
    loading.value = false
  }
}

async function lookup() {
  const parsed = parseQuery(query.value)
  if (parsed) {
    await openAccount(parsed)
  }
  await load()
}

onMounted(async () => {
  const id = String(route.query.id || '')
  const username = String(route.query.username || '')
  const email = String(route.query.email || '')
  query.value = String(route.query.q || id || username || email)
  await load()
  if (id || username || email) {
    await openAccount(id ? { id: Number(id) } : username ? { username } : { email })
  }
})
</script>

<template>
  <div>
    <p class="title">用户看板</p>
    <p class="subtle">默认列出全部账号。兴趣标签存在账号字段里，点赞、打赏、评论会写入，最多 7 个。邮箱只在管理后台展示。</p>

    <div v-if="board" class="cards">
      <div class="card metric">
        <div class="metric-num">{{ board.summary.total }}</div>
        <div class="metric-label">全部账号</div>
      </div>
    </div>

    <div class="row search">
      <input v-model.trim="query" placeholder="ID / 用户名 / 邮箱" @keydown.enter="lookup()" />
      <button class="primary" type="button" :disabled="loading" @click="lookup()">查询</button>
    </div>

    <div v-if="account" class="card">
      <h2>{{ account.username }}</h2>
      <p class="subtle">
        #{{ account.id }}
        <template v-if="account.email"> · {{ account.email }}</template>
        · 粉丝 {{ account.follower_count }}
        · 注册 {{ formatTime(account.created_at) }}
      </p>
      <div v-if="account.interest_tags?.length" class="tags">
        <button
          v-for="tag in account.interest_tags"
          :key="tag.label"
          class="pill"
          type="button"
          @click="router.push({ path: '/admin/videos', query: tag.video_id ? { id: String(tag.video_id) } : { q: tag.label } })"
        >
          {{ tag.label }}
        </button>
      </div>
      <p v-else class="subtle">还没有可展示的兴趣标签。</p>
    </div>

    <div v-if="account" class="table-wrap works">
      <table>
        <thead>
          <tr>
            <th>作品</th>
            <th>标签</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in videos" :key="item.id">
            <td>
              <button class="work" type="button" @click="router.push({ path: '/admin/videos', query: { id: String(item.id) } })">
                <img v-if="item.cover_url" :src="item.cover_url" alt="" />
                <div>
                  <strong>{{ item.title }}</strong>
                  <div class="subtle">#{{ item.id }}</div>
                </div>
              </button>
            </td>
            <td>
              <VideoTags v-if="item.tags?.length" :tags="item.tags" />
              <span v-else class="subtle">—</span>
            </td>
            <td>{{ AUDIT_STATUS_LABEL[item.audit_status] }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="videos.length === 0" class="subtle">该账号还没有作品。</p>
    </div>

    <div v-if="loading && !board" class="subtle">加载中…</div>
    <div v-else-if="board && board.accounts.length === 0" class="card">
      <p class="subtle" style="margin: 0">没有匹配的用户。</p>
    </div>
    <div v-else-if="board" class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>账号</th>
            <th>邮箱</th>
            <th>兴趣标签</th>
            <th>粉丝</th>
            <th>注册</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in board.accounts" :key="item.id">
            <td>
              <button class="link" type="button" @click="openAccount({ id: item.id })">
                {{ item.username }}
              </button>
              <div class="subtle">#{{ item.id }}</div>
            </td>
            <td>{{ item.email || '—' }}</td>
            <td>
              <div v-if="item.interest_tags?.length" class="tags">
                <button
                  v-for="tag in item.interest_tags"
                  :key="`${item.id}-${tag.label}`"
                  class="pill"
                  type="button"
                  @click="router.push({ path: '/admin/videos', query: tag.video_id ? { id: String(tag.video_id) } : { q: tag.label } })"
                >
                  {{ tag.label }}
                </button>
              </div>
              <span v-else class="subtle">暂无</span>
            </td>
            <td>{{ item.follower_count }}</td>
            <td>{{ formatTime(item.created_at) }}</td>
          </tr>
        </tbody>
      </table>
      <button v-if="board.has_more" type="button" :disabled="loading" @click="load(false)">加载更多</button>
    </div>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 16px 0 0;
}

.metric-num {
  font-size: 24px;
  font-weight: 800;
}

.metric-label {
  margin-top: 4px;
  color: var(--muted);
  font-size: 13px;
}

.search {
  margin: 16px 0;
}

.search input {
  max-width: 320px;
}

h2 {
  margin: 0 0 6px;
  font-size: 18px;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.pill {
  min-height: 28px;
  padding: 4px 10px;
  font-size: 12px;
}

.works {
  margin: 14px 0;
}

.work {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: 10px;
  padding: 0;
  min-height: 0;
  border: 0;
  background: transparent;
  text-align: left;
  align-items: center;
}

.work img {
  width: 88px;
  height: 66px;
  object-fit: cover;
  border-radius: 10px;
  background: var(--fill);
}

.work strong {
  display: block;
  font-size: 14px;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

th,
td {
  padding: 10px 8px;
  text-align: left;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
}

.link {
  padding: 0;
  min-height: 0;
  border: 0;
  background: transparent;
  color: inherit;
  text-decoration: underline;
}

@media (max-width: 720px) {
  .cards {
    grid-template-columns: 1fr;
  }
}
</style>
