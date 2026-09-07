import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AppCell, APP_TEXT_MAX_WIDTH, deriveMonogram, describeProject } from './AppCell';

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

  it('renders the project as a subtitle beneath the app name', () => {
    render(<AppCell app="checkout-api" project="infra/prod" />);
    expect(screen.getByText('infra/prod')).toBeInTheDocument();
    expect(screen.queryByRole('link')).toBeNull();
  });

  // Both lines are nowrap, so an uncapped box would set the cell's min-content
  // width under `table-layout: auto` and stretch the whole table sideways.
  it('caps the subtitle so a long project cannot widen the column', () => {
    render(<AppCell app="checkout-api" project="infra/prod" />);
    expect(screen.getByText('infra/prod')).toHaveStyle({
      display: 'block',
      maxWidth: `${APP_TEXT_MAX_WIDTH}px`,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    });
  });

  it('caps the app name for the same reason', () => {
    const app = 'a-very-long-application-name-that-would-otherwise-set-the-column-width';
    render(<AppCell app={app} />);
    expect(screen.getByText(app)).toHaveStyle({
      display: 'block',
      maxWidth: `${APP_TEXT_MAX_WIDTH}px`,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    });
  });

  it('caps the linked project label too', () => {
    render(<AppCell app="checkout-api" project="https://gitlab.example.net/acme/checkout" />);
    expect(screen.getByRole('link')).toHaveStyle({ maxWidth: `${APP_TEXT_MAX_WIDTH}px` });
  });

  it('links a URL project out to the repository without triggering the row', () => {
    const onRowClick = vi.fn();
    render(
      <button type="button" aria-label="parent row" onClick={onRowClick} onKeyDown={onRowClick}>
        <AppCell app="checkout-api" project="https://github.com/org/repo/" />
      </button>,
    );

    const link = screen.getByRole('link');
    expect(link).toHaveAttribute('href', 'https://github.com/org/repo/');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveTextContent('github.com/repo');

    fireEvent.click(link);
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it('renders no subtitle when the task carries no project', () => {
    render(<AppCell app="checkout-api" project="" />);
    expect(screen.queryByText('—')).toBeNull();
    expect(screen.getByText('checkout-api')).toBeInTheDocument();
  });

  it('flags a rollback beside the app name', () => {
    render(<AppCell app="checkout-api" isRollback />);
    expect(screen.getByText('Rollback')).toBeInTheDocument();
  });

  it('omits the rollback chip for a regular deployment', () => {
    render(<AppCell app="checkout-api" />);
    expect(screen.queryByText('Rollback')).toBeNull();
  });
});
