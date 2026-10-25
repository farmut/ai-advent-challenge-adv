<script setup lang="ts">
// Обёртка раздела главной: <section> с якорем, заголовком h2 и data-testid для тестов.
const props = withDefaults(
  defineProps<{
    id: string
    title: string
    muted?: boolean
  }>(),
  { muted: false },
)

const titleId = `${props.id}-title`
</script>

<template>
  <section
    :id="id"
    class="page-section"
    :class="{ 'page-section--muted': muted }"
    :aria-labelledby="titleId"
    :data-testid="`section-${id}`"
  >
    <div class="page-section__inner">
      <h2
        :id="titleId"
        class="page-section__title"
      >
        {{ title }}
      </h2>
      <slot />
    </div>
  </section>
</template>

<style scoped>
.page-section {
  padding: var(--space-7) 0;
  scroll-margin-top: calc(var(--header-height, 4rem) + var(--space-2));
}

.page-section--muted {
  background: var(--color-bg-alt);
}

.page-section__inner {
  max-width: var(--container-max);
  margin: 0 auto;
  padding: 0 var(--container-padding);
}

.page-section__title {
  margin: 0 0 var(--space-5);
  font-size: var(--text-h2);
  line-height: 1.2;
  color: var(--color-heading);
}

@media (min-width: 960px) {
  .page-section {
    padding: var(--space-8) 0;
  }
}
</style>
