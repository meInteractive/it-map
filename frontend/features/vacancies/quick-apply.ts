import type { ApplicationCreationContext, Vacancy } from '../../../shared/contracts';
import { ApiError, mutate, request } from '../../api/client';

export type QuickApplyIntent = {
  id: number;
  companyName: string;
  key: string;
  context?: string;
};

export function createQuickApplyIntent(vacancy: Vacancy): QuickApplyIntent {
  return { id: vacancy.id, companyName: vacancy.companyName, key: crypto.randomUUID() };
}

export async function quickApply(intent: QuickApplyIntent, isActive: () => boolean) {
  if (!intent.context) {
    const response = await request<ApplicationCreationContext>('/api/applications/create-context');
    if (!isActive()) return;
    if (
      typeof response?.creationContext !== 'string' ||
      !/^ac1\.[A-Za-z0-9_-]{43}$/.test(response.creationContext)
    )
      throw new ApiError('Не удалось подготовить создание отклика.', 503);
    intent.context = response.creationContext;
  }
  await mutate('/api/vacancies/' + intent.id + '/apply/create', 'POST', {
    companyName: intent.companyName,
    idempotencyKey: intent.key,
    creationContext: intent.context,
  });
  return true;
}
