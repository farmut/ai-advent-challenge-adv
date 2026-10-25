<script setup lang="ts">
defineProps<{
  name: string
  role: string
  location: string
}>()
</script>

<template>
  <section
    id="hero"
    class="hero"
    aria-labelledby="hero-title"
    data-testid="section-hero"
  >
    <div class="hero__inner">
      <p class="hero__eyebrow">
        Привет, меня зовут
      </p>
      <h1
        id="hero-title"
        class="hero__title"
      >
        {{ name }}
      </h1>
      <p class="hero__role">
        {{ role }}
      </p>
      <p class="hero__location">
        {{ location }}
      </p>
      <div class="hero__actions">
        <Button
          as="router-link"
          :to="{ path: '/', hash: '#projects' }"
          label="Смотреть проекты"
          class="hero__action"
        />
        <Button
          as="router-link"
          :to="{ path: '/', hash: '#contact' }"
          label="Написать мне"
          severity="secondary"
          variant="outlined"
          class="hero__action"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.hero {
  padding: var(--space-8) 0 var(--space-7);
  scroll-margin-top: calc(var(--header-height, 4rem) + var(--space-2));
}

.hero__inner {
  max-width: var(--container-max);
  margin: 0 auto;
  padding: 0 var(--container-padding);
}

.hero__eyebrow {
  margin: 0 0 var(--space-2);
  color: var(--color-primary);
  font-weight: 600;
}

.hero__title {
  margin: 0 0 var(--space-3);
  font-size: var(--text-h1);
  line-height: 1.1;
  color: var(--color-heading);
}

.hero__role {
  margin: 0 0 var(--space-1);
  font-size: var(--text-lg);
  font-weight: 600;
}

.hero__location {
  margin: 0 0 var(--space-6);
  color: var(--color-text-muted);
}

.hero__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.hero__action {
  min-height: var(--tap-target);
  text-decoration: none;
}

@media (min-width: 960px) {
  .hero {
    padding: var(--space-9) 0 var(--space-8);
  }
}
</style>
