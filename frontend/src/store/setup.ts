import type { Backend } from '@/types'
import { computed, ref } from 'vue'

// A Rumiho instance has exactly one backend. URL parameters and stored backend
// lists cannot redirect authenticated API traffic to another server.
const localBackend: Backend = {
  uuid: 'rumiho',
  type: 'clash',
  label: 'Rumiho',
  protocol: location.protocol.slice(0, -1),
  host: location.hostname,
  port: location.port || (location.protocol === 'https:' ? '443' : '80'),
  secondaryPath: '/mihomo',
  password: '',
  disableUpgradeCore: true,
  disableTunMode: true,
}
export const backendList = computed(() => [localBackend])
export const activeUuid = ref('')
export const activeBackend = computed(() => (activeUuid.value ? localBackend : undefined))
export const activateInstance = () => {
  activeUuid.value = localBackend.uuid
}
export const deactivateInstance = () => {
  activeUuid.value = ''
}
