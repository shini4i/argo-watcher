import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SearchInput } from './SearchInput';
import { TaskListProvider, useTaskListContext } from './TaskListContext';

const PausedReasonsProbe = () => {
  const { state } = useTaskListContext();
  return (
    <div data-testid="paused-reasons">{Array.from(state.pausedReasons).sort((a, b) => a.localeCompare(b)).join(',')}</div>
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

const setViewportWide = (wide: boolean) => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query: string) => ({
      matches: wide && query.includes('1200'),
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });
};

describe('SearchInput', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('wide viewport (≥1200 px)', () => {
    beforeEach(() => setViewportWide(true));

    it('renders the input directly without an Open button', () => {
      renderWith('');
      expect(screen.getByLabelText('Search tasks')).toBeInTheDocument();
      expect(screen.queryByLabelText('Open search')).toBeNull();
    });

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

  describe('narrow viewport (<1200 px)', () => {
    beforeEach(() => setViewportWide(false));

    it('starts collapsed when there is no value', () => {
      renderWith('');
      expect(screen.queryByLabelText('Search tasks')).toBeNull();
      expect(screen.getByLabelText('Open search')).toBeInTheDocument();
    });

    it('expands and focuses the input when the trigger is clicked', async () => {
      renderWith('');
      fireEvent.click(screen.getByLabelText('Open search'));

      const input = await screen.findByLabelText('Search tasks');
      await waitFor(() => expect(document.activeElement).toBe(input));
    });

    it('stays expanded when a value is present', () => {
      renderWith('checkout');
      expect(screen.getByLabelText('Search tasks')).toBeInTheDocument();
      expect(screen.queryByLabelText('Open search')).toBeNull();
    });

    it('collapses back to the icon on blur when the draft is empty', async () => {
      renderWith('');
      fireEvent.click(screen.getByLabelText('Open search'));

      const input = await screen.findByLabelText('Search tasks');
      act(() => {
        (input as HTMLInputElement).focus();
      });
      act(() => {
        (input as HTMLInputElement).blur();
      });

      await waitFor(() => expect(screen.queryByLabelText('Search tasks')).toBeNull());
      expect(screen.getByLabelText('Open search')).toBeInTheDocument();
    });

    it('does not collapse when viewport crosses 1200px during focused keystroke (focus gate)', async () => {
      renderWith('');
      fireEvent.click(screen.getByLabelText('Open search'));

      const input = await screen.findByLabelText('Search tasks');
      act(() => {
        (input as HTMLInputElement).focus();
      });

      fireEvent.change(input, { target: { value: 'checkout' } });
      expect(screen.getByLabelText('Search tasks')).toBeInTheDocument();

      act(() => {
        setViewportWide(true);
        window.dispatchEvent(new Event('resize'));
      });

      await waitFor(() => {
        expect(screen.getByLabelText('Search tasks')).toBeInTheDocument();
      });

      act(() => {
        setViewportWide(false);
        window.dispatchEvent(new Event('resize'));
      });

      await waitFor(() => {
        expect(screen.getByLabelText('Search tasks')).toBeInTheDocument();
      });

      act(() => {
        (input as HTMLInputElement).blur();
      });

      fireEvent.change(input, { target: { value: '' } });

      await waitFor(() => expect(screen.queryByLabelText('Search tasks')).toBeNull());
    });
  });
});
