<script setup lang="ts">
// Единственный компонент с v-html. HTML приходит только из поля html контент-модуля:
// он собран при сборке через markdown-it (html: false) и очищен sanitize-html (build/content-core.ts).
defineProps<{
  html: string
}>()
</script>

<template>
  <div
    class="markdown"
    v-html="html"
  />
</template>

<style scoped>
.markdown {
  max-width: 70ch;
}

.markdown :deep(p),
.markdown :deep(ul),
.markdown :deep(ol),
.markdown :deep(blockquote),
.markdown :deep(pre) {
  margin: 0 0 var(--space-4);
}

.markdown :deep(:last-child) {
  margin-bottom: 0;
}

.markdown :deep(h2),
.markdown :deep(h3),
.markdown :deep(h4) {
  margin: var(--space-6) 0 var(--space-3);
  color: var(--color-heading);
  line-height: 1.3;
}

.markdown :deep(h2) {
  font-size: var(--text-h3);
}

.markdown :deep(h3),
.markdown :deep(h4) {
  font-size: var(--text-lg);
}

.markdown :deep(ul),
.markdown :deep(ol) {
  padding-left: var(--space-5);
}

.markdown :deep(li + li) {
  margin-top: var(--space-1);
}

.markdown :deep(a) {
  color: var(--color-primary);
  text-underline-offset: 0.15em;
}

.markdown :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.9em;
  padding: 0.1em 0.35em;
  border-radius: 0.25rem;
  background: var(--color-bg-alt);
}

.markdown :deep(pre) {
  overflow-x: auto;
  padding: var(--space-4);
  border-radius: var(--radius);
  background: var(--color-bg-alt);
}

.markdown :deep(pre code) {
  padding: 0;
  background: none;
}

.markdown :deep(blockquote) {
  padding: var(--space-3) var(--space-4);
  border-left: 4px solid var(--color-primary);
  background: var(--color-bg-alt);
  color: var(--color-text-muted);
}
</style>
