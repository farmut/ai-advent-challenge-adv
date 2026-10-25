<script setup lang="ts">
import { useHead } from '@unhead/vue'
import AboutSection from '@/components/sections/AboutSection.vue'
import ContactSection from '@/components/sections/ContactSection.vue'
import ExperienceSection from '@/components/sections/ExperienceSection.vue'
import HeroSection from '@/components/sections/HeroSection.vue'
import ProjectsSection from '@/components/sections/ProjectsSection.vue'
import SkillsSection from '@/components/sections/SkillsSection.vue'
import { experience, profile, projects } from '@/content'

useHead({
  title: `${profile.name} — ${profile.role}`,
  meta: [
    {
      name: 'description',
      content: `${profile.name}, ${profile.role.toLowerCase()}. Проекты, навыки, опыт и контакты.`,
    },
  ],
})
</script>

<template>
  <div class="home">
    <HeroSection
      :name="profile.name"
      :role="profile.role"
      :location="profile.location"
    />
    <AboutSection :html="profile.html" />
    <SkillsSection :skills="profile.skills" />
    <ProjectsSection :projects="projects" />
    <ExperienceSection :items="experience" />
    <ContactSection :links="profile.links" />
  </div>
</template>
