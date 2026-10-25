<script setup lang="ts">
import type { Project } from '@/content/types'

defineProps<{
  project: Project
}>()
</script>

<template>
  <Card
    class="project-card"
    :data-testid="`project-card-${project.slug}`"
  >
    <template
      v-if="project.cover !== null"
      #header
    >
      <img
        class="project-card__cover"
        :src="project.cover"
        :alt="project.coverAlt ?? ''"
        width="640"
        height="360"
        loading="lazy"
        decoding="async"
      >
    </template>
    <template #title>
      <h3 class="project-card__title">
        <RouterLink
          :to="{ name: 'project', params: { slug: project.slug } }"
          class="project-card__link"
        >
          {{ project.title }}
        </RouterLink>
      </h3>
    </template>
    <template #subtitle>
      <span class="project-card__year">{{ project.year }}</span>
    </template>
    <template #content>
      <p class="project-card__summary">
        {{ project.summary }}
      </p>
      <ul
        v-if="project.tags.length > 0"
        class="project-card__tags"
        aria-label="Технологии"
      >
        <li
          v-for="tag in project.tags"
          :key="tag"
        >
          <Tag
            :value="tag"
            severity="secondary"
          />
        </li>
      </ul>
    </template>
  </Card>
</template>

<style scoped>
.project-card {
  position: relative;
  height: 100%;
  overflow: hidden;
  transition: box-shadow 0.2s ease;
}

.project-card:hover,
.project-card:focus-within {
  box-shadow: 0 0 0 2px var(--color-primary);
}

.project-card__cover {
  display: block;
  width: 100%;
  height: auto;
  aspect-ratio: 16 / 9;
  object-fit: cover;
}

.project-card__title {
  margin: 0;
  font-size: var(--text-h3);
  line-height: 1.3;
}

.project-card__link {
  color: var(--color-heading);
  text-decoration: none;
}

/* Растянутая ссылка: вся карточка кликабельна, в дереве доступности одна ссылка. */
.project-card__link::after {
  content: '';
  position: absolute;
  inset: 0;
}

.project-card__link:focus-visible {
  outline: none;
}

.project-card__year {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
}

.project-card__summary {
  margin: 0 0 var(--space-4);
}

.project-card__tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
</style>
