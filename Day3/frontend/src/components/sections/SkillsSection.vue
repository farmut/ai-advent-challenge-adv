<script setup lang="ts">
import PageSection from '@/components/ui/PageSection.vue'
import type { SkillGroup } from '@/content/types'

defineProps<{
  skills: readonly SkillGroup[]
}>()
</script>

<template>
  <PageSection
    id="skills"
    title="Навыки"
  >
    <p
      v-if="skills.length === 0"
      class="skills__empty"
      data-testid="skills-empty"
    >
      Навыки пока не добавлены.
    </p>
    <div
      v-else
      class="skills"
    >
      <div
        v-for="group in skills"
        :key="group.group"
        class="skills__group"
        data-testid="skill-group"
      >
        <h3 class="skills__title">
          {{ group.group }}
        </h3>
        <ul class="skills__list">
          <li
            v-for="item in group.items"
            :key="item"
          >
            <Tag :value="item" />
          </li>
        </ul>
      </div>
    </div>
  </PageSection>
</template>

<style scoped>
.skills {
  display: grid;
  gap: var(--space-5);
}

.skills__title {
  margin: 0 0 var(--space-3);
  font-size: var(--text-lg);
  color: var(--color-heading);
}

.skills__list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.skills__empty {
  margin: 0;
  color: var(--color-text-muted);
}

@media (min-width: 640px) {
  .skills {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1200px) {
  .skills {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
</style>
