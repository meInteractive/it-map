export const settingsSections = (admin: boolean) => [
  ['general', 'Общие'], ['resume', 'Резюме'],
  ...(admin ? [['roles', 'Роли'], ['prompts', 'Промты'], ['parsing', 'Парсинг вакансий'], ['ai', 'Нейросети'], ['docs', 'Документация']] : []),
] as const;
