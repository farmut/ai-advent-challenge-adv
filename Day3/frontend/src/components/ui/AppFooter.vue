<template>
  <footer class="app-footer">
    <div class="app-footer__inner">
      <p
        class="app-footer__note"
        data-testid="fictional-note"
      >
        Демонстрационный сайт, персона вымышлена
      </p>
    </div>
  </footer>
</template>

<style scoped>
.app-footer {
  border-top: 1px solid var(--color-border);
  background: var(--color-bg-alt);
}

.app-footer__inner {
  max-width: var(--container-max);
  margin: 0 auto;
  padding: var(--space-5) var(--container-padding);
}

.app-footer__note {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
}
</style>
