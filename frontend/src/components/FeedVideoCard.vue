<script setup lang="ts">
import type { FeedVideoItem } from '../api/types'

const props = defineProps<{
  item: FeedVideoItem
  canLike: boolean
  busy?: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle-like', item: FeedVideoItem): void
}>()

function onToggle() {
  emit('toggle-like', props.item)
}

function formatPopularity(value?: number) {
  if (typeof value !== 'number' || !Number.isFinite(value)) return ''
  return value.toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}
</script>

<template>
  <div class="feed-card">
    <RouterLink class="cover" :to="`/video/${item.id}`" :title="`查看《${item.title}》详情`">
      <img :src="item.cover_url" :alt="item.title" loading="lazy" />
    </RouterLink>
    <div class="content">
      <div class="row" style="justify-content: space-between">
        <div>
          <div class="title">
            <RouterLink :to="`/video/${item.id}`">{{ item.title }}</RouterLink>
          </div>
          <div class="subtle">
            &#x4F5C;&#x8005;&#xFF1A;{{ item.author.username }} (#{{ item.author.id }}) &#x00B7;
            &#x521B;&#x5EFA;&#x65F6;&#x95F4;&#xFF1A;{{ new Date(item.create_time * 1000).toLocaleString() }}
          </div>
        </div>
        <div class="row">
          <span v-if="typeof item.popularity === 'number'" class="pill mono hot-pill">热度 {{ formatPopularity(item.popularity) }}</span>
          <span class="pill mono">&#x2665; {{ item.likes_count }}</span>
          <span class="pill mono">&#x1F4AC; {{ item.comment_count }}</span>
          <button
            v-if="canLike"
            class="primary"
            type="button"
            :disabled="busy"
            @click="onToggle"
            :title="item.is_liked ? '\u53D6\u6D88\u70B9\u8D5E' : '\u70B9\u8D5E'"
          >
            {{ item.is_liked ? '\u5DF2\u8D5E' : '\u70B9\u8D5E' }}
          </button>
        </div>
      </div>
      <div v-if="item.description" class="muted" style="margin-top: 8px">{{ item.description }}</div>
      <div class="row" style="margin-top: 10px">
        <a class="pill mono" :href="item.play_url" target="_blank" rel="noreferrer">&#x64AD;&#x653E;&#x5730;&#x5740;</a>
        <RouterLink class="pill" :to="`/video/${item.id}`">&#x67E5;&#x770B;&#x8BE6;&#x60C5; / &#x8BC4;&#x8BBA;</RouterLink>
      </div>
    </div>
  </div>
</template>

<style scoped>
.feed-card {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  gap: 14px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: rgba(255, 255, 255, 0.06);
  border-radius: 16px;
  overflow: hidden;
}

.cover {
  background: rgba(0, 0, 0, 0.25);
  aspect-ratio: 16/9;
  display: block;
  overflow: hidden;
}

.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 160ms ease;
}

.cover:hover img {
  transform: scale(1.03);
}

.content {
  padding: 12px 12px 14px;
}

.hot-pill {
  border-color: rgba(254, 44, 85, 0.35);
  background: rgba(254, 44, 85, 0.1);
  color: rgba(255, 244, 246, 0.95);
}

@media (max-width: 900px) {
  .feed-card {
    grid-template-columns: 1fr;
  }
}
</style>
