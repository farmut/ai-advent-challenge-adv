import { definePreset } from '@primeuix/themes'
import Aura from '@primeuix/themes/aura'

// Primary — indigo: белый текст на indigo-600 даёт контраст ≈ 6.3:1 (WCAG AA).
// Тап-цели меню и кнопки бургера — не меньше 44×44px.
export const AppPreset = definePreset(Aura, {
  semantic: {
    primary: {
      50: '{indigo.50}',
      100: '{indigo.100}',
      200: '{indigo.200}',
      300: '{indigo.300}',
      400: '{indigo.400}',
      500: '{indigo.500}',
      600: '{indigo.600}',
      700: '{indigo.700}',
      800: '{indigo.800}',
      900: '{indigo.900}',
      950: '{indigo.950}',
    },
    colorScheme: {
      light: {
        primary: {
          color: '{indigo.600}',
          contrastColor: '#ffffff',
          hoverColor: '{indigo.700}',
          activeColor: '{indigo.800}',
        },
      },
    },
  },
  components: {
    menubar: {
      baseItem: { padding: '0.75rem 1rem' },
      item: { padding: '0.75rem 1rem' },
      mobileButton: { size: '2.75rem', borderRadius: '0.5rem' },
    },
  },
})
