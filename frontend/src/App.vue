<script setup lang="ts">
import './assembly/session'
import './store/conn-history'
import { computed, ref, type Ref, watch } from 'vue'
import { RouterView } from 'vue-router'
import BackendConnectionError from './components/common/BackendConnectionError.vue'
import { useAppearanceVars } from './composables/use-appearance-vars'
import { useOverscrollLock } from './composables/use-overscroll-lock'
import { useThemeColor } from './composables/use-theme-color'
import ConfirmDialogHost from './components/common/ConfirmDialogHost.vue'
import { useKeyboard } from './composables/use-keyboard'
import { EMOJIS, FONTS } from './constant'
import { backgroundImage } from './helper/indexeddb'
import { initNotification } from './helper/notification'
import { emoji, font, theme } from './store/settings'

const app = ref<HTMLElement>()
const toast = ref<HTMLElement>()

initNotification(toast as Ref<HTMLElement>)

const FONT_CLASS_MAP = {
  [EMOJIS.TWEMOJI]: {
    [FONTS.MI_SANS]: 'font-MiSans-Twemoji',
    [FONTS.SARASA_UI]: 'font-SarasaUI-Twemoji',
    [FONTS.PING_FANG]: 'font-PingFang-Twemoji',
    [FONTS.FIRA_SANS]: 'font-FiraSans-Twemoji',
    [FONTS.SYSTEM_UI]: 'font-SystemUI-Twemoji',
  },
  [EMOJIS.NOTO_COLOR_EMOJI]: {
    [FONTS.MI_SANS]: 'font-MiSans-NotoEmoji',
    [FONTS.SARASA_UI]: 'font-SarasaUI-NotoEmoji',
    [FONTS.PING_FANG]: 'font-PingFang-NotoEmoji',
    [FONTS.FIRA_SANS]: 'font-FiraSans-NotoEmoji',
    [FONTS.SYSTEM_UI]: 'font-SystemUI-NotoEmoji',
  },
} as const

const fontClassName = computed(() => {
  return (
    FONT_CLASS_MAP[emoji.value]?.[font.value] || FONT_CLASS_MAP[EMOJIS.TWEMOJI][FONTS.SYSTEM_UI]
  )
})

const { setThemeColor } = useThemeColor(app)

useOverscrollLock()

watch(
  theme,
  () => {
    document.documentElement.setAttribute('data-theme', theme.value)
    setThemeColor()
  },
  {
    immediate: true,
  },
)

useAppearanceVars()
useKeyboard()
</script>

<template>
  <div
    ref="app"
    id="app-content"
    :class="[
      'bg-base-100 flex w-screen overflow-hidden',
      fontClassName,
      backgroundImage && 'custom-background bg-cover bg-center',
    ]"
    :style="[backgroundImage, { height: 'var(--app-height, 100dvh)' }]"
  >
    <div
      aria-hidden="true"
      class="status-bar-tint"
    />
    <RouterView />
    <BackendConnectionError />
    <ConfirmDialogHost />
    <div
      ref="toast"
      class="app-toast-region"
    />
  </div>
</template>

<style>
.status-bar-tint {
  display: none;
}

@supports (-webkit-touch-callout: none) {
  .status-bar-tint {
    position: fixed;
    top: 0;
    right: 0;
    left: 0;
    z-index: 2147483647;
    display: block;
    height: 12px;
    background-color: var(--status-bar-tint, var(--color-base-100));
    opacity: 0.12;
    pointer-events: none;
  }
}

.app-toast-region {
  position: fixed;
  top: calc(0.75rem + env(safe-area-inset-top, 0px));
  right: calc(0.75rem + env(safe-area-inset-right, 0px));
  z-index: 100000;
  display: flex;
  width: min(24rem, calc(100vw - 1.5rem));
  flex-direction: column;
  gap: 0.625rem;
  pointer-events: none;
}

@media (min-width: 768px) {
  .app-toast-region {
    top: calc(2.75rem + env(safe-area-inset-top, 0px));
    right: calc(1rem + env(safe-area-inset-right, 0px));
  }
}

.app-toast {
  --toast-accent: var(--color-primary);
  grid-template-columns: 0.25rem 1.75rem minmax(0, 1fr) 1.5rem;
  animation: appToastIn 0.22s cubic-bezier(0.32, 0.72, 0, 1) both;
}

.app-toast[data-toast-type='success'] {
  --toast-accent: var(--color-success);
}

.app-toast[data-toast-type='error'] {
  --toast-accent: var(--color-error);
}

.app-toast[data-toast-type='warning'] {
  --toast-accent: var(--color-warning);
}

.app-toast[data-toast-type='info'] {
  --toast-accent: var(--color-info);
}

.app-toast.is-leaving {
  pointer-events: none;
  animation: appToastOut 0.16s ease-in both;
}

.app-toast__content {
  min-width: 0;
  padding-top: 0.2rem;
  color: var(--color-base-content);
  font-size: 0.875rem;
  line-height: 1.4;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

@keyframes appToastIn {
  from {
    opacity: 0;
    transform: translateX(0.75rem) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateX(0) scale(1);
  }
}

@keyframes appToastOut {
  from {
    opacity: 1;
    transform: translateX(0) scale(1);
  }
  to {
    opacity: 0;
    transform: translateX(0.5rem) scale(0.98);
  }
}

@keyframes progressBar {
  from {
    width: 100%;
  }
  to {
    width: 0%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .app-toast,
  .app-toast.is-leaving {
    animation-duration: 0.01ms;
  }
}
</style>
