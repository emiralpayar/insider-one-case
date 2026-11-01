import { createRouter, createWebHistory } from 'vue-router'
import CampaignsView from './views/CampaignsView.vue'
import ComposerView from './views/ComposerView.vue'
import CampaignView from './views/CampaignView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'campaigns', component: CampaignsView },
    { path: '/new', name: 'new', component: ComposerView },
    { path: '/campaigns/:id/edit', name: 'edit', component: ComposerView, props: true },
    { path: '/campaigns/:id', name: 'campaign', component: CampaignView, props: true },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  // Every navigation (including the composer's ?step= changes) starts at the
  // top; back/forward restores the saved position.
  scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
})
