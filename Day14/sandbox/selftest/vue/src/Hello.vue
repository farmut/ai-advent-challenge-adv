<script setup lang="ts">
const props = defineProps<{ name: string }>()
</script>

<template>
  <p class="hello">Hello, {{ props.name }}</p>
</template>
