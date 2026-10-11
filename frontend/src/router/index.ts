import { ROUTE_NAME } from '@/constant'
import { resolvePageTransition } from '@/helper/page-transition'
import { i18n } from '@/i18n'
import InstancePage from '@/rumiho/InstancePage.vue'
import { ensureSession, instance } from '@/rumiho/session'
import { language } from '@/store/settings'
import { activeBackend } from '@/store/setup'
import ConnectionsPage from '@/views/ConnectionsPage.vue'
import HomePage from '@/views/HomePage.vue'
import LogsPage from '@/views/LogsPage.vue'
import OverviewPage from '@/views/OverviewPage.vue'
import ProxiesPage from '@/views/ProxiesPage.vue'
import RulesPage from '@/views/RulesPage.vue'
import SettingsPage from '@/views/SettingsPage.vue'
import SetupPage from '@/views/SetupPage.vue'
import { useTitle } from '@vueuse/core'
import { watch } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'

const childrenRouter = [
  { path: 'instance', name: ROUTE_NAME.instance, component: InstancePage },
  {
    path: 'proxies',
    name: ROUTE_NAME.proxies,
    component: ProxiesPage,
  },
  {
    path: 'overview',
    name: ROUTE_NAME.overview,
    component: OverviewPage,
  },
  {
    path: 'connections',
    name: ROUTE_NAME.connections,
    component: ConnectionsPage,
  },
  {
    path: 'logs',
    name: ROUTE_NAME.logs,
    component: LogsPage,
  },
  {
    path: 'rules',
    name: ROUTE_NAME.rules,
    component: RulesPage,
  },
  {
    path: 'settings',
    name: ROUTE_NAME.settings,
    component: SettingsPage,
  },
]

const router = createRouter({
  history: createWebHashHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      redirect: ROUTE_NAME.proxies,
      component: HomePage,
      children: childrenRouter,
    },
    {
      path: '/login',
      name: ROUTE_NAME.setup,
      component: SetupPage,
    },
    { path: '/setup', redirect: '/instance' },
    {
      path: '/:catchAll(.*)',
      redirect: ROUTE_NAME.proxies,
    },
  ],
})

const title = useTitle('Rumiho')
const setTitleByName = (name: string | symbol | undefined) => {
  if (typeof name === 'string' && activeBackend.value) {
    const backend = activeBackend.value
    const prefix = backend.label || 'Rumiho'
    title.value = `${prefix} | ${i18n.global.t(name)}`
  } else {
    title.value = '登录 · Rumiho'
  }
}

router.beforeEach(async (to, from) => {
  resolvePageTransition(to, from)
  const authenticated = await ensureSession()
  if (!authenticated && to.name !== ROUTE_NAME.setup) {
    return { name: ROUTE_NAME.setup, query: { next: to.fullPath } }
  }
  if (authenticated && to.name === ROUTE_NAME.setup) {
    return { name: instance.value?.nodeCount ? ROUTE_NAME.proxies : ROUTE_NAME.instance }
  }
})

router.afterEach((to) => {
  setTitleByName(to.name)
})

watch([language, activeBackend], () => {
  setTimeout(() => {
    setTitleByName(router.currentRoute.value.name)
  })
})

export default router
