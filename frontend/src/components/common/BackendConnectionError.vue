<script setup lang="ts">
import { computed } from 'vue'
import { backendProbe } from '@/assembly/version'
import { startBackendSession } from '@/assembly/session'
import { instance } from '@/rumiho/session'
const failed = computed(() => !!instance.value && backendProbe.value?.status === 'failed')
</script>
<template>
  <div
    v-if="failed"
    role="alert"
    class="core-unavailable"
  >
    <span>暂时无法连接代理内核</span
    ><button
      class="btn btn-sm"
      @click="startBackendSession"
    >
      重新连接
    </button>
  </div>
</template>
