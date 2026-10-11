<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import {
  ArrowPathIcon,
  ArrowRightOnRectangleIcon,
  CheckCircleIcon,
  ChevronDownIcon,
  EyeIcon,
  EyeSlashIcon,
  LinkIcon,
  KeyIcon,
  ShieldCheckIcon,
  SignalIcon,
} from '@heroicons/vue/24/outline'
import { fetchConfigs } from '@/assembly/config'
import { fetchProxies } from '@/assembly/proxies'
import { fetchRules } from '@/assembly/rules'
import { showConfirmDialog } from '@/helper/confirm-dialog'
import {
  instance,
  request,
  refreshInstance,
  logout,
  expireSession,
  type InstanceSettings,
  type InstanceState,
} from './session'
import SecretField from './SecretField.vue'

const form = reactive<Omit<InstanceSettings, 'mode'>>({
  subscriptionURL: '',
  updateHours: 24,
  publicURL: '',
})
const edited = ref(false)
const busy = ref('')
const notice = ref('')
const error = ref('')
const revealSubscription = ref(false)
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
function editableSettings(state: InstanceState) {
  const { subscriptionURL, updateHours, publicURL } = state.settings
  return { subscriptionURL, updateHours, publicURL }
}
const dirty = computed(
  () =>
    edited.value &&
    JSON.stringify(form) !== JSON.stringify(instance.value && editableSettings(instance.value)),
)
const updatedAt = computed(() => {
  const value = instance.value?.lastUpdated
  return value && !value.startsWith('0001')
    ? new Date(value).toLocaleString('zh-CN', {
        month: 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    : '尚未更新'
})
function fill(state: InstanceState) {
  Object.assign(form, editableSettings(state))
  edited.value = false
}
watch(
  instance,
  (state) => {
    if (state && !edited.value) fill(state)
  },
  { immediate: true },
)
onMounted(() => {
  refreshInstance().catch((e) => {
    error.value = e.message
  })
})

async function confirmDiscard() {
  if (!dirty.value) return true
  return (
    await showConfirmDialog({
      title: '放弃未保存的配置？',
      message: '离开后，本次填写的内容不会保存。',
      confirmText: '放弃修改',
      cancelText: '继续编辑',
    })
  ).confirmed
}
onBeforeRouteLeave(confirmDiscard)

async function run(action: string, task: () => Promise<void>) {
  if (busy.value) return
  busy.value = action
  error.value = ''
  notice.value = ''
  try {
    await task()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败，请稍后重试'
  } finally {
    busy.value = ''
  }
}
async function applyCoreState(state: InstanceState) {
  instance.value = state
  fill(state)
  // Reuse zashboard's data stores so node and rule pages update immediately.
  await Promise.allSettled([fetchConfigs(), fetchProxies(), fetchRules()])
}
async function save() {
  if (instance.value?.settings.subscriptionURL && !form.subscriptionURL.trim()) {
    const answer = await showConfirmDialog({
      title: '清空订阅？',
      message: '当前订阅的节点、分组和规则将被删除，代理将使用直连。',
      confirmText: '清空订阅',
      cancelText: '取消',
    })
    if (!answer.confirmed) return
  }
  await run('save', async () => {
    await applyCoreState(
      await request<InstanceState>('/settings', 'PUT', {
        ...form,
        subscriptionURL: form.subscriptionURL.trim(),
        publicURL: form.publicURL.trim(),
      }),
    )
    notice.value = '配置已保存并应用。'
  })
}
async function refresh() {
  await run('refresh', async () => {
    try {
      await applyCoreState(await request<InstanceState>('/refresh', 'POST'))
      notice.value = '订阅已更新，节点、分组和规则已同步。'
    } catch (e) {
      await refreshInstance().catch(() => {})
      throw e
    }
  })
}
async function rotate() {
  const answer = await showConfirmDialog({
    title: '重置 HTTP 代理链接？',
    message: '旧代理链接将立即失效，当前代理连接会断开。请在局域网设备上替换为新链接。',
    confirmText: '重置链接',
    cancelText: '取消',
    confirmButtonClass: 'btn-error',
  })
  if (!answer.confirmed) return
  await run('rotate', async () => {
    instance.value = await request<InstanceState>('/rotate', 'POST')
    notice.value = 'HTTP 代理链接已重置，请复制新链接。'
  })
}
async function changePassword() {
  if (Array.from(newPassword.value).length < 8) {
    error.value = '登录密码至少需要 8 个字符。'
    return
  }
  if (new TextEncoder().encode(newPassword.value).length > 4096) {
    error.value = '登录密码不能超过 4096 字节。'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    error.value = '两次输入的新密码不一致。'
    return
  }
  if (!(await confirmDiscard())) return
  await run('password', async () => {
    await request('/password', 'POST', {
      currentPassword: currentPassword.value,
      newPassword: newPassword.value,
    })
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    edited.value = false
    expireSession()
  })
}
async function signOut() {
  if (!(await confirmDiscard())) return
  await run('logout', async () => {
    edited.value = false
    await logout()
  })
}
</script>

<template>
  <div class="instance-page">
    <div class="instance-inner">
      <header class="instance-header">
        <div>
          <span class="rumiho-eyebrow">我的实例</span>
          <h1>订阅与接入</h1>
          <p>管理上游订阅，为局域网设备提供 HTTP 代理。</p>
        </div>
        <span
          class="instance-status"
          :class="!instance?.coreOnline && 'is-offline'"
          ><span></span>{{ instance?.coreOnline ? '运行正常' : '内核未连接' }}</span
        >
      </header>
      <div
        v-if="error || notice"
        class="instance-feedback"
        :class="error ? 'is-error' : 'is-success'"
        :role="error ? 'alert' : 'status'"
      >
        <CheckCircleIcon
          v-if="!error"
          class="size-4 shrink-0"
        /><span>{{ error || notice }}</span>
      </div>
      <form
        id="instance-settings"
        @submit.prevent="save"
        @input="edited = true"
        @change="edited = true"
      >
        <fieldset
          :disabled="!!busy"
          class="contents"
        >
          <div class="instance-grid">
            <section class="instance-card">
              <div class="instance-card-heading">
                <span class="instance-icon"><LinkIcon /></span>
                <div>
                  <h2>上游订阅</h2>
                  <p>导入 Clash / mihomo 节点，保留订阅分组与规则。</p>
                </div>
                <button
                  type="button"
                  class="btn btn-ghost btn-square btn-sm ml-auto"
                  :disabled="!!busy || dirty || !instance?.settings.subscriptionURL"
                  aria-label="更新订阅"
                  title="更新订阅"
                  @click="refresh"
                >
                  <ArrowPathIcon
                    class="size-4"
                    :class="busy === 'refresh' && 'animate-spin'"
                  />
                </button>
              </div>
              <div class="rumiho-field">
                <label for="subscription-url">订阅链接</label>
                <div class="rumiho-input-action">
                  <input
                    id="subscription-url"
                    v-model="form.subscriptionURL"
                    :type="revealSubscription ? 'url' : 'password'"
                    class="input w-full"
                    placeholder="粘贴你的订阅链接"
                    autocomplete="off"
                    spellcheck="false"
                  /><button
                    type="button"
                    :aria-label="revealSubscription ? '隐藏订阅链接' : '显示订阅链接'"
                    @click="revealSubscription = !revealSubscription"
                  >
                    <component
                      :is="revealSubscription ? EyeSlashIcon : EyeIcon"
                      class="size-4"
                    />
                  </button>
                </div>
              </div>
              <div class="subscription-summary">
                <span
                  ><i class="summary-dot"></i>{{ instance?.nodeCount || 0 }} 个节点<span
                    v-if="instance?.subscriptionGroupCount"
                  >
                    · {{ instance.subscriptionGroupCount }} 个订阅分组</span
                  ></span
                ><span>{{ updatedAt }}</span>
              </div>
              <p
                v-if="instance?.lastError"
                role="alert"
                class="rumiho-error"
              >
                {{ instance.lastError }}
              </p>
              <div class="instance-divider"></div>
              <div class="rumiho-field">
                <label for="update-hours">自动更新</label>
                <div class="instance-inline-field">
                  <p>定时检查订阅，保持节点最新。</p>
                  <select
                    id="update-hours"
                    v-model.number="form.updateHours"
                    class="select"
                  >
                    <option :value="1">每小时</option>
                    <option :value="6">每 6 小时</option>
                    <option :value="12">每 12 小时</option>
                    <option :value="24">每天</option>
                    <option :value="48">每 2 天</option>
                    <option :value="168">每周</option>
                  </select>
                </div>
              </div>
            </section>
            <section class="instance-card">
              <div class="instance-card-heading">
                <span class="instance-icon"><SignalIcon /></span>
                <div>
                  <h2>HTTP 代理</h2>
                  <p>复制带认证的代理链接，在局域网设备中使用。</p>
                </div>
              </div>
              <div class="rumiho-field">
                <label for="public-url">接入地址</label
                ><input
                  id="public-url"
                  v-model="form.publicURL"
                  type="url"
                  class="input w-full"
                  :placeholder="instance?.access.publicURL || 'http://192.168.1.10:8080'"
                  spellcheck="false"
                />
                <p class="rumiho-hint">填写局域网可访问的地址；保存后更新代理链接。</p>
              </div>
              <div class="instance-divider"></div>
              <SecretField
                id="proxy-url"
                label="HTTP 代理链接"
                :value="instance?.access.proxyURL || ''"
              />
              <div class="proxy-reset">
                <p class="rumiho-hint">重置后旧链接立即失效。</p>
                <button
                  type="button"
                  class="btn btn-sm btn-ghost text-error"
                  :disabled="!!busy"
                  @click="rotate"
                >
                  <KeyIcon class="size-4" />{{ busy === 'rotate' ? '正在重置…' : '重置链接' }}
                </button>
              </div>
            </section>
          </div>
          <div class="instance-save-bar">
            <span class="text-base-content/50 text-sm">{{
              dirty ? '有尚未保存的修改' : '配置已同步到当前实例'
            }}</span
            ><button
              class="btn btn-primary"
              :disabled="!!busy || !dirty"
            >
              <span
                v-if="busy === 'save'"
                class="loading loading-spinner loading-xs"
              ></span
              >{{ busy === 'save' ? '检查并应用中…' : '保存配置' }}
            </button>
          </div>
        </fieldset>
      </form>
      <section class="instance-card account-card">
        <div class="account-row">
          <span class="instance-icon"><ShieldCheckIcon /></span>
          <div>
            <h2>{{ instance?.username || '管理员' }}</h2>
            <p>登录与 HTTP 代理共用此用户名</p>
          </div>
          <button
            type="button"
            class="btn btn-ghost btn-sm ml-auto"
            :disabled="!!busy"
            @click="signOut"
          >
            <ArrowRightOnRectangleIcon class="size-4" />退出登录
          </button>
        </div>
        <details class="account-password">
          <summary>修改登录密码<ChevronDownIcon class="size-3.5" /></summary>
          <form
            class="password-form"
            @submit.prevent="changePassword"
          >
            <div class="rumiho-field">
              <label for="current-password">当前密码</label>
              <input
                id="current-password"
                v-model="currentPassword"
                type="password"
                class="input w-full"
                autocomplete="current-password"
                required
              />
            </div>
            <div class="rumiho-field">
              <label for="new-password">新密码</label>
              <input
                id="new-password"
                v-model="newPassword"
                type="password"
                class="input w-full"
                autocomplete="new-password"
                minlength="8"
                maxlength="4096"
                placeholder="至少 8 个字符"
                required
              />
            </div>
            <div class="rumiho-field">
              <label for="confirm-password">确认新密码</label>
              <input
                id="confirm-password"
                v-model="confirmPassword"
                type="password"
                class="input w-full"
                autocomplete="new-password"
                required
              />
            </div>
            <div class="password-actions">
              <p class="rumiho-hint">保存后重新登录，HTTP 代理密码保持不变。</p>
              <button
                class="btn btn-sm"
                :disabled="!!busy"
              >
                {{ busy === 'password' ? '正在保存…' : '保存新密码' }}
              </button>
            </div>
          </form>
        </details>
      </section>
      <footer class="instance-footer">
        Rumiho <span>·</span> zashboard <span>·</span> mihomo {{ instance?.version?.version }}
      </footer>
    </div>
  </div>
</template>
