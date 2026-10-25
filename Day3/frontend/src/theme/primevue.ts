import type { PrimeVueConfiguration } from 'primevue/config'
import { AppPreset } from './preset'

/** Настройки PrimeVue: общие для приложения (main.ts) и тестов компонентов. */
export const primeVueOptions: PrimeVueConfiguration = {
  theme: {
    preset: AppPreset,
    // Тёмной темы нет: без false PrimeVue перекрасит свои компоненты по системной теме,
    // а собственные стили останутся светлыми.
    options: { darkModeSelector: false },
  },
}
