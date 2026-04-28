import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { StatusPill } from './StatusPill';

describe('StatusPill', () => {
  it('renders the deployed status with its display label', () => {
    render(<StatusPill status="deployed" />);
    expect(screen.getByRole('status')).toHaveAttribute('aria-label', 'Deployed');
    expect(screen.getByText('Deployed')).toBeInTheDocument();
  });

  it('shortens the in-progress label to "Running"', () => {
    render(<StatusPill status="in progress" />);
    expect(screen.getByText('Running')).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveAttribute('aria-label', 'In Progress');
  });

  it('renders failed status with its label', () => {
    render(<StatusPill status="failed" />);
    expect(screen.getByText('Failed')).toBeInTheDocument();
  });

  it('shortens the app-not-found label to "Not found"', () => {
    render(<StatusPill status="app not found" />);
    expect(screen.getByText('Not found')).toBeInTheDocument();
  });

  it('falls back to "Unknown" when no status is provided', () => {
    render(<StatusPill status={null} />);
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });

  it('passes unknown statuses through verbatim', () => {
    render(<StatusPill status="custom" />);
    expect(screen.getByText('custom')).toBeInTheDocument();
  });
});
