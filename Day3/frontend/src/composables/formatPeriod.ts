// Форматирование периода работы. Свои названия месяцев вместо Intl: одинаковый вывод в Node (пререндер) и браузере.

const MONTHS = [
  'январь',
  'февраль',
  'март',
  'апрель',
  'май',
  'июнь',
  'июль',
  'август',
  'сентябрь',
  'октябрь',
  'ноябрь',
  'декабрь',
] as const

/** "2023-03" → "март 2023". Некорректное значение возвращается как есть. */
export function formatYearMonth(value: string): string {
  const match = /^(\d{4})-(\d{2})$/.exec(value)
  if (match === null) return value
  const [, year, month] = match
  const name = MONTHS[Number(month) - 1]
  return name === undefined || year === undefined ? value : `${name} ${year}`
}

/** Период: "март 2023 — по настоящее время". */
export function formatPeriod(start: string, end: string | null): string {
  return `${formatYearMonth(start)} — ${end === null ? 'по настоящее время' : formatYearMonth(end)}`
}
