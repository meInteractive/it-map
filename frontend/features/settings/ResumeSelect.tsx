import { useState } from 'react';
import { useApi } from '../../api/client';

export interface NamedResume {
  id: number;
  name: string;
  markdown: string;
}

export interface Resumes {
  primary: number;
  revision: number;
  legacyRevision: number;
  items: NamedResume[];
}

export function useResumeSelection(enabled = true) {
  const { data, error } = useApi<Resumes>(enabled ? '/api/profile/resumes?view=options' : null);
  const [selected, setSelected] = useState<number>();
  const items = data?.items ?? [];
  const id = items.some((item) => item.id === selected) ? selected : undefined;
  return {
    id,
    control: items.length ? (
      <label className="crm-field">
        <span>Резюме</span>
        <select
          value={id ?? data?.primary}
          onChange={(event) => setSelected(Number(event.target.value))}
        >
          {items.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
      </label>
    ) : error ? (
      <div className="error" role="alert">
        Не удалось загрузить список резюме.
      </div>
    ) : null,
  };
}
