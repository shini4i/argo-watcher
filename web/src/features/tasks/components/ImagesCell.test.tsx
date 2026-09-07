import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
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

  it('renders a single image with no counter', () => {
    render(<ImagesCell images={[{ image: 'api', tag: 'v1' }]} />);
    expect(screen.getByText('api')).toBeInTheDocument();
    expect(screen.getByText('v1')).toBeInTheDocument();
    expect(screen.queryByText(/^\+/)).toBeNull();
  });

  it('shows only the first image and counts the rest', () => {
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
    expect(screen.getByText('+2')).toBeInTheDocument();
  });

  it('offers no control to expand, so a click always reaches the row', () => {
    render(
      <ImagesCell
        images={[
          { image: 'api', tag: 'v1' },
          { image: 'worker', tag: 'v2' },
        ]}
      />,
    );
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('names the hidden images in the counter tooltip', () => {
    render(
      <ImagesCell
        images={[
          { image: 'api', tag: 'v1' },
          { image: 'ghcr.io/org/worker', tag: 'v2' },
          { image: 'cron', tag: 'v3' },
        ]}
      />,
    );
    expect(screen.getByText('+2')).toHaveAttribute('title', 'worker:v2, cron:v3');
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
      height: '18px',
      lineHeight: '18px',
    });
    expect(badge).toHaveAttribute('title', tag);
  });
});
