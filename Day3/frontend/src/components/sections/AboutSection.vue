<script setup lang="ts">
import MarkdownContent from '@/components/ui/MarkdownContent.vue'
import PageSection from '@/components/ui/PageSection.vue'

defineProps<{
  html: string
}>()
</script>

<template>
  <PageSection
    id="about"
    title="О себе"
    muted
  >
    <MarkdownContent :html="html" />
  </PageSection>
</template>
