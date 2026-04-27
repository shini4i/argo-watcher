import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SearchInput } from './SearchInput';
import { TaskListProvider, useTaskListContext } from './TaskListContext';

const PausedReasonsProbe = () => {
  const { state } = useTaskListContext();
  return (
    <div data-testid="paused-reasons">{Array.from(state.pausedReasons).sort().join(',')}</div>
  );
};

const renderWith = (value = '', debounceMs = 50) => {
  const onChange = vi.fn();
  const utils = render(
    <TaskListProvider>
      <SearchInput value={value} onChange={onChange} debounceMs={debounceMs} />
      <PausedReasonsProbe />
    </TaskListProvider>,
  );
  return { ...utils, onChange };
};

describe('SearchInput', () => {
  it('debounces onChange and emits the latest draft once', async () => {
    const { onChange } = renderWith('', 50);
    const input = screen.getByLabelText('Search tasks');

    fireEvent.change(input, { target: { value: 'a' } });
    fireEvent.change(input, { target: { value: 'ab' } });
    fireEvent.change(input, { target: { value: 'abc' } });

    expect(onChange).not.toHaveBeenCalled();
    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    expect(onChange).toHaveBeenCalledWith('abc');
  });

  it('registers "search" pause reason on focus and releases it after blur grace', async () => {
    renderWith('', 50);
    const input = screen.getByLabelText('Search tasks');

    expect(screen.getByTestId('paused-reasons').textContent).toBe('');

    act(() => {
      (input as HTMLInputElement).focus();
    });
    await waitFor(() =>
      expect(screen.getByTestId('paused-reasons').textContent).toBe('search'),
    );

    act(() => {
      (input as HTMLInputElement).blur();
    });
    // Still paused immediately after blur — grace period absorbs trailing debounce.
    expect(screen.getByTestId('paused-reasons').textContent).toBe('search');

    await waitFor(
      () => expect(screen.getByTestId('paused-reasons').textContent).toBe(''),
      { timeout: 500 },
    );
  });
});
