<script setup lang="ts">
import { useHead } from '@unhead/vue'

useHead({
  title: 'Страница не найдена',
  meta: [
    { name: 'description', content: 'Такой страницы нет. Вернитесь на главную.' },
    { name: 'robots', content: 'noindex' },
  ],
})
</script>

<template>
  <section
    class="not-found"
    aria-labelledby="not-found-title"
    data-testid="not-found"
  >
    <div class="not-found__inner">
      <p class="not-found__code">
        404
      </p>
      <h1
        id="not-found-title"
        class="not-found__title"
      >
        Страница не найдена
      </h1>
      <p class="not-found__text">
        Возможно, ссылка устарела или в адресе опечатка.
      </p>
      <Button
        as="router-link"
        to="/"
        label="На главную"
        class="not-found__action"
        data-testid="home-link"
      />
    </div>
  </section>
</template>

<style scoped>
.not-found__inner {
  max-width: var(--container-max);
  margin: 0 auto;
  padding: var(--space-8) var(--container-padding);
}

.not-found__code {
  margin: 0;
  font-size: var(--text-h1);
  font-weight: 700;
  color: var(--color-primary);
}

.not-found__title {
  margin: 0 0 var(--space-3);
  font-size: var(--text-h2);
  color: var(--color-heading);
}

.not-found__text {
  margin: 0 0 var(--space-5);
}

.not-found__action {
  min-height: var(--tap-target);
  text-decoration: none;
}
</style>
