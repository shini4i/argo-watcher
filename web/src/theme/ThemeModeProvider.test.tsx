import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
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
  it('toggles between light and dark modes', async () => {
    const user = userEvent.setup();
    render(
      <ThemeModeProvider>
        <ModeConsumer />
      </ThemeModeProvider>,
    );

    expect(screen.getByTestId('mode').textContent).toBe('light');
    await act(async () => {
      await user.click(screen.getByRole('button', { name: /toggle/i }));
    });
    expect(screen.getByTestId('mode').textContent).toBe('dark');
  });
});
