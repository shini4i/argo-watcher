import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { EmptyState, EmptyStateCta } from './EmptyState';

describe('EmptyState', () => {
  it('renders title and description', () => {
    render(<EmptyState title="Nothing here" description="Kick something off" />);
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
    expect(screen.getByText('Kick something off')).toBeInTheDocument();
  });

  it('sets the hint apart from the description instead of running the two together', () => {
    const { container } = render(
      <EmptyState title="Broken" description="The server refused the read." hint="Retry shortly." />,
    );

    const paragraphs = [...container.querySelectorAll('p')].map(node => node.textContent);
    expect(paragraphs).toEqual(['The server refused the read.', 'Retry shortly.']);
  });

  it('omits the hint paragraph when no hint is provided', () => {
    const { container } = render(<EmptyState title="Broken" description="Only this." />);
    expect(container.querySelectorAll('p')).toHaveLength(1);
  });

  it('omits description when not provided', () => {
    const { container } = render(<EmptyState title="Empty" />);
    expect(screen.getByText('Empty')).toBeInTheDocument();
    // Description is rendered as Typography variant="body2" which becomes <p>;
    // its absence guards against silently rendering an empty paragraph.
    expect(container.querySelector('p')).toBeNull();
  });

  it('renders the error icon variant', () => {
    const { container } = render(<EmptyState icon="error" title="Couldn't load" />);
    expect(screen.getByText("Couldn't load")).toBeInTheDocument();
    expect(container.querySelector('svg[data-testid="ErrorOutlinedIcon"]')).not.toBeNull();
  });

  it('renders the supplied CTA', () => {
    const onClick = vi.fn();
    render(
      <EmptyState
        title="Empty"
        cta={<EmptyStateCta label="Clear filters" onClick={onClick} />}
      />,
    );
    const button = screen.getByRole('button', { name: 'Clear filters' });
    button.click();
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
