<script setup lang="ts">
import MarkdownContent from '@/components/ui/MarkdownContent.vue'
import { formatPeriod } from '@/composables/formatPeriod'
import type { ExperienceItem } from '@/content/types'

defineProps<{
  item: ExperienceItem
}>()
</script>

<template>
  <article
    class="experience-entry"
    data-testid="experience-item"
  >
    <h3 class="experience-entry__position">
      {{ item.position }}
    </h3>
    <p class="experience-entry__company">
      {{ item.company }}
    </p>
    <p class="experience-entry__period">
      {{ formatPeriod(item.start, item.end) }}
    </p>
    <MarkdownContent :html="item.html" />
  </article>
</template>

<style scoped>
.experience-entry {
  padding-bottom: var(--space-6);
}

.experience-entry__position {
  margin: 0;
  font-size: var(--text-h3);
  line-height: 1.3;
  color: var(--color-heading);
}

.experience-entry__company {
  margin: var(--space-1) 0 0;
  font-weight: 600;
}

.experience-entry__period {
  margin: var(--space-1) 0 var(--space-3);
  font-size: var(--text-sm);
  color: var(--color-text-muted);
}
</style>
