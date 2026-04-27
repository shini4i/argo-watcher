import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ActiveFilterBar } from './ActiveFilterBar';

describe('ActiveFilterBar', () => {
  it('renders nothing when no chips are active', () => {
    const { container } = render(<ActiveFilterBar chips={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders chip prefix and value', () => {
    render(
      <ActiveFilterBar
        chips={[{ key: 'app', labelPrefix: 'app', labelValue: 'checkout-api', onRemove: () => {} }]}
      />,
    );
    expect(screen.getByText('app:')).toBeInTheDocument();
    expect(screen.getByText('checkout-api')).toBeInTheDocument();
  });

  it('calls onRemove when the close icon is clicked', () => {
    const onRemove = vi.fn();
    render(
      <ActiveFilterBar
        chips={[{ key: 'app', labelPrefix: 'app', labelValue: 'checkout-api', onRemove }]}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Remove filter/ }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it('calls onClearAll when the link is clicked', () => {
    const onClearAll = vi.fn();
    render(
      <ActiveFilterBar
        chips={[{ key: 'app', labelValue: 'foo', onRemove: () => {} }]}
        onClearAll={onClearAll}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    expect(onClearAll).toHaveBeenCalledTimes(1);
  });
});
