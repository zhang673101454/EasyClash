import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { applyTheme, getTheme } from './lib/theme'
import './style.css'

applyTheme(getTheme())

const app = createApp(App)
app.use(createPinia())
app.mount('#app')
