<script setup lang="ts">
import { computed, onMounted, reactive, watch } from 'vue'

import AppShell from '../components/AppShell.vue'
import JsonBox from '../components/JsonBox.vue'
import FeedVideoCard from '../components/FeedVideoCard.vue'
import { ApiError } from '../api/client'
import * as feedApi from '../api/feed'
import * as likeApi from '../api/like'
import type { FeedVideoItem } from '../api/types'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()

type ListState = {
  loading: boolean
  error: string
  items: FeedVideoItem[]
  has_more: boolean
}

const latest = reactive<ListState & { limit: number; next_time: number; next_id: number }>({
  loading: false,
  error: '',
  items: [],
  has_more: false,
  limit: 10,
  next_time: 0,
  next_id: 0,
})

const likesCount = reactive<ListState & { limit: number; next_likes_count_before?: number; next_id_before?: number }>({
  loading: false,
  error: '',
  items: [],
  has_more: false,
  limit: 10,
  next_likes_count_before: undefined,
  next_id_before: undefined,
})

const following = reactive<ListState & { limit: number; next_time: number; next_id: number }>({
  loading: false,
  error: '',
  items: [],
  has_more: false,
  limit: 10,
  next_time: 0,
  next_id: 0,
})

const action = reactive<{ loading: boolean; error: string; payload: unknown; name: string }>({
  loading: false,
  error: '',
  payload: null,
  name: '',
})

const canLike = computed(() => auth.isLoggedIn)

async function syncLikedState(items: FeedVideoItem[]) {
  if (!items.length) return
  if (!auth.isLoggedIn) {
    items.forEach((item) => {
      item.is_liked = false
    })
    return
  }

  try {
    const likedIds = await likeApi.listLikedVideoIds(items.map((item) => item.id))
    const likedSet = new Set(likedIds)
    items.forEach((item) => {
      item.is_liked = likedSet.has(item.id)
    })
  } catch {
    items.forEach((item) => {
      item.is_liked = false
    })
  }
}

async function runAction(name: string, fn: () => Promise<unknown>) {
  action.name = name
  action.loading = true
  action.error = ''
  action.payload = null
  try {
    action.payload = await fn()
  } catch (e) {
    action.error = e instanceof ApiError ? e.message : String(e)
    action.payload = e instanceof ApiError ? e.payload : null
  } finally {
    action.loading = false
  }
}

async function loadLatest(reset: boolean) {
  latest.loading = true
  latest.error = ''
  try {
    const latest_time = reset ? 0 : latest.next_time
    const last_id = reset ? 0 : latest.next_id
    const res = await feedApi.listLatest({ limit: latest.limit, latest_time, last_id })
    latest.has_more = res.has_more
    latest.next_time = res.next_time
    latest.next_id = res.next_id
    latest.items = reset ? res.video_list : latest.items.concat(res.video_list)
    await syncLikedState(latest.items)
  } catch (e) {
    latest.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    latest.loading = false
  }
}

async function loadLikesCount(reset: boolean) {
  likesCount.loading = true
  likesCount.error = ''
  try {
    const res = await feedApi.listLikesCount({
      limit: likesCount.limit,
      likes_count_before: reset ? undefined : likesCount.next_likes_count_before,
      id_before: reset ? undefined : likesCount.next_id_before,
    })
    likesCount.has_more = res.has_more
    likesCount.next_likes_count_before = res.next_likes_count_before
    likesCount.next_id_before = res.next_id_before
    likesCount.items = reset ? res.video_list : likesCount.items.concat(res.video_list)
    await syncLikedState(likesCount.items)
  } catch (e) {
    likesCount.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    likesCount.loading = false
  }
}

async function loadFollowing(reset: boolean) {
  following.loading = true
  following.error = ''
  try {
    const latest_time = reset ? 0 : following.next_time
    const last_id = reset ? 0 : following.next_id
    const res = await feedApi.listByFollowing({ limit: following.limit, latest_time, last_id })
    following.has_more = res.has_more
    following.next_time = res.next_time
    following.next_id = res.next_id
    following.items = reset ? res.video_list : following.items.concat(res.video_list)
    await syncLikedState(following.items)
  } catch (e) {
    following.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    following.loading = false
  }
}

async function toggleLike(item: FeedVideoItem) {
  if (!auth.isLoggedIn) return

  await runAction(item.is_liked ? '\u53D6\u6D88\u70B9\u8D5E' : '\u70B9\u8D5E', async () => {
    if (item.is_liked) await likeApi.unlike(item.id)
    else await likeApi.like(item.id)

    item.is_liked = !item.is_liked
    item.likes_count = Math.max(0, item.likes_count + (item.is_liked ? 1 : -1))
    return { ok: true, is_liked: item.is_liked, likes_count: item.likes_count }
  })
}

onMounted(async () => {
  await loadLatest(true)
  await loadLikesCount(true)
  if (auth.isLoggedIn) {
    await loadFollowing(true)
  }
})

watch(
  () => auth.isLoggedIn,
  async (v) => {
    if (v && following.items.length === 0) {
      await loadFollowing(true)
    }
    if (!v) {
      await syncLikedState(latest.items)
      await syncLikedState(likesCount.items)
      await syncLikedState(following.items)
    }
  },
)
</script>

<template>
  <AppShell>
    <div class="grid two">
      <div class="card">
        <p class="title">Feed</p>
        <p class="subtle">`/feed/listLatest` &#x4E0E; `/feed/listLikesCount` &#x652F;&#x6301;&#x533F;&#x540D;&#x8BBF;&#x95EE;&#xFF08;&#x53EF;&#x9009; JWT&#xFF09;&#xFF1B; `/feed/listByFollowing` &#x9700;&#x8981; JWT&#x3002;</p>

        <div class="card" style="margin-top: 12px">
          <div class="row" style="justify-content: space-between">
            <div>
              <p class="title">&#x63A8;&#x8350;&#x6D41;&#xFF08;listLatest&#xFF09;</p>
              <div class="subtle">limit={{ latest.limit }} &#x00B7; next_time={{ latest.next_time }} &#x00B7; has_more={{ latest.has_more }}</div>
            </div>
            <div class="row">
              <label class="subtle" style="margin: 0">limit</label>
              <input v-model.number="latest.limit" type="number" min="1" max="50" style="width: 90px" />
              <button class="primary" type="button" :disabled="latest.loading" @click="loadLatest(true)">&#x5237;&#x65B0;</button>
              <button type="button" :disabled="latest.loading || !latest.has_more" @click="loadLatest(false)">&#x52A0;&#x8F7D;&#x66F4;&#x591A;</button>
            </div>
          </div>
          <div v-if="latest.error" class="pill bad" style="margin-top: 10px">&#x9519;&#x8BEF;&#xFF1A;{{ latest.error }}</div>
          <div class="grid" style="gap: 10px; margin-top: 12px">
            <FeedVideoCard
              v-for="item in latest.items"
              :key="`latest-${item.id}`"
              :item="item"
              :can-like="canLike"
              :busy="action.loading"
              @toggle-like="toggleLike"
            />
          </div>
        </div>

        <div class="card" style="margin-top: 12px">
          <div class="row" style="justify-content: space-between">
            <div>
              <p class="title">&#x70B9;&#x8D5E;&#x699C;&#xFF08;listLikesCount&#xFF09;</p>
              <div class="subtle">
                limit={{ likesCount.limit }} &#x00B7; next=(likes={{ likesCount.next_likes_count_before }}, id={{ likesCount.next_id_before }})
                &#x00B7; has_more={{ likesCount.has_more }}
              </div>
            </div>
            <div class="row">
              <label class="subtle" style="margin: 0">limit</label>
              <input v-model.number="likesCount.limit" type="number" min="1" max="50" style="width: 90px" />
              <button class="primary" type="button" :disabled="likesCount.loading" @click="loadLikesCount(true)">&#x5237;&#x65B0;</button>
              <button type="button" :disabled="likesCount.loading || !likesCount.has_more" @click="loadLikesCount(false)">&#x52A0;&#x8F7D;&#x66F4;&#x591A;</button>
            </div>
          </div>
          <div v-if="likesCount.error" class="pill bad" style="margin-top: 10px">&#x9519;&#x8BEF;&#xFF1A;{{ likesCount.error }}</div>
          <div class="grid" style="gap: 10px; margin-top: 12px">
            <FeedVideoCard
              v-for="item in likesCount.items"
              :key="`likes-${item.id}`"
              :item="item"
              :can-like="canLike"
              :busy="action.loading"
              @toggle-like="toggleLike"
            />
          </div>
        </div>

        <div class="card" style="margin-top: 12px">
          <div class="row" style="justify-content: space-between">
            <div>
              <p class="title">&#x5173;&#x6CE8;&#x6D41;&#xFF08;listByFollowing&#xFF0C;JWT&#xFF09;</p>
              <div class="subtle">limit={{ following.limit }} &#x00B7; next_time={{ following.next_time }} &#x00B7; has_more={{ following.has_more }}</div>
            </div>
            <div class="row">
              <label class="subtle" style="margin: 0">limit</label>
              <input v-model.number="following.limit" type="number" min="1" max="50" style="width: 90px" />
              <button class="primary" type="button" :disabled="following.loading" @click="loadFollowing(true)">&#x5237;&#x65B0;</button>
              <button type="button" :disabled="following.loading || !following.has_more" @click="loadFollowing(false)">&#x52A0;&#x8F7D;&#x66F4;&#x591A;</button>
            </div>
          </div>
          <div v-if="!auth.isLoggedIn" class="pill bad" style="margin-top: 10px">&#x672A;&#x767B;&#x5F55;&#xFF1A;&#x65E0;&#x6CD5;&#x8BBF;&#x95EE;&#x5173;&#x6CE8;&#x6D41;</div>
          <div v-if="following.error" class="pill bad" style="margin-top: 10px">&#x9519;&#x8BEF;&#xFF1A;{{ following.error }}</div>
          <div class="grid" style="gap: 10px; margin-top: 12px">
            <FeedVideoCard
              v-for="item in following.items"
              :key="`following-${item.id}`"
              :item="item"
              :can-like="canLike"
              :busy="action.loading"
              @toggle-like="toggleLike"
            />
          </div>
        </div>
      </div>

      <div class="card">
        <p class="title">&#x52A8;&#x4F5C;&#x8F93;&#x51FA;&#xFF08;&#x70B9;&#x8D5E;&#x7B49;&#xFF09;</p>
        <div class="row" style="margin-bottom: 10px">
          <span class="pill">&#x52A8;&#x4F5C;&#xFF1A;{{ action.name || '-' }}</span>
          <span v-if="action.loading" class="pill">&#x8BF7;&#x6C42;&#x4E2D;&#x2026;</span>
          <span v-if="action.error" class="pill bad">&#x9519;&#x8BEF;&#xFF1A;{{ action.error }}</span>
        </div>
        <JsonBox :value="action.payload" />
      </div>
    </div>
  </AppShell>
</template>
