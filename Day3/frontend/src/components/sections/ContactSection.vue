<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import PageSection from '@/components/ui/PageSection.vue'
import { useContactForm } from '@/composables/useContactForm'
import type { ContactField, UseContactFormOptions } from '@/composables/useContactForm'
import type { ProfileLink } from '@/content/types'

const props = withDefaults(
  defineProps<{
    links: readonly ProfileLink[]
    /** Параметры имитации отправки. В тестах — simulate: 'error' для проверки UI ошибки. */
    formOptions?: UseContactFormOptions
  }>(),
  { formOptions: () => ({}) },
)

const { values, visibleErrors, status, firstInvalidField, onBlur, submit } = useContactForm(props.formOptions)

// Без JS форма не работает, поэтому кнопка активируется только после монтирования.
const mounted = ref(false)
onMounted(() => {
  mounted.value = true
})

const sending = computed(() => status.value === 'sending')

const FIELD_IDS: Record<ContactField, string> = {
  name: 'contact-name',
  email: 'contact-email',
  message: 'contact-message',
}

// Message по умолчанию — role="alert" + aria-live="assertive". Ошибки полей связаны с полем через
// aria-describedby, статус отправки объявляет общий role="status", вложенные live-регионы не нужны.
const quietMessagePt = { root: { role: undefined, 'aria-live': undefined } }

function setField(field: ContactField, value: string | undefined): void {
  values[field] = value ?? ''
}

function errorId(field: ContactField): string | undefined {
  return visibleErrors.value[field] === undefined ? undefined : `${FIELD_IDS[field]}-error`
}

function isExternal(url: string): boolean {
  return url.startsWith('https:')
}

async function onSubmit(): Promise<void> {
  const pending = submit()
  const invalid = firstInvalidField.value
  if (status.value !== 'sending' && invalid !== undefined) {
    await nextTick()
    document.getElementById(FIELD_IDS[invalid])?.focus()
  }
  await pending
}
</script>

<template>
  <PageSection
    id="contact"
    title="Контакты"
    muted
  >
    <div class="contact">
      <div class="contact__intro">
        <p class="contact__lead">
          Есть задача или вопрос? Напишите через форму или по ссылкам ниже.
        </p>
        <ul
          v-if="links.length > 0"
          class="contact__links"
          aria-label="Ссылки"
        >
          <li
            v-for="link in links"
            :key="link.url"
          >
            <a
              class="contact__link"
              :href="link.url"
              :rel="isExternal(link.url) ? 'noopener noreferrer' : undefined"
            >{{ link.label }}</a>
          </li>
        </ul>
      </div>

      <form
        class="contact__form"
        novalidate
        data-testid="contact-form"
        aria-describedby="demo-note"
        @submit.prevent="onSubmit"
      >
        <div class="contact__field">
          <label
            :for="FIELD_IDS.name"
            class="contact__label"
          >Имя</label>
          <InputText
            :id="FIELD_IDS.name"
            :model-value="values.name"
            name="name"
            autocomplete="name"
            data-testid="field-name"
            fluid
            :invalid="visibleErrors.name !== undefined"
            :aria-invalid="visibleErrors.name !== undefined"
            :aria-describedby="errorId('name')"
            @update:model-value="setField('name', $event)"
            @blur="onBlur('name')"
          />
          <Message
            v-if="visibleErrors.name"
            :id="errorId('name')"
            severity="error"
            size="small"
            variant="simple"
            data-testid="field-error-name"
            :pt="quietMessagePt"
          >
            {{ visibleErrors.name }}
          </Message>
        </div>

        <div class="contact__field">
          <label
            :for="FIELD_IDS.email"
            class="contact__label"
          >Email</label>
          <InputText
            :id="FIELD_IDS.email"
            :model-value="values.email"
            type="email"
            name="email"
            autocomplete="email"
            inputmode="email"
            data-testid="field-email"
            fluid
            :invalid="visibleErrors.email !== undefined"
            :aria-invalid="visibleErrors.email !== undefined"
            :aria-describedby="errorId('email')"
            @update:model-value="setField('email', $event)"
            @blur="onBlur('email')"
          />
          <Message
            v-if="visibleErrors.email"
            :id="errorId('email')"
            severity="error"
            size="small"
            variant="simple"
            data-testid="field-error-email"
            :pt="quietMessagePt"
          >
            {{ visibleErrors.email }}
          </Message>
        </div>

        <div class="contact__field">
          <label
            :for="FIELD_IDS.message"
            class="contact__label"
          >Сообщение</label>
          <Textarea
            :id="FIELD_IDS.message"
            :model-value="values.message"
            name="message"
            rows="5"
            data-testid="field-message"
            fluid
            :invalid="visibleErrors.message !== undefined"
            :aria-invalid="visibleErrors.message !== undefined"
            :aria-describedby="errorId('message')"
            @update:model-value="setField('message', $event)"
            @blur="onBlur('message')"
          />
          <Message
            v-if="visibleErrors.message"
            :id="errorId('message')"
            severity="error"
            size="small"
            variant="simple"
            data-testid="field-error-message"
            :pt="quietMessagePt"
          >
            {{ visibleErrors.message }}
          </Message>
        </div>

        <Button
          type="submit"
          label="Отправить"
          class="contact__submit"
          data-testid="contact-submit"
          :disabled="!mounted || sending"
          :loading="sending"
        />

        <div
          class="contact__status"
          role="status"
          aria-live="polite"
          data-testid="form-status"
          :data-status="status"
        >
          <span v-if="status === 'sending'">Отправляем…</span>
          <Message
            v-else-if="status === 'success'"
            severity="success"
            :pt="quietMessagePt"
          >
            Спасибо! Сообщение «отправлено».
          </Message>
          <Message
            v-else-if="status === 'error'"
            severity="error"
            :pt="quietMessagePt"
          >
            Не удалось отправить сообщение. Попробуйте ещё раз.
          </Message>
        </div>

        <p
          id="demo-note"
          class="contact__note"
          data-testid="demo-note"
        >
          Это демо: сообщение никуда не отправляется
        </p>
      </form>
    </div>
  </PageSection>
</template>

<style scoped>
.contact {
  display: grid;
  gap: var(--space-6);
}

.contact__lead {
  margin: 0 0 var(--space-4);
}

.contact__links {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}

.contact__link {
  display: inline-flex;
  align-items: center;
  min-height: var(--tap-target);
  color: var(--color-primary);
  font-weight: 600;
  text-underline-offset: 0.15em;
}

.contact__form {
  display: grid;
  gap: var(--space-4);
  width: 100%;
  max-width: var(--form-max);
}

.contact__field {
  display: grid;
  gap: var(--space-2);
}

.contact__label {
  font-weight: 600;
  color: var(--color-heading);
}

.contact__submit {
  justify-self: start;
  min-height: var(--tap-target);
  min-width: 10rem;
}

.contact__note {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
}

@media (min-width: 960px) {
  .contact {
    grid-template-columns: minmax(0, 1fr) minmax(0, var(--form-max));
    align-items: start;
  }
}
</style>
