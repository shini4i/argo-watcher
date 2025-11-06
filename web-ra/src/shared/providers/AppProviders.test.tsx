import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AppProviders } from './AppProviders';

describe('AppProviders', () => {
  it('renders children content', () => {
    render(
      <AppProviders>
        <span>hello</span>
      </AppProviders>,
    );

    expect(screen.getByText('hello')).toBeInTheDocument();
  });
});
