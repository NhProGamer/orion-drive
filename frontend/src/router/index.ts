import { createRouter, createWebHistory } from 'vue-router'

// Every view is lazily loaded so each ends up in its own chunk: a public
// share-link visitor downloads only the small entry + the ShareView chunk,
// not the whole drive shell, image editor or admin panel.
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: () => import('@/views/HomeView.vue') },
    { path: '/admin', name: 'admin', component: () => import('@/views/AdminView.vue') },
    { path: '/s/:token', name: 'share', component: () => import('@/views/ShareView.vue') },
  ],
})
