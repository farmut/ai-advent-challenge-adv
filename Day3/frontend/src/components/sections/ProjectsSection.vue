<script setup lang="ts">
import PageSection from '@/components/ui/PageSection.vue'
import ProjectCard from '@/components/ui/ProjectCard.vue'
import type { Project } from '@/content/types'

defineProps<{
  projects: readonly Project[]
}>()
</script>

<template>
  <PageSection
    id="projects"
    title="Проекты"
    muted
  >
    <p
      v-if="projects.length === 0"
      class="projects__empty"
      data-testid="projects-empty"
    >
      Проекты пока не добавлены.
    </p>
    <ul
      v-else
      class="projects"
    >
      <li
        v-for="project in projects"
        :key="project.slug"
        class="projects__item"
      >
        <ProjectCard :project="project" />
      </li>
    </ul>
  </PageSection>
</template>

<style scoped>
.projects {
  display: grid;
  gap: var(--space-5);
  margin: 0;
  padding: 0;
  list-style: none;
}

.projects__empty {
  margin: 0;
  color: var(--color-text-muted);
}

@media (min-width: 640px) {
  .projects {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 960px) {
  .projects {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
</style>
