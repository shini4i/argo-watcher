import { useState, useCallback } from 'react';
import { Box, ButtonBase, Stack, Typography } from '@mui/material';
import type { Image } from '../../../data/types';
import { tokens } from '../../../theme/tokens';
import { EmptyCell } from './EmptyCell';

interface ImagesCellProps {
  readonly images?: readonly Image[];
}

/** Strips common ghcr.io/docker.io prefixes so the visible label fits the column. */
export const stripRegistryPrefix = (image: string): string => {
  const cleaned = image.replace(/^ghcr\.io\/[^/]+\//, '').replace(/^docker\.io\/(library\/)?/, '');
  const parts = cleaned.split('/');
  return parts[parts.length - 1] || cleaned;
};

interface ImageRowProps {
  readonly image: Image;
}

/** Single repo + tag-chip row, rendered identically for primary and expanded entries. */
const ImageRow = ({ image }: ImageRowProps) => (
  <Stack direction="row" spacing={0.75} alignItems="center" sx={{ minWidth: 0 }}>
    <Typography
      component="span"
      sx={{
        fontFamily: tokens.fontMono,
        fontSize: 11.5,
        color: 'text.primary',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
      }}
      title={image.image}
    >
      {stripRegistryPrefix(image.image)}
    </Typography>
    <Box
      component="span"
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        height: 18,
        padding: '0 6px',
        borderRadius: tokens.radiusPill,
        backgroundColor: tokens.accentSoft,
        color: tokens.accent,
        fontFamily: tokens.fontMono,
        fontSize: 11,
        fontWeight: 500,
      }}
    >
      {image.tag}
    </Box>
  </Stack>
);

/**
 * Compact image cell. Renders the first image:tag inline; remaining images
 * collapse behind a "+N more" toggle that expands the cell in-place. The
 * toggle stops propagation so it doesn't trigger a row navigation.
 */
export const ImagesCell = ({ images }: ImagesCellProps) => {
  const [expanded, setExpanded] = useState(false);
  const handleToggle = useCallback(
    (event: React.MouseEvent) => {
      event.stopPropagation();
      setExpanded(prev => !prev);
    },
    [],
  );

  if (!images?.length) {
    return <EmptyCell />;
  }

  const [primary, ...rest] = images;
  const moreCount = rest.length;

  return (
    <Stack spacing={0.5} sx={{ minWidth: 0 }}>
      <ImageRow image={primary} />
      {moreCount > 0 && !expanded && (
        <ButtonBase
          onClick={handleToggle}
          sx={{
            justifyContent: 'flex-start',
            fontFamily: tokens.fontMono,
            fontSize: 11,
            color: 'text.secondary',
            '&:hover': { color: 'text.primary' },
          }}
        >
          +{moreCount} more
        </ButtonBase>
      )}
      {expanded && (
        <>
          {rest.map((image, index) => (
            <ImageRow key={`${image.image}:${image.tag}:${index}`} image={image} />
          ))}
          <ButtonBase
            onClick={handleToggle}
            sx={{
              justifyContent: 'flex-start',
              fontFamily: tokens.fontMono,
              fontSize: 11,
              color: 'text.secondary',
              '&:hover': { color: 'text.primary' },
            }}
          >
            Show less
          </ButtonBase>
        </>
      )}
    </Stack>
  );
};
