import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { LoadingState } from './LoadingState';

describe('LoadingState', () => {
  it('renders a spinner with an accessible label', () => {
    render(<LoadingState />);
    const region = screen.getByRole('status', { name: 'Loading' });
    expect(region).toBeInTheDocument();
    expect(region.querySelector('.MuiCircularProgress-root')).not.toBeNull();
  });
});
