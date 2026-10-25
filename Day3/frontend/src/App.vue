<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { nextTick, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import AppFooter from '@/components/ui/AppFooter.vue'
import AppHeader from '@/components/ui/AppHeader.vue'
import { scrollToHash } from '@/router/scrollBehavior'

// unhead на сервере по умолчанию ставит lang="en" и перекрывает атрибут из index.html.
useHead({ htmlAttrs: { lang: 'ru' } })

const route = useRoute()

onMounted(async () => {
  // vite-ssg вызывает scrollBehavior первой навигации до монтирования, на пререндеренном DOM без стилей
  // PrimeVue. После монтирования вёрстка другая, поэтому прямой заход по /#раздел докручиваем повторно.
  if (route.hash !== '') {
    await nextTick()
    scrollToHash(route.hash)
  }
  // Сигнал «приложение смонтировано»: без гидрации vite-ssg заменяет пререндеренный DOM после загрузки JS.
  // По нему E2E и smoke-проверки ждут интерактивности, а не таймаутов.
  document.documentElement.dataset.appMounted = 'true'
})
</script>

<template>
  <a
    class="skip-link"
    href="#main"
  >Перейти к содержимому</a>
  <AppHeader />
  <main
    id="main"
    class="app-main"
    tabindex="-1"
  >
    <RouterView />
  </main>
  <AppFooter />
</template>

<style scoped>
.skip-link {
  position: absolute;
  top: var(--space-2);
  left: var(--space-2);
  z-index: 100;
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius);
  background: var(--color-primary);
  color: var(--color-primary-contrast);
  font-weight: 600;
  transform: translateY(-200%);
}

.skip-link:focus {
  transform: none;
}

.app-main {
  display: block;
  min-height: 60vh;
}

.app-main:focus {
  outline: none;
}
</style>
