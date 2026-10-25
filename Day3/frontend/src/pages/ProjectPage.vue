<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import MarkdownContent from '@/components/ui/MarkdownContent.vue'
import { getProject, profile } from '@/content'

const route = useRoute()

// Неизвестный slug сюда не попадает: beforeEnter в router/routes.ts отправляет на 404.
const project = computed(() => {
  const slug = route.params.slug
  return typeof slug === 'string' ? getProject(slug) : undefined
})

useHead({
  title: () => (project.value === undefined ? 'Проект' : `${project.value.title} — ${profile.name}`),
  meta: [
    {
      name: 'description',
      content: () => project.value?.summary ?? '',
    },
  ],
})

function linkRel(url: string): string | undefined {
  return url.startsWith('https:') ? 'noopener noreferrer' : undefined
}
</script>

<template>
  <article
    v-if="project"
    class="project"
    data-testid="project-page"
  >
    <div class="project__inner">
      <RouterLink
        :to="{ path: '/', hash: '#projects' }"
        class="project__back"
        data-testid="back-link"
      >
        ← К проектам
      </RouterLink>

      <header class="project__header">
        <h1
          class="project__title"
          data-testid="project-title"
        >
          {{ project.title }}
        </h1>
        <p class="project__summary">
          {{ project.summary }}
        </p>
        <p class="project__meta">
          <span>{{ project.year }}</span>
        </p>
        <ul
          v-if="project.tags.length > 0"
          class="project__tags"
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
        <div
          v-if="project.repo !== null || project.demo !== null"
          class="project__actions"
        >
          <Button
            v-if="project.demo !== null"
            as="a"
            :href="project.demo"
            :rel="linkRel(project.demo)"
            label="Демо"
            class="project__action"
          />
          <Button
            v-if="project.repo !== null"
            as="a"
            :href="project.repo"
            :rel="linkRel(project.repo)"
            label="Исходный код"
            severity="secondary"
            variant="outlined"
            class="project__action"
          />
        </div>
      </header>

      <img
        v-if="project.cover !== null"
        class="project__cover"
        :src="project.cover"
        :alt="project.coverAlt ?? ''"
        width="640"
        height="360"
      >

      <MarkdownContent
        :html="project.html"
        data-testid="project-body"
      />
    </div>
  </article>
</template>

<style scoped>
.project__inner {
  max-width: var(--container-max);
  margin: 0 auto;
  padding: var(--space-5) var(--container-padding) var(--space-8);
}

.project__back {
  display: inline-flex;
  align-items: center;
  min-height: var(--tap-target);
  color: var(--color-primary);
  font-weight: 600;
  text-decoration: none;
}

.project__back:hover {
  text-decoration: underline;
}

.project__header {
  margin: var(--space-4) 0 var(--space-6);
}

.project__title {
  margin: 0 0 var(--space-3);
  font-size: var(--text-h1);
  line-height: 1.15;
  color: var(--color-heading);
}

.project__summary {
  margin: 0 0 var(--space-2);
  font-size: var(--text-lg);
  max-width: 70ch;
}

.project__meta {
  margin: 0 0 var(--space-3);
  color: var(--color-text-muted);
}

.project__tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: 0 0 var(--space-4);
  padding: 0;
  list-style: none;
}

.project__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.project__action {
  min-height: var(--tap-target);
  text-decoration: none;
}

.project__cover {
  display: block;
  width: 100%;
  max-width: 960px;
  height: auto;
  aspect-ratio: 16 / 9;
  margin: 0 0 var(--space-6);
  border-radius: var(--radius);
  border: 1px solid var(--color-border);
  object-fit: cover;
}
</style>
