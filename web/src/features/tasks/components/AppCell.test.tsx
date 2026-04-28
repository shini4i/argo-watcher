import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AppCell, deriveMonogram, describeProject } from './AppCell';

describe('deriveMonogram', () => {
  it('returns first letter of each segment for hyphenated names', () => {
    expect(deriveMonogram('checkout-api')).toBe('CA');
    expect(deriveMonogram('payments_service')).toBe('PS');
  });

  it('returns first two letters of a single-segment name', () => {
    expect(deriveMonogram('argo')).toBe('AR');
  });

  it('falls back to "?" for empty input', () => {
    expect(deriveMonogram('')).toBe('?');
    expect(deriveMonogram('   ')).toBe('?');
  });
});

describe('describeProject', () => {
  it('treats non-URL strings as plain labels', () => {
    expect(describeProject('infra/prod')).toEqual({ isUrl: false, label: 'infra/prod' });
  });

  it('extracts host + last path segment from URLs', () => {
    expect(describeProject('https://github.com/org/repo/')).toEqual({
      isUrl: true,
      label: 'github.com/repo',
      href: 'https://github.com/org/repo/',
    });
  });

  it('returns just the host when no path is present', () => {
    expect(describeProject('https://example.com/')).toEqual({
      isUrl: true,
      label: 'example.com',
      href: 'https://example.com/',
    });
  });
});

describe('AppCell', () => {
  it('renders the monogram and app name', () => {
    render(<AppCell app="checkout-api" />);
    expect(screen.getByText('CA')).toBeInTheDocument();
    expect(screen.getByText('checkout-api')).toBeInTheDocument();
  });
});
