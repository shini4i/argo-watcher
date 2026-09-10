import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { blockStorageAccess } from '../test/blockStorage';
import { ThemeModeProvider, useThemeMode } from './ThemeModeProvider';

vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);

const ModeConsumer = () => {
  const { mode, toggleMode } = useThemeMode();
  return (
    <div>
      <span data-testid="mode">{mode}</span>
      <button type="button" onClick={toggleMode}>
        toggle
      </button>
    </div>
  );
};

describe('ThemeModeProvider', () => {
  // This provider wraps the whole app and reads the stored mode while
  // rendering, so an unguarded read is a blank page, not a lost preference.
  it('still renders when the browser blocks storage', async () => {
    const restore = blockStorageAccess();
    try {
      const user = userEvent.setup();
      render(
        <ThemeModeProvider>
          <ModeConsumer />
        </ThemeModeProvider>,
      );

      expect(screen.getByTestId('mode').textContent).toBe('light');
      // The toggle writes, so it must survive the refusal too.
      await user.click(screen.getByRole('button', { name: /toggle/i }));
      expect(screen.getByTestId('mode').textContent).toBe('dark');
    } finally {
      restore();
    }
  });

  it('toggles between light and dark modes', async () => {
    const user = userEvent.setup();
    render(
      <ThemeModeProvider>
        <ModeConsumer />
      </ThemeModeProvider>,
    );

    expect(screen.getByTestId('mode').textContent).toBe('light');
    await user.click(screen.getByRole('button', { name: /toggle/i }));
    expect(screen.getByTestId('mode').textContent).toBe('dark');
  });
});
