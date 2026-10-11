<script setup lang="ts">
import { ref } from 'vue'
import { useClipboard } from '@vueuse/core'
import { CheckIcon, ClipboardDocumentIcon, EyeIcon, EyeSlashIcon } from '@heroicons/vue/24/outline'

const props = defineProps<{ id: string; label: string; value: string }>()
const revealed = ref(false)
const error = ref('')
const { copy, copied } = useClipboard({ legacy: true, copiedDuring: 1800 })
async function copyValue() {
  error.value = ''
  try {
    await copy(props.value)
  } catch {
    error.value = '复制失败，请显示后手动复制。'
  }
}
</script>
<template>
  <div class="rumiho-field">
    <label :for="id">{{ label }}</label>
    <div class="secret-field">
      <input
        :id="id"
        :type="revealed ? 'text' : 'password'"
        :value="value"
        readonly
        autocomplete="off"
        spellcheck="false"
      />
      <button
        type="button"
        class="btn btn-ghost btn-square btn-sm"
        :aria-label="`${revealed ? '隐藏' : '显示'}${label}`"
        :aria-pressed="revealed"
        @click="revealed = !revealed"
      >
        <component
          :is="revealed ? EyeSlashIcon : EyeIcon"
          class="size-4"
        />
      </button>
      <button
        type="button"
        class="btn btn-ghost btn-square btn-sm"
        :aria-label="`复制${label}`"
        @click="copyValue"
      >
        <component
          :is="copied ? CheckIcon : ClipboardDocumentIcon"
          class="size-4"
          :class="copied && 'text-primary'"
        />
      </button>
    </div>
    <span
      v-if="copied"
      role="status"
      class="text-primary text-xs"
      >已复制</span
    >
    <span
      v-if="error"
      role="alert"
      class="text-error text-xs"
      >{{ error }}</span
    >
  </div>
</template>
