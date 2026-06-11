import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import './style.css'
import App from './App.vue'
import router from './router'
import { useSessionStore } from './stores/session'
import { applyEmbeddedContext, parseEmbeddedLaunchContext, stripEmbeddedLaunchParams } from './utils/embedded'

async function bootstrap() {
  const embeddedContext = parseEmbeddedLaunchContext()
  if (embeddedContext) {
    applyEmbeddedContext(embeddedContext)
    stripEmbeddedLaunchParams()
  }

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)
  app.use(ElementPlus)
  app.use(router)

  const session = useSessionStore()
  await session.bootstrap(embeddedContext)
  await router.isReady()

  app.mount('#app')
}

void bootstrap()
