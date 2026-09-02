<template>
  <span v-if="displayVersion" class="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-[var(--app-muted)]">
    <span>v{{ displayVersion }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  version?: string
}>()

// 仅静态展示版本号，不提供检测更新 / 执行更新 / 重启服务等控件，
// 也不发起任何网络请求。
const displayVersion = computed(() => props.version || '')
</script>
