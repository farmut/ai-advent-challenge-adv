<script setup lang="ts">
import type { MenuItem } from 'primevue/menuitem'
import { useRoute } from 'vue-router'
import { scrollToHash } from '@/router/scrollBehavior'

// Пункты навигации по разделам главной. Подписи — тексты интерфейса, не контент.
const NAV_ITEMS = [
  { key: 'hero', label: 'Главная' },
  { key: 'about', label: 'О себе' },
  { key: 'skills', label: 'Навыки' },
  { key: 'projects', label: 'Проекты' },
  { key: 'experience', label: 'Опыт' },
  { key: 'contact', label: 'Контакты' },
] as const

const model: MenuItem[] = NAV_ITEMS.map((item) => ({ key: item.key, label: item.label }))

const route = useRoute()

function labelOf(item: MenuItem): string {
  return typeof item.label === 'string' ? item.label : ''
}

function hashOf(item: MenuItem): string {
  return `#${item.key ?? ''}`
}

/**
 * Повторный клик по уже активному якорю роутер считает дублирующей навигацией и не прокручивает.
 * В этом случае прокручиваем сами, с тем же отступом под шапку, что и scrollBehavior.
 */
function onNavigate(event: MouseEvent, navigate: (e?: MouseEvent) => unknown, hash: string): void {
  if (route.path === '/' && route.hash === hash && scrollToHash(hash)) {
    event.preventDefault()
    return
  }
  navigate(event)
}
</script>

<template>
  <header
    class="app-header"
    data-app-header
  >
    <nav
      class="app-header__nav"
      aria-label="Основная навигация"
    >
      <Menubar
        :model="model"
        breakpoint="640px"
        class="app-header__menubar"
        :button-props="{ 'aria-label': 'Меню' }"
      >
        <template #start>
          <RouterLink
            :to="{ path: '/', hash: '#hero' }"
            class="app-header__brand"
          >
            Портфолио
          </RouterLink>
        </template>
        <template #item="{ item, props }">
          <RouterLink
            v-slot="{ href, navigate }"
            :to="{ path: '/', hash: hashOf(item) }"
            custom
          >
            <a
              :href="href"
              v-bind="props.action"
              class="app-header__link"
              :data-testid="`nav-${item.key}`"
              @click="onNavigate($event, navigate, hashOf(item))"
            >
              <span v-bind="props.label">{{ labelOf(item) }}</span>
            </a>
          </RouterLink>
        </template>
      </Menubar>
    </nav>
  </header>
</template>

<style scoped>
.app-header {
  position: sticky;
  top: 0;
  z-index: 10;
  background: var(--color-bg);
  box-shadow: var(--header-shadow);
}

.app-header__nav {
  max-width: var(--container-max);
  margin: 0 auto;
  padding: var(--space-1) var(--container-padding);
}

.app-header__menubar {
  justify-content: space-between;
  border: 0;
  border-radius: 0;
  padding: 0;
  background: transparent;
}

.app-header__brand {
  display: inline-flex;
  align-items: center;
  min-height: var(--tap-target);
  margin-right: var(--space-4);
  font-weight: 700;
  color: var(--color-heading);
  text-decoration: none;
}

.app-header__link {
  min-height: var(--tap-target);
  align-items: center;
}
</style>
