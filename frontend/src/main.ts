import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import './assets/styles/index.css'
import { loadBranding } from './lib/branding'

createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
// Not awaited: the built-in banner and favicon show until the custom ones load.
loadBranding()
