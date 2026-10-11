<template>
  <div class="flex flex-col gap-2 p-2 text-sm">
    <SidebarStatistics />
    <div class="flex items-center gap-2">
      <SidebarButtons />
      <BackendVersion class="min-w-0 flex-1" />
    </div>
  </div>
</template>

<script setup lang="ts">
import BackendVersion from '../common/BackendVersion.vue'
import SidebarButtons from './SidebarButtons.vue'
import SidebarStatistics from './SidebarStatistics.vue'
</script>
