import { mount } from '@vue/test-utils'
import Hello from './Hello.vue'

describe('Hello', () => {
  it('renders the name', () => {
    expect(mount(Hello, { props: { name: 'sandbox' } }).text()).toBe('Hello, sandbox')
  })
})
