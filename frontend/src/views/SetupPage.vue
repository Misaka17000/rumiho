<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowRightIcon, EyeIcon, EyeSlashIcon } from '@heroicons/vue/24/outline'
import BrandMark from '@/rumiho/BrandMark.vue'
import { login } from '@/rumiho/session'

const route = useRoute()
const router = useRouter()
const username = ref('admin')
const password = ref('')
const visible = ref(false)
const busy = ref(false)
const error = ref('')
const host = location.host
const canSubmit = computed(() => username.value.trim() && password.value && !busy.value)

async function submit() {
  if (!canSubmit.value) return
  busy.value = true
  error.value = ''
  try {
    const state = await login(username.value.trim(), password.value)
    password.value = ''
    const next = typeof route.query.next === 'string' ? route.query.next : ''
    const destination = /^\/(proxies|connections|rules|logs|overview|settings|instance)(\?|$)/.test(
      next,
    )
      ? next
      : state.nodeCount
        ? '/proxies'
        : '/instance'
    await router.replace(!state.nodeCount && destination === '/proxies' ? '/instance' : destination)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '无法登录，请稍后重试'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="rumiho-login">
    <div class="login-layout">
      <section class="login-intro">
        <div class="login-brand"><BrandMark class="text-primary size-10" /><span>Rumiho</span></div>
        <div class="login-message">
          <h1>比特流转，<br />远方咫尺。</h1>
          <p>循着微光，与世界相连。</p>
          <div
            class="login-path"
            aria-hidden="true"
          >
            <span class="path-point"></span><i></i><BrandMark class="text-primary size-12" /><i></i
            ><span class="path-point"></span>
          </div>
          <div
            class="login-path-labels"
            aria-hidden="true"
          >
            <span>此端</span><span>Rumiho</span><span>远方</span>
          </div>
        </div>
        <span class="login-credit">zashboard · mihomo</span>
      </section>
      <section class="login-form-panel">
        <div class="login-server">
          <span class="bg-primary size-1.5 rounded-full"></span>{{ host }}
        </div>
        <div class="login-form-heading">
          <h2>登录</h2>
        </div>
        <form
          class="rumiho-form"
          @submit.prevent="submit"
        >
          <div class="rumiho-field">
            <label for="username">用户名</label
            ><input
              id="username"
              v-model="username"
              class="input w-full"
              autocomplete="username"
              required
              autofocus
            />
          </div>
          <div class="rumiho-field">
            <label for="password">密码</label>
            <div class="rumiho-input-action">
              <input
                id="password"
                v-model="password"
                class="input w-full"
                :type="visible ? 'text' : 'password'"
                autocomplete="current-password"
                required
              /><button
                type="button"
                :aria-label="visible ? '隐藏密码' : '显示密码'"
                @click="visible = !visible"
              >
                <component
                  :is="visible ? EyeSlashIcon : EyeIcon"
                  class="size-4"
                />
              </button>
            </div>
          </div>
          <p
            v-if="error"
            role="alert"
            class="rumiho-error"
          >
            {{ error }}
          </p>
          <button
            class="btn btn-primary login-submit w-full"
            :disabled="!canSubmit"
          >
            <span
              v-if="busy"
              class="loading loading-spinner loading-xs"
            ></span
            >{{ busy ? '正在登录' : '登录'
            }}<ArrowRightIcon
              v-if="!busy"
              class="size-4"
            />
          </button>
        </form>
      </section>
    </div>
  </main>
</template>
