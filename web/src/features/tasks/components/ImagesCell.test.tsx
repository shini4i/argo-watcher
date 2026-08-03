import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ImagesCell, stripRegistryPrefix, TAG_MAX_WIDTH } from './ImagesCell';

describe('stripRegistryPrefix', () => {
  it('strips ghcr.io/<org>/ prefixes', () => {
    expect(stripRegistryPrefix('ghcr.io/shini4i/api')).toBe('api');
  });

  it('strips docker.io/library/ prefixes', () => {
    expect(stripRegistryPrefix('docker.io/library/postgres')).toBe('postgres');
  });

  it('returns the last path segment for nested repos', () => {
    expect(stripRegistryPrefix('quay.io/myorg/group/img')).toBe('img');
  });

  it('passes through bare image names', () => {
    expect(stripRegistryPrefix('redis')).toBe('redis');
  });
});

describe('ImagesCell', () => {
  it('renders the em-dash when no images', () => {
    render(<ImagesCell images={[]} />);
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('renders a single image inline without a toggle', () => {
    render(<ImagesCell images={[{ image: 'api', tag: 'v1' }]} />);
    expect(screen.getByText('api')).toBeInTheDocument();
    expect(screen.getByText('v1')).toBeInTheDocument();
    expect(screen.queryByText(/more/)).toBeNull();
  });

  it('collapses extra images behind a +N more toggle', () => {
    render(
      <ImagesCell
        images={[
          { image: 'api', tag: 'v1' },
          { image: 'worker', tag: 'v2' },
          { image: 'cron', tag: 'v3' },
        ]}
      />,
    );
    expect(screen.getByText('api')).toBeInTheDocument();
    expect(screen.queryByText('worker')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '+2 more' }));
    expect(screen.getByText('worker')).toBeInTheDocument();
    expect(screen.getByText('cron')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Show less' })).toBeInTheDocument();
  });

  it('stops propagation so row navigation does not fire when toggling', () => {
    const onRowClick = vi.fn();
    render(
      <button
        type="button"
        aria-label="parent row"
        onClick={onRowClick}
        onKeyDown={onRowClick}
      >
        <ImagesCell
          images={[
            { image: 'api', tag: 'v1' },
            { image: 'worker', tag: 'v2' },
          ]}
        />
      </button>,
    );
    fireEvent.click(screen.getByRole('button', { name: '+1 more' }));
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it('keeps a hyphenated tag on one line instead of shrinking its badge', () => {
    render(<ImagesCell images={[{ image: 'web-frontend', tag: '895-public' }]} />);

    const badge = screen.getByText('895-public');
    expect(badge).toHaveStyle({ whiteSpace: 'nowrap', flexShrink: '0' });
  });

  it('trims an over-long tag and keeps the full value in the tooltip', () => {
    const tag = 'release-a-very-long-build-identifier';
    render(<ImagesCell images={[{ image: 'web-frontend', tag }]} />);

    const badge = screen.getByText(tag);
    expect(badge).toHaveStyle({
      // inline-flex would ignore text-overflow and clip the tag mid-character.
      display: 'inline-block',
      maxWidth: `${TAG_MAX_WIDTH}px`,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      // With inline-block the line-height, not flex alignment, centers the text.
      height: '18px',
      lineHeight: '18px',
    });
    expect(badge).toHaveAttribute('title', tag);
  });
});
