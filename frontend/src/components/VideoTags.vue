<script setup lang="ts">
defineProps<{
  tags?: string[]
  overlay?: boolean
}>()
</script>

<template>
  <div v-if="tags?.length" class="video-tags" :class="{ overlay }">
    <span v-for="tag in tags" :key="tag" class="tag">{{ tag }}</span>
  </div>
</template>

<style scoped>
.video-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.tag {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  padding: 2px 8px;
  border-radius: 999px;
  background: rgba(var(--fg), 0.08);
  color: rgba(var(--fg), 0.82);
  font-size: 12px;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overlay .tag {
  background: rgba(255, 255, 255, 0.16);
  color: #fff;
  text-shadow: 0 1px 6px rgba(0, 0, 0, 0.45);
}
</style>
