<script setup lang="ts">
import { ref } from 'vue'

import type { AuthorManageKind } from '../api/video'

const emit = defineEmits<{
  unpublish: []
  relist: []
  delete: []
  continueDraft: []
}>()

const open = ref(false)
const kind = ref<AuthorManageKind>('published')
const busy = ref(false)

function openFor(next: AuthorManageKind, nextBusy = false) {
  kind.value = next
  busy.value = nextBusy
  open.value = true
}

function setBusy(next: boolean) {
  busy.value = next
}

function close() {
  open.value = false
}

defineExpose({ openFor, close, setBusy })
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="mask" @click.self="close">
      <div class="sheet" role="dialog" aria-modal="true" aria-label="作品管理">
        <div class="grab" />
        <div class="head">
          <p class="title">作品</p>
          <button class="x" type="button" aria-label="关闭" @click="close">×</button>
        </div>
        <p class="subtle">不公开后会出现在账号页的私密作品里，删除后无法找回。</p>
        <div class="row">
          <button v-if="kind === 'draft'" type="button" :disabled="busy" @click="emit('continueDraft')">继续编辑</button>
          <button v-else-if="kind === 'unpublished'" type="button" :disabled="busy" @click="emit('relist')">公开</button>
          <button v-else type="button" :disabled="busy" @click="emit('unpublish')">不公开</button>
          <button class="danger" type="button" :disabled="busy" @click="emit('delete')">删除</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 140;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding: 0;
  background: rgba(0, 0, 0, 0.58);
  backdrop-filter: blur(10px);
}

.sheet {
  width: min(480px, 100%);
  padding: 10px 18px 20px;
  border-radius: 20px 20px 0 0;
  border: 1px solid rgba(var(--fg), 0.12);
  border-bottom: 0;
  background: var(--surface);
  display: grid;
  gap: 12px;
}

.grab {
  width: 36px;
  height: 4px;
  margin: 4px auto 2px;
  border-radius: 999px;
  background: rgba(var(--fg), 0.2);
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.title {
  margin: 0;
  font-size: 18px;
  font-weight: 800;
}

.subtle {
  margin: 0;
  color: rgba(var(--fg), 0.62);
  font-size: 13px;
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

.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.row button {
  min-height: 42px;
  border-radius: 12px;
  border: 1px solid rgba(var(--fg), 0.14);
  background: rgba(var(--fg), 0.05);
  color: inherit;
  cursor: pointer;
}

.row button.danger {
  color: #fe2c55;
  border-color: rgba(254, 44, 85, 0.28);
}

.row button:disabled {
  opacity: 0.55;
  cursor: default;
}
</style>
