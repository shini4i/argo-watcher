import { Box, Stack, Typography } from '@mui/material';
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

/** Widest a tag badge may grow, in px, before its label is ellipsised. */
export const TAG_MAX_WIDTH = 120;

/**
 * The tag badge never wraps or shrinks — a hyphenated tag would otherwise break
 * after the hyphen and spill out of the fixed-height pill.
 */
const ImageRow = ({ image }: ImageRowProps) => (
  <Stack
    direction="row"
    spacing={0.75}
    sx={{
      alignItems: 'center',
      minWidth: 0
    }}>
    <Typography
      component="span"
      sx={{
        fontFamily: tokens.fontMono,
        fontSize: 11.5,
        color: 'text.primary',
        minWidth: 0,
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
      title={image.tag}
      sx={{
        // inline-block, not inline-flex: text-overflow is ignored on a flex
        // container, which clips the tag mid-character instead of ellipsizing.
        display: 'inline-block',
        flexShrink: 0,
        maxWidth: TAG_MAX_WIDTH,
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        height: 18,
        lineHeight: '18px',
        padding: '0 6px',
        borderRadius: tokens.radiusPill,
        backgroundColor: theme => (theme.palette.mode === 'dark' ? tokens.accentSoftDark : tokens.accentSoft),
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
 * @description One line per task: the first image and a count of the rest. The
 * extras are named in the counter's tooltip and listed in full on the task
 * detail page, so nothing here competes with the row's own click target.
 */
export const ImagesCell = ({ images }: ImagesCellProps) => {
  if (!images?.length) {
    return <EmptyCell />;
  }

  const [primary, ...rest] = images;

  return (
    <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', minWidth: 0 }}>
      <Box sx={{ minWidth: 0 }}>
        <ImageRow image={primary} />
      </Box>
      {rest.length > 0 && (
        <Typography
          component="span"
          title={rest.map(image => `${stripRegistryPrefix(image.image)}:${image.tag}`).join(', ')}
          sx={{
            flexShrink: 0,
            fontFamily: tokens.fontMono,
            fontSize: 11,
            color: 'text.secondary',
          }}
        >
          +{rest.length}
        </Typography>
      )}
    </Stack>
  );
};
