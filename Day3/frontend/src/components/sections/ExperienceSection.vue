<script setup lang="ts">
import ExperienceEntry from '@/components/ui/ExperienceEntry.vue'
import PageSection from '@/components/ui/PageSection.vue'
import type { ExperienceItem } from '@/content/types'

defineProps<{
  items: readonly ExperienceItem[]
}>()

// Пустая колонка «opposite» у Timeline align="left" занимает половину ширины — на 360px это много.
// Класс выдаётся через pass-through, внутренние классы .p-* не переопределяются.
const timelinePt = { eventOpposite: { class: 'experience__opposite' } }
</script>

<template>
  <PageSection
    id="experience"
    title="Опыт"
  >
    <p
      v-if="items.length === 0"
      class="experience__empty"
      data-testid="experience-empty"
    >
      Опыт пока не добавлен.
    </p>
    <Timeline
      v-else
      :value="[...items]"
      align="left"
      data-key="id"
      class="experience"
      :pt="timelinePt"
    >
      <!-- Слот Timeline типизирован библиотекой как any; ExperienceEntry принимает строго ExperienceItem. -->
      <template #content="{ item }">
        <ExperienceEntry :item="item" />
      </template>
    </Timeline>
  </PageSection>
</template>

<style scoped>
.experience :deep(.experience__opposite) {
  flex: 0 0 0;
  padding: 0;
}

.experience__empty {
  margin: 0;
  color: var(--color-text-muted);
}
</style>
