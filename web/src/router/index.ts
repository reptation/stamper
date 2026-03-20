import { createRouter, createWebHistory } from 'vue-router'

import RunDetailPage from '@/pages/RunDetailPage.vue'
import RunsPage from '@/pages/RunsPage.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      redirect: '/runs',
    },
    {
      path: '/runs',
      name: 'runs',
      component: RunsPage,
    },
    {
      path: '/runs/:id',
      name: 'run-detail',
      component: RunDetailPage,
      props: true,
    },
    {
      path: '/:pathMatch(.*)*',
      redirect: '/runs',
    },
  ],
})

export default router
