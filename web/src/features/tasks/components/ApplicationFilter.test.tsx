import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Task } from '../../../data/types';
import { ApplicationFilter, normalizeApplicationFilterValue, readInitialApplication } from './ApplicationFilter';

type AutocompleteProps = {
  options: string[];
  value: string;
  onChange: (event: unknown, newValue?: string | null) => void;
  renderInput: (params: Record<string, unknown>) => React.ReactNode;
};

const localStorageStore = new Map<string, string>();

const localStorageStub = {
  getItem: vi.fn((key: string) => localStorageStore.get(key) ?? null),
  setItem: vi.fn((key: string, value: string) => {
    localStorageStore.set(key, value);
  }),
  removeItem: vi.fn((key: string) => {
    localStorageStore.delete(key);
  }),
};

vi.mock('../../../shared/utils', () => ({
  getBrowserWindow: () => ({ localStorage: localStorageStub }),
}));

let lastAutocompleteProps: AutocompleteProps | undefined;

const { AutocompleteMock, TextFieldMock } = vi.hoisted(() => {
  const autocomplete = (props: AutocompleteProps) => {
    lastAutocompleteProps = props;
    return (
      <div data-testid="autocomplete">
        <div data-testid="options">{props.options.join(',')}</div>
        <button type="button" onClick={() => props.onChange(null, 'frontend')}>
          select-frontend
        </button>
        <button type="button" onClick={() => props.onChange(null, '')}>
          clear
        </button>
        {props.renderInput({ 'data-testid': 'text-field-props' })}
      </div>
    );
  };

  const textField = (props: Record<string, unknown>) => (
    <div data-testid="text-field" data-label={props.label} data-placeholder={props.placeholder} />
  );

  return { AutocompleteMock: autocomplete, TextFieldMock: textField };
});

vi.mock('@mui/material', () => ({
  Autocomplete: AutocompleteMock,
  TextField: TextFieldMock,
}));

let taskIdCounter = 0;
const createTask = (overrides: Partial<Task>): Task => ({
  id: overrides.id ?? `task-${++taskIdCounter}`,
  created: overrides.created ?? 0,
  updated: overrides.updated ?? 0,
  app: overrides.app ?? '',
  author: overrides.author ?? '',
  project: overrides.project ?? '',
  images: overrides.images ?? [],
  status: overrides.status,
  status_reason: overrides.status_reason,
});

describe('ApplicationFilter', () => {
  beforeEach(() => {
    localStorageStore.clear();
    localStorageStub.getItem.mockClear();
    localStorageStub.setItem.mockClear();
    localStorageStub.removeItem.mockClear();
    lastAutocompleteProps = undefined;
  });

  it('normalizes filter values', () => {
    expect(normalizeApplicationFilterValue()).toBe('');
    expect(normalizeApplicationFilterValue(null)).toBe('');
    expect(normalizeApplicationFilterValue('   ')).toBe('');
    expect(normalizeApplicationFilterValue('null')).toBe('');
    expect(normalizeApplicationFilterValue('frontend')).toBe('frontend');
  });

  it('reads the initial value from localStorage', () => {
    localStorageStore.set('custom', 'app-one');
    expect(readInitialApplication('custom')).toBe('app-one');
    expect(localStorageStub.getItem).toHaveBeenCalledWith('custom');
  });

  it('derives unique sorted options from task records', () => {
    render(
      <ApplicationFilter
        records={[
          createTask({ app: 'api' }),
          createTask({ app: 'frontend' }),
          createTask({ app: 'api' }),
          createTask({ app: ' ' }),
          createTask({ app: ' NULL ' }),
        ]}
        value=""
        onChange={vi.fn()}
      />,
    );

    expect(screen.getByTestId('options')).toHaveTextContent('api,frontend');
    const textField = screen.getByTestId('text-field');
    expect(textField.dataset.label).toBe('Application');
  });

  it('persists selection changes and clears storage when emptied', () => {
    const handleChange = vi.fn();

    render(
      <ApplicationFilter
        records={[createTask({ app: 'bootstrap' })]}
        value=""
        onChange={handleChange}
        storageKey="custom.storage"
      />,
    );

    const props = lastAutocompleteProps!;
    props.onChange(null, 'frontend');
    expect(localStorageStub.setItem).toHaveBeenCalledWith('custom.storage', 'frontend');
    expect(handleChange).toHaveBeenCalledWith('frontend');

    props.onChange(null, '');
    expect(localStorageStub.removeItem).toHaveBeenCalledWith('custom.storage');
    expect(handleChange).toHaveBeenLastCalledWith('');
  });
});
