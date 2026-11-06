import { watch } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import { locale, t } from './i18n'

export const routes = [
  { path: '/', name: 'monitor', component: () => import('./views/Monitor.vue'), meta: { title: 'nav.monitor' } },
  { path: '/lists/presets', name:'presets', component:()=>import('./views/Presets.vue'),meta:{title:'nav.presets'} },
  { path: '/lists/filters', name:'filters', component:()=>import('./views/Filters.vue'),meta:{title:'nav.filters'} },
  { path: '/lists/groups', name: 'groups', component: () => import('./views/Groups.vue'), meta: { title: 'nav.whitelist' } },
  { path: '/network/connections', name: 'connections', component: () => import('./views/Connections.vue'), meta: { title: 'nav.connections' } },
  { path: '/network/clients', name: 'clients', component: () => import('./views/Clients.vue'), meta: { title: 'nav.clients' } },
  { path: '/service/journal', name: 'journal', component: () => import('./views/Journal.vue'), meta: { title: 'nav.journal' } },
  { path:'/service/diagnostics',name:'diagnostics',component:()=>import('./views/Diagnostics.vue'),meta:{title:'ui.diagnostics'} },
  { path: '/settings', name: 'settings', component: () => import('./views/Settings.vue'), meta: { title: 'nav.settings' } },
  { path: '/login', name: 'login', component: () => import('./views/Login.vue'), meta: { title: 'nav.login', bare: true } },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({ history: createWebHistory(), routes })

// meta.title is a translation key; update the tab title when the locale changes.
const title = () => {
  const key = router.currentRoute.value.meta.title as string | undefined
  document.title = `${key ? t(key) : ''} · KeenProxy`
}
router.afterEach(title)
watch(locale, title)
