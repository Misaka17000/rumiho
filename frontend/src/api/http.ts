import { expireSession } from '@/rumiho/session'
import axios from 'axios'

axios.interceptors.request.use((config) => {
  config.baseURL = `${location.origin}/mihomo`
  config.headers['X-Rumiho-Request'] = '1'
  delete config.headers.Authorization
  return config
})
axios.interceptors.response.use(undefined, (error) => {
  if (error.response?.status === 401) expireSession()
  return Promise.reject(error)
})
