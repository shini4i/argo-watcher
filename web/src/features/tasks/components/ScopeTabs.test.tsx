import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { deriveInitials, ScopeTabs } from './ScopeTabs';

describe('deriveInitials', () => {
  it('takes the first letter of the first two local-part segments', () => {
    expect(deriveInitials('jane.doe@example.com')).toBe('JD');
    expect(deriveInitials('sam_reed@example.com')).toBe('SR');
    expect(deriveInitials('lee-park@example.com')).toBe('LP');
  });

  it('takes the first two letters of a single-segment local part', () => {
    expect(deriveInitials('alice@example.com')).toBe('AL');
  });

  it('falls back to "?" when there is nothing to derive from', () => {
    expect(deriveInitials('')).toBe('?');
    expect(deriveInitials('@example.com')).toBe('?');
  });

  it('handles an address with no domain at all', () => {
    expect(deriveInitials('ci-bot')).toBe('CB');
  });
});

describe('ScopeTabs', () => {
  it('marks the active scope for assistive technology', () => {
    render(<ScopeTabs value="mine" onChange={vi.fn()} identityEmail="jane.doe@example.com" />);

    expect(screen.getByRole('tab', { name: /Mine/ })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: /Everyone/ })).toHaveAttribute('aria-selected', 'false');
  });

  it('names the group so the two pill rows are distinguishable', () => {
    render(<ScopeTabs value="mine" onChange={vi.fn()} identityEmail="jane@example.com" />);
    expect(screen.getByRole('tablist', { name: 'Deployment scope' })).toBeInTheDocument();
  });

  it('reports the chosen scope', () => {
    const onChange = vi.fn();
    render(<ScopeTabs value="mine" onChange={onChange} identityEmail="jane@example.com" />);

    fireEvent.click(screen.getByRole('tab', { name: /Everyone/ }));
    expect(onChange).toHaveBeenCalledWith('everyone');

    fireEvent.click(screen.getByRole('tab', { name: /Mine/ }));
    expect(onChange).toHaveBeenCalledWith('mine');
  });

  // The initials are decoration beside the word "Mine"; announcing them would
  // read as a second, meaningless label on the same tab.
  it('keeps the initials out of the accessible name', () => {
    render(<ScopeTabs value="mine" onChange={vi.fn()} identityEmail="jane.doe@example.com" />);
    expect(screen.getByRole('tab', { name: 'Mine' })).toBeInTheDocument();
  });
});
