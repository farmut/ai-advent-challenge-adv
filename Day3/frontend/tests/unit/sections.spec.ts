// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Component, Plugin } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import AboutSection from '@/components/sections/AboutSection.vue'
import ContactSection from '@/components/sections/ContactSection.vue'
import ExperienceSection from '@/components/sections/ExperienceSection.vue'
import HeroSection from '@/components/sections/HeroSection.vue'
import ProjectsSection from '@/components/sections/ProjectsSection.vue'
import SkillsSection from '@/components/sections/SkillsSection.vue'
import { MESSAGES } from '@/composables/useContactForm'
import type { ExperienceItem, Project } from '@/content/types'
import { primeVueOptions } from '@/theme/primevue'

const Empty: Component = { render: () => null }

/** Плагины как в приложении: роутер (нужен RouterLink) и PrimeVue с той же темой. */
function globalConfig(): { plugins: Array<Plugin | [Plugin, ...unknown[]]> } {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: Empty },
      { path: '/projects/:slug', name: 'project', component: Empty },
    ],
  })
  return { plugins: [router, [PrimeVue, primeVueOptions]] }
}

const project = (slug: string, overrides: Partial<Project> = {}): Project => ({
  kind: 'project',
  slug,
  title: `Проект ${slug}`,
  summary: `Описание ${slug}`,
  tags: ['Vue', 'TS'],
  year: 2024,
  cover: null,
  coverAlt: null,
  repo: null,
  demo: null,
  order: 1,
  html: '',
  ...overrides,
})

const job = (id: string, end: string | null): ExperienceItem => ({
  kind: 'experience',
  id,
  company: `Компания ${id}`,
  position: `Должность ${id}`,
  start: '2021-03',
  end,
  html: `<p>Обязанности ${id}</p>`,
})

afterEach(() => {
  vi.useRealTimers()
})

describe('HeroSection', () => {
  it('рендерит имя в единственном h1, роль и локацию', () => {
    const wrapper = mount(HeroSection, { props: { name: 'Тест Тестов', role: 'Роль', location: 'Город' }, global: globalConfig() })
    expect(wrapper.attributes('id')).toBe('hero')
    expect(wrapper.attributes('data-testid')).toBe('section-hero')
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.find('h1').text()).toBe('Тест Тестов')
    expect(wrapper.text()).toContain('Роль')
    expect(wrapper.text()).toContain('Город')
  })

  it('CTA ведут на якоря #projects и #contact', () => {
    const wrapper = mount(HeroSection, { props: { name: 'Н', role: 'Р', location: 'Л' }, global: globalConfig() })
    const hrefs = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(hrefs).toEqual(['/#projects', '/#contact'])
  })
})

describe('AboutSection', () => {
  it('выводит переданный HTML внутри section#about', () => {
    const wrapper = mount(AboutSection, { props: { html: '<p>Био <strong>жирно</strong></p>' }, global: globalConfig() })
    expect(wrapper.find('section#about').exists()).toBe(true)
    expect(wrapper.find('h2').text()).toBe('О себе')
    expect(wrapper.find('.markdown strong').text()).toBe('жирно')
  })
})

describe('SkillsSection', () => {
  it('рендерит группы и навыки', () => {
    const wrapper = mount(SkillsSection, {
      props: {
        skills: [
          { group: 'Языки', items: ['TypeScript', 'CSS'] },
          { group: 'Тесты', items: ['Vitest'] },
        ],
      },
      global: globalConfig(),
    })
    expect(wrapper.findAll('[data-testid="skill-group"]')).toHaveLength(2)
    expect(wrapper.findAll('h3').map((h) => h.text())).toEqual(['Языки', 'Тесты'])
    expect(wrapper.text()).toContain('TypeScript')
    expect(wrapper.text()).toContain('Vitest')
  })

  it('пустой список не ломает разметку', () => {
    const wrapper = mount(SkillsSection, { props: { skills: [] }, global: globalConfig() })
    expect(wrapper.find('section#skills h2').text()).toBe('Навыки')
    expect(wrapper.find('[data-testid="skills-empty"]').exists()).toBe(true)
  })
})

describe('ProjectsSection', () => {
  it('карточка на каждый проект со ссылкой на /projects/<slug>', () => {
    const wrapper = mount(ProjectsSection, { props: { projects: [project('alpha'), project('beta')] }, global: globalConfig() })
    const alpha = wrapper.find('[data-testid="project-card-alpha"]')
    expect(alpha.exists()).toBe(true)
    expect(alpha.find('h3').text()).toBe('Проект alpha')
    expect(alpha.find('a').attributes('href')).toBe('/projects/alpha')
    expect(alpha.text()).toContain('Описание alpha')
    expect(alpha.text()).toContain('2024')
    expect(wrapper.find('[data-testid="project-card-beta"]').exists()).toBe(true)
  })

  it('обложка выводится с alt, если указана', () => {
    const wrapper = mount(ProjectsSection, {
      props: { projects: [project('pic', { cover: '/images/x.svg', coverAlt: 'Описание картинки' })] },
      global: globalConfig(),
    })
    const img = wrapper.find('[data-testid="project-card-pic"] img')
    expect(img.attributes('src')).toBe('/images/x.svg')
    expect(img.attributes('alt')).toBe('Описание картинки')
  })

  it('пустой список проектов не ломает разметку', () => {
    const wrapper = mount(ProjectsSection, { props: { projects: [] }, global: globalConfig() })
    expect(wrapper.find('section#projects').exists()).toBe(true)
    expect(wrapper.find('section#projects h2').text()).toBe('Проекты')
    expect(wrapper.find('[data-testid="projects-empty"]').exists()).toBe(true)
    expect(wrapper.find('ul').exists()).toBe(false)
  })
})

describe('ExperienceSection', () => {
  it('рендерит места работы с периодом', () => {
    const wrapper = mount(ExperienceSection, { props: { items: [job('a', null), job('b', '2022-11')] }, global: globalConfig() })
    const items = wrapper.findAll('[data-testid="experience-item"]')
    expect(items).toHaveLength(2)
    expect(items[0]?.find('h3').text()).toBe('Должность a')
    expect(items[0]?.text()).toContain('март 2021 — по настоящее время')
    expect(items[1]?.text()).toContain('март 2021 — ноябрь 2022')
    expect(items[1]?.text()).toContain('Обязанности b')
  })

  it('пустой список не ломает разметку', () => {
    const wrapper = mount(ExperienceSection, { props: { items: [] }, global: globalConfig() })
    expect(wrapper.find('section#experience h2').text()).toBe('Опыт')
    expect(wrapper.find('[data-testid="experience-empty"]').exists()).toBe(true)
  })
})

describe('ContactSection', () => {
  const links = [
    { label: 'Сайт', url: 'https://example.com/me' },
    { label: 'Почта', url: 'mailto:me@example.com' },
  ]

  async function fillValid(wrapper: VueWrapper): Promise<void> {
    await wrapper.find('[data-testid="field-name"]').setValue('Иван')
    await wrapper.find('[data-testid="field-email"]').setValue('ivan@example.com')
    await wrapper.find('[data-testid="field-message"]').setValue('Здравствуйте, есть задача.')
  }

  it('форма без action/method, подпись-демо видна всегда, внешние ссылки с rel', () => {
    const wrapper = mount(ContactSection, { props: { links }, global: globalConfig() })
    const form = wrapper.find('[data-testid="contact-form"]')
    expect(form.attributes('action')).toBeUndefined()
    expect(form.attributes('method')).toBeUndefined()
    expect(wrapper.find('[data-testid="demo-note"]').text()).toBe('Это демо: сообщение никуда не отправляется')
    const external = wrapper.find('a[href="https://example.com/me"]')
    expect(external.attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.find('a[href="mailto:me@example.com"]').attributes('rel')).toBeUndefined()
  })

  it('ошибка поля появляется после blur', async () => {
    const wrapper = mount(ContactSection, { props: { links }, global: globalConfig() })
    expect(wrapper.find('[data-testid="field-error-email"]').exists()).toBe(false)
    await wrapper.find('[data-testid="field-email"]').setValue('not-an-email')
    await wrapper.find('[data-testid="field-email"]').trigger('blur')
    expect(wrapper.find('[data-testid="field-error-email"]').text()).toBe(MESSAGES.emailInvalid)
    expect(wrapper.find('[data-testid="field-email"]').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('[data-testid="field-error-name"]').exists()).toBe(false)
  })

  it('пустая отправка показывает ошибки под всеми полями', async () => {
    const wrapper = mount(ContactSection, { props: { links }, global: globalConfig() })
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="field-error-name"]').text()).toBe(MESSAGES.nameRequired)
    expect(wrapper.find('[data-testid="field-error-email"]').text()).toBe(MESSAGES.emailRequired)
    expect(wrapper.find('[data-testid="field-error-message"]').text()).toBe(MESSAGES.messageRequired)
    expect(wrapper.find('[data-testid="form-status"]').attributes('data-status')).toBe('idle')
  })

  it('валидная отправка: sending → success, без сетевых запросов', async () => {
    vi.useFakeTimers()
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    const wrapper = mount(ContactSection, { props: { links, formOptions: { delayMs: 800 } }, global: globalConfig() })
    await fillValid(wrapper)

    await wrapper.find('form').trigger('submit')
    const status = wrapper.find('[data-testid="form-status"]')
    expect(status.attributes('role')).toBe('status')
    expect(status.attributes('aria-live')).toBe('polite')
    expect(status.attributes('data-status')).toBe('sending')
    expect(wrapper.find('[data-testid="contact-submit"]').attributes('disabled')).toBeDefined()

    vi.advanceTimersByTime(800)
    await flushPromises()
    expect(status.attributes('data-status')).toBe('success')
    expect(status.text()).toContain('Спасибо')
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('simulate: error показывает сообщение об ошибке', async () => {
    vi.useFakeTimers()
    const wrapper = mount(ContactSection, { props: { links, formOptions: { delayMs: 100, simulate: 'error' } }, global: globalConfig() })
    await fillValid(wrapper)
    await wrapper.find('form').trigger('submit')
    vi.advanceTimersByTime(100)
    await flushPromises()
    const status = wrapper.find('[data-testid="form-status"]')
    expect(status.attributes('data-status')).toBe('error')
    expect(status.text()).toContain('Не удалось отправить')
    expect((wrapper.find('[data-testid="field-name"]').element as HTMLInputElement).value).toBe('Иван') // элемент — input, проверено селектором
  })
})
