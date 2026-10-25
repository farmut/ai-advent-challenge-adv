import { describe, expect, it } from 'vitest'
import {
  ContentError,
  assertSafeUrl,
  detectContentKind,
  parseContentFile,
  renderMarkdown,
  sanitizeContentHtml,
} from '../../build/content-core'

const assets = new Set(['/images/cover.svg'])
const options = { assetExists: (path: string) => assets.has(path) }

function projectSource(frontmatter: Record<string, string>, body = 'Описание проекта.'): string {
  const lines = Object.entries(frontmatter).map(([key, value]) => `${key}: ${value}`)
  return `---\n${lines.join('\n')}\n---\n${body}\n`
}

const validProject: Record<string, string> = {
  title: 'Трекер задач',
  summary: 'Короткое описание.',
  tags: '[Vue, TypeScript]',
  year: '2025',
  cover: '/images/cover.svg',
  coverAlt: 'Схема доски',
  repo: 'https://example.com/me/tracker',
  demo: 'null',
  order: '1',
}

function withoutField(field: string): Record<string, string> {
  const copy = { ...validProject }
  delete copy[field]
  return copy
}

/** Ловит ContentError и возвращает его; если ошибки нет — тест падает. */
function catchContentError(fn: () => unknown): ContentError {
  try {
    fn()
  } catch (error) {
    if (error instanceof ContentError) return error
    throw error
  }
  throw new Error('ожидалась ContentError, но функция завершилась успешно')
}

describe('detectContentKind', () => {
  it('определяет вид по пути', () => {
    expect(detectContentKind('profile.md')).toBe('profile')
    expect(detectContentKind('experience/acme.md')).toBe('experience')
    expect(detectContentKind('projects/tracker.md')).toBe('project')
  })

  it('неизвестный путь — ошибка с именем файла', () => {
    const error = catchContentError(() => detectContentKind('drafts/x.md'))
    expect(error.message).toContain('drafts/x.md')
  })
})

describe('parseContentFile: проект', () => {
  it('валидный файл → ожидаемый объект', () => {
    const raw = projectSource(validProject, '## Задача\n\nТекст [ссылка](https://example.com/x).')
    expect(parseContentFile('project', 'projects/task-tracker.md', raw, options)).toEqual({
      kind: 'project',
      slug: 'task-tracker',
      title: 'Трекер задач',
      summary: 'Короткое описание.',
      tags: ['Vue', 'TypeScript'],
      year: 2025,
      cover: '/images/cover.svg',
      coverAlt: 'Схема доски',
      repo: 'https://example.com/me/tracker',
      demo: null,
      order: 1,
      html: '<h2>Задача</h2>\n<p>Текст <a href="https://example.com/x" rel="noopener noreferrer">ссылка</a>.</p>\n',
    })
  })

  it.each(['title', 'summary', 'tags', 'year', 'cover', 'coverAlt', 'repo', 'demo', 'order'])(
    'пропущенное поле %s → ошибка с именем файла и поля',
    (field) => {
      const error = catchContentError(() =>
        parseContentFile('project', 'projects/task-tracker.md', projectSource(withoutField(field)), options),
      )
      expect(error.fileName).toBe('projects/task-tracker.md')
      expect(error.field).toBe(field)
      expect(error.message).toContain('projects/task-tracker.md')
      expect(error.message).toContain(field)
    },
  )

  it('неверный тип year → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, year: '"2025"' }), options),
    )
    expect(error.field).toBe('year')
  })

  it('tags не список → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, tags: 'Vue' }), options),
    )
    expect(error.field).toBe('tags')
  })

  it('неизвестное поле → ошибка (опечатки не проходят молча)', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, titel: 'x' }), options),
    )
    expect(error.field).toBe('titel')
  })

  it('slug не по шаблону ^[a-z0-9-]+$ → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/Task_Tracker.md', projectSource(validProject), options),
    )
    expect(error.field).toBe('slug')
  })

  it('cover указывает на несуществующий файл → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, cover: '/images/none.svg' }), options),
    )
    expect(error.field).toBe('cover')
  })

  it('cover без coverAlt → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, coverAlt: 'null' }), options),
    )
    expect(error.field).toBe('coverAlt')
  })

  it('ссылка не на example.com → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, repo: 'https://github.com/x' }), options),
    )
    expect(error.field).toBe('repo')
  })

  it('javascript: в URL frontmatter → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('project', 'projects/a.md', projectSource({ ...validProject, demo: '"javascript:alert(1)"' }), options),
    )
    expect(error.field).toBe('demo')
  })
})

describe('parseContentFile: профиль и опыт', () => {
  const profile = [
    '---',
    'name: Иван Вымыслов',
    'role: Разработчик',
    'location: Город N',
    'fictional: true',
    'skills:',
    '  - group: Языки',
    '    items: [TypeScript]',
    'links:',
    '  - label: Почта',
    '    url: mailto:ivan@example.com',
    '---',
    'Биография.',
  ].join('\n')

  it('валидный профиль', () => {
    expect(parseContentFile('profile', 'profile.md', profile, options)).toEqual({
      kind: 'profile',
      name: 'Иван Вымыслов',
      role: 'Разработчик',
      location: 'Город N',
      fictional: true,
      skills: [{ group: 'Языки', items: ['TypeScript'] }],
      links: [{ label: 'Почта', url: 'mailto:ivan@example.com' }],
      html: '<p>Биография.</p>\n',
    })
  })

  it('fictional: false → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('profile', 'profile.md', profile.replace('fictional: true', 'fictional: false'), options),
    )
    expect(error.field).toBe('fictional')
  })

  it('mailto на чужой домен → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('profile', 'profile.md', profile.replace('ivan@example.com', 'ivan@gmail.com'), options),
    )
    expect(error.field).toBe('links[0].url')
  })

  const experience = (start: string, end: string) =>
    `---\ncompany: ООО Пример\nposition: Разработчик\nstart: ${start}\nend: ${end}\n---\nОбязанности.\n`

  it('валидный опыт, end: null', () => {
    expect(parseContentFile('experience', 'experience/acme.md', experience('"2023-03"', 'null'), options)).toEqual({
      kind: 'experience',
      id: 'acme',
      company: 'ООО Пример',
      position: 'Разработчик',
      start: '2023-03',
      end: null,
      html: '<p>Обязанности.</p>\n',
    })
  })

  it('start не в формате YYYY-MM → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('experience', 'experience/acme.md', experience('"2023-13"', 'null'), options),
    )
    expect(error.field).toBe('start')
  })

  it('end раньше start → ошибка', () => {
    const error = catchContentError(() =>
      parseContentFile('experience', 'experience/acme.md', experience('"2023-03"', '"2022-01"'), options),
    )
    expect(error.field).toBe('end')
  })

  it('пропущенное поле company → ошибка с именем файла', () => {
    const raw = '---\nposition: Разработчик\nstart: "2023-03"\nend: null\n---\nТекст\n'
    const error = catchContentError(() => parseContentFile('experience', 'experience/acme.md', raw, options))
    expect(error.message).toContain('experience/acme.md')
    expect(error.field).toBe('company')
  })
})

describe('frontmatter: безопасность', () => {
  it('---js frontmatter отклоняется и не выполняется', () => {
    const marker = '__day3_eval_marker__'
    const raw = `---js\n{ title: (globalThis["${marker}"] = true, "x") }\n---\nТекст\n`
    const error = catchContentError(() => parseContentFile('project', 'projects/a.md', raw, options))
    expect(error.field).toBe('frontmatter')
    expect(Reflect.has(globalThis, marker)).toBe(false)
  })

  it('файл без frontmatter → ошибка', () => {
    const error = catchContentError(() => parseContentFile('project', 'projects/a.md', 'Просто текст', options))
    expect(error.field).toBe('frontmatter')
  })
})

describe('renderMarkdown: очистка', () => {
  it('<script> не превращается в тег', () => {
    const html = renderMarkdown('a.md', 'До\n\n<script>alert(1)</script>\n\nПосле')
    expect(html).not.toMatch(/<script/i)
    expect(html).toContain('После')
  })

  it('атрибут onerror= не появляется ни в одном теге', () => {
    const html = renderMarkdown('a.md', 'Текст <img src=x onerror=alert(1)> и <b onmouseover="x()">жирный</b>')
    expect(html).not.toMatch(/<img/i)
    expect(html).not.toMatch(/<[a-z][^>]*\son[a-z]+\s*=/i)
  })

  it('ссылка javascript: не становится href', () => {
    const html = renderMarkdown('a.md', '[нажми](javascript:alert(1)) и <javascript:alert(2)>')
    expect(html).not.toMatch(/href\s*=\s*["']?\s*javascript:/i)
    expect(html).not.toMatch(/<a\s/i)
  })

  it('h1 в теле → ошибка сборки', () => {
    const error = catchContentError(() => renderMarkdown('projects/a.md', '# Заголовок'))
    expect(error.field).toBe('body')
    expect(error.message).toContain('projects/a.md')
  })

  it('картинка в теле → ошибка сборки', () => {
    const error = catchContentError(() => renderMarkdown('projects/a.md', '![alt](https://example.com/a.png)'))
    expect(error.field).toBe('body')
  })

  it('h5 и h6 понижаются до h4', () => {
    const html = renderMarkdown('a.md', '##### Пять\n\n###### Шесть')
    expect(html).toBe('<h4>Пять</h4>\n<h4>Шесть</h4>\n')
  })

  it('ссылка в теле не на example.com → ошибка сборки', () => {
    const error = catchContentError(() => renderMarkdown('a.md', '[x](https://evil.test/)'))
    expect(error.field).toBe('body')
  })

  it('якорные и mailto-ссылки остаются, внешним добавляется rel', () => {
    const html = renderMarkdown('a.md', '[якорь](#contact), [почта](mailto:a@example.com), [сайт](https://example.com/p)')
    expect(html).toContain('<a href="#contact">якорь</a>')
    expect(html).toContain('<a href="mailto:a@example.com">почта</a>')
    expect(html).toContain('<a href="https://example.com/p" rel="noopener noreferrer">сайт</a>')
  })
})

describe('sanitizeContentHtml', () => {
  it('вырезает script целиком, обработчики событий и javascript:', () => {
    const html = sanitizeContentHtml(
      '<p onclick="x()">a</p><script>alert(1)</script><img src="x" onerror="alert(1)"><a href="javascript:alert(1)">b</a>',
    )
    expect(html).toBe('<p>a</p><a>b</a>')
  })

  it('оставляет только теги из белого списка', () => {
    const html = sanitizeContentHtml('<h1>x</h1><h2>y</h2><iframe src="https://example.com"></iframe><div>z</div>')
    expect(html).toBe('x<h2>y</h2>z')
  })

  it('протокол-относительные и data: ссылки отбрасываются', () => {
    expect(sanitizeContentHtml('<a href="//evil.test">a</a>')).toBe('<a>a</a>')
    expect(sanitizeContentHtml('<a href="data:text/html,x">a</a>')).toBe('<a>a</a>')
  })

  it('https-ссылке добавляется rel="noopener noreferrer", лишние атрибуты удаляются', () => {
    expect(sanitizeContentHtml('<a href="https://example.com/x" target="_blank" rel="opener">a</a>')).toBe(
      '<a href="https://example.com/x" rel="noopener noreferrer">a</a>',
    )
  })
})

describe('assertSafeUrl', () => {
  const check = (value: string) => assertSafeUrl('f.md', 'url', value, { allowHash: true })

  it('разрешает https://example.com, поддомены, mailto @example.com и #', () => {
    expect(check('https://example.com/a')).toBe('https://example.com/a')
    expect(check('https://www.example.com/a')).toBe('https://www.example.com/a')
    expect(check('mailto:me@example.com')).toBe('mailto:me@example.com')
    expect(check('#')).toBe('#')
  })

  it.each([
    'http://example.com/a',
    'https://example.com.evil.test/a',
    'https://user:pass@example.com/a',
    'javascript:alert(1)',
    'data:text/html,x',
    'mailto:me@example.com?body=x',
    'ftp://example.com/a',
    'не url',
  ])('отклоняет %s', (value) => {
    expect(() => check(value)).toThrow(ContentError)
  })
})
