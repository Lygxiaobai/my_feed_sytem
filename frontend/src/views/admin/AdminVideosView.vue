<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../../api/client'
import * as adminApi from '../../api/admin'
import type { AdminVideo, AdminVideoBoard } from '../../api/admin'
import { AUDIT_STATUS_LABEL } from '../../api/admin'
import { useToastStore } from '../../stores/toast'
import VideoTags from '../../components/VideoTags.vue'

const emit = defineEmits<{ changed: [] }>()
const route = useRoute()
const router = useRouter()
const toast = useToastStore()

const query = ref('')
const status = ref('')
const loading = ref(false)
const takingDown = ref(false)
const board = ref<AdminVideoBoard | null>(null)
const video = ref<AdminVideo | null>(null)
const note = ref('')

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN')
}

async function load(reset = true) {
  loading.value = true
  try {
    const offset = reset ? 0 : board.value?.videos.length || 0
    const next = await adminApi.listAdminVideos({
      query: query.value.trim() || undefined,
      audit_status: status.value || undefined,
      limit: 20,
      offset,
    })
    if (reset || !board.value) {
      board.value = next
    } else {
      board.value = {
        ...next,
        videos: [...board.value.videos, ...next.videos],
      }
    }
    await router.replace({
      path: '/admin/videos',
      query: {
        ...(query.value.trim() ? { q: query.value.trim() } : {}),
        ...(status.value ? { status: status.value } : {}),
        ...(route.query.id ? { id: String(route.query.id) } : {}),
      },
    })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载视频失败')
  } finally {
    loading.value = false
  }
}

async function openVideo(id: number) {
  loading.value = true
  try {
    video.value = await adminApi.lookupAdminVideo(id)
    query.value = query.value.trim() || String(id)
    await router.replace({
      path: '/admin/videos',
      query: {
        ...(query.value.trim() ? { q: query.value.trim() } : {}),
        ...(status.value ? { status: status.value } : {}),
        id: String(id),
      },
    })
  } catch (e) {
    video.value = null
    toast.error(e instanceof ApiError ? e.message : '查询失败')
  } finally {
    loading.value = false
  }
}

async function lookup() {
  const text = query.value.trim()
  if (/^\d+$/.test(text)) {
    await openVideo(Number(text))
    return
  }
  video.value = null
  await load()
}

async function takedown() {
  if (!video.value || takingDown.value) return
  const reason = note.value.trim()
  if (!reason) {
    toast.error('下架必须填写处置说明')
    return
  }
  if (!window.confirm(`确认下架「${video.value.title}」？公开信息流将不再展示该内容。`)) return

  takingDown.value = true
  try {
    await adminApi.takedownAdminVideo(video.value.id, reason)
    toast.success('已下架')
    note.value = ''
    emit('changed')
    await openVideo(video.value.id)
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '下架失败')
  } finally {
    takingDown.value = false
  }
}

onMounted(async () => {
  query.value = String(route.query.q || route.query.id || '')
  status.value = String(route.query.status || '')
  await load()
  const id = Number(route.query.id || 0)
  if (Number.isInteger(id) && id > 0) {
    await openVideo(id)
  }
})
</script>

<template>
  <div>
    <p class="title">视频看板</p>
    <p class="subtle">列出全部作品，含未过审和下架。点开一条才能播放或下架。未过审内容对普通用户仍显示为不存在。</p>

    <div v-if="board" class="cards">
      <div class="card metric">
        <div class="metric-num">{{ board.summary.total }}</div>
        <div class="metric-label">全部</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.approved }}</div>
        <div class="metric-label">公开</div>
      </div>
      <div class="card metric">
        <div class="metric-num">{{ board.summary.rejected }}</div>
        <div class="metric-label">已下架 · 待审 {{ board.summary.pending }}</div>
      </div>
    </div>

    <div class="row search">
      <input v-model.trim="query" placeholder="视频 ID / 标题 / 作者 / 标签" @keydown.enter="lookup()" />
      <select v-model="status" @change="load()">
        <option value="">全部状态</option>
        <option value="approved">公开</option>
        <option value="pending">待审</option>
        <option value="reviewing">复审中</option>
        <option value="rejected">已下架</option>
      </select>
      <button class="primary" type="button" :disabled="loading" @click="lookup()">查询</button>
    </div>

    <div v-if="video" class="card detail">
      <div class="head">
        <img v-if="video.cover_url" :src="video.cover_url" alt="" />
        <div>
          <h2>{{ video.title }}</h2>
          <p class="subtle">
            #{{ video.id }} · {{ video.username }} · {{ AUDIT_STATUS_LABEL[video.audit_status] }}
            · 待处理举报 {{ video.pending_reports }}
          </p>
          <p class="subtle">{{ formatTime(video.created_at) }} · 赞 {{ video.likes_count }} · 评 {{ video.comment_count }}</p>
          <p v-if="video.description">{{ video.description }}</p>
          <VideoTags :tags="video.tags" />
          <div class="row">
            <button class="ghost" type="button" @click="router.push({ path: '/admin/users', query: { id: String(video.author_id) } })">
              查看作者
            </button>
            <a class="ghost" :href="`/video/${video.id}`" target="_blank" rel="noreferrer">打开前台页</a>
          </div>
        </div>
      </div>

      <video v-if="video.play_url" :src="video.play_url" :poster="video.cover_url" controls playsinline />

      <template v-if="video.audit_status !== 'rejected'">
        <label>下架说明</label>
        <textarea v-model="note" :maxlength="adminApi.ADMIN_NOTE_MAX" placeholder="写明依据，会记入审核流水" />
        <button class="danger" type="button" :disabled="takingDown" @click="takedown">确认下架</button>
      </template>
      <p v-else class="subtle">该内容已下架。如需补充依据，可再次查询后联系留存流水。</p>
    </div>

    <div v-if="loading && !board" class="subtle">加载中…</div>
    <div v-else-if="board && board.videos.length === 0" class="card">
      <p class="subtle" style="margin: 0">没有匹配的视频。</p>
    </div>
    <div v-else-if="board" class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>作品</th>
            <th>标签</th>
            <th>作者</th>
            <th>状态</th>
            <th>互动</th>
            <th>时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in board.videos" :key="item.id">
            <td>
              <button class="work" type="button" @click="openVideo(item.id)">
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
            <td>
              <button class="link" type="button" @click="router.push({ path: '/admin/users', query: { id: String(item.author_id) } })">
                {{ item.username }}
              </button>
            </td>
            <td>{{ AUDIT_STATUS_LABEL[item.audit_status] }}</td>
            <td>赞 {{ item.likes_count }} · 评 {{ item.comment_count }}</td>
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
  margin: 16px 0;
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
  margin: 0 0 16px;
}

.search input,
.search select {
  max-width: 220px;
}

.detail {
  display: grid;
  gap: 14px;
  margin-bottom: 16px;
}

.head {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  gap: 14px;
}

.head img {
  width: 160px;
  height: 120px;
  object-fit: cover;
  border-radius: 12px;
  background: var(--fill);
}

.head h2 {
  margin: 0 0 6px;
  font-size: 18px;
}

video {
  width: 100%;
  max-height: 420px;
  border-radius: 12px;
  background: #000;
}

.ghost {
  display: inline-flex;
  align-items: center;
  text-decoration: none;
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
  vertical-align: middle;
}

.work {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 8px;
  align-items: center;
  padding: 0;
  min-height: 0;
  border: 0;
  background: transparent;
  text-align: left;
}

.work img {
  width: 72px;
  height: 54px;
  object-fit: cover;
  border-radius: 8px;
  background: var(--fill);
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
  .cards,
  .head {
    grid-template-columns: 1fr;
  }

  .head img {
    width: 100%;
    height: 160px;
  }
}
</style>
