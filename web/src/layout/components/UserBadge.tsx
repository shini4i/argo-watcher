import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import LogoutIcon from '@mui/icons-material/Logout';
import {
  Avatar,
  ButtonBase,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Stack,
  SvgIcon,
  Tooltip,
  Typography,
} from '@mui/material';
import type { SvgIconProps } from '@mui/material';
import { useState } from 'react';
import { useGetIdentity, useLogout } from 'react-admin';

interface UserBadgeProps {
  /** Whether the signed-in user belongs to one of the configured privileged groups. */
  readonly privileged: boolean;
}

/** Crown marking a privileged user; @mui/icons-material ships no crown glyph. */
const CrownIcon = (props: SvgIconProps) => (
  <SvgIcon viewBox="0 0 24 24" {...props}>
    <path d="M3 5l5.5 4L12 3l3.5 6L21 5l-2 12H5L3 5zm2 14h14v2H5v-2z" />
  </SvgIcon>
);

/**
 * Account card for the signed-in user: the OIDC profile picture (initial when the
 * provider serves none), the display name with a crown for privileged users, the
 * email, and a menu holding the sign-out action.
 *
 * Renders nothing until an identity resolves, so a session still being established
 * never shows an empty card.
 */
export const UserBadge = ({ privileged }: UserBadgeProps) => {
  const { identity, isPending } = useGetIdentity();
  const logout = useLogout();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

  if (isPending || !identity?.id) {
    return null;
  }
  // A provider is free to keep every naming claim out of the ID token; the card still
  // has to render, because its menu is the only way to sign out.
  const name = identity.fullName || identity.email || 'Signed in';
  // Only a second line when it adds something the name does not already say.
  const secondary = identity?.email && identity.email !== name ? identity.email : undefined;

  return (
    <>
      <ButtonBase
        onClick={event => setAnchorEl(event.currentTarget)}
        aria-haspopup="menu"
        aria-expanded={Boolean(anchorEl)}
        sx={theme => ({
          width: '100%',
          justifyContent: 'flex-start',
          gap: 1.5,
          px: 1.5,
          py: 1,
          borderRadius: 1,
          border: `1px solid ${theme.palette.divider}`,
          backgroundColor: theme.palette.action.hover,
          '&:hover': { backgroundColor: theme.palette.action.selected },
        })}
      >
        <Avatar
          src={identity.avatar}
          alt={name}
          // Keeps the deployment's hostname out of the request when the avatar is a
          // Gravatar: the third party has no business learning where it was loaded from.
          slotProps={{ img: { referrerPolicy: 'no-referrer' } }}
          sx={theme => ({
            width: 36,
            height: 36,
            fontSize: 15,
            fontWeight: 700,
            backgroundColor: theme.palette.primary.main,
            color: theme.palette.primary.contrastText,
          })}
        >
          {[...name][0].toUpperCase()}
        </Avatar>
        <Stack sx={{ minWidth: 0, flexGrow: 1, alignItems: 'flex-start' }}>
          <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', maxWidth: '100%' }}>
            <Typography variant="body2" sx={{ fontWeight: 600, minWidth: 0 }} noWrap title={name}>
              {name}
            </Typography>
            {privileged && (
              <Tooltip title="Privileged access">
                <CrownIcon
                  titleAccess="Privileged access"
                  sx={{ fontSize: 16, color: 'warning.main', flexShrink: 0 }}
                />
              </Tooltip>
            )}
          </Stack>
          {secondary && (
            <Typography
              variant="caption"
              sx={{ color: 'text.secondary', maxWidth: '100%' }}
              noWrap
              title={secondary}
            >
              {secondary}
            </Typography>
          )}
        </Stack>
        <ExpandMoreIcon fontSize="small" sx={{ color: 'text.secondary', flexShrink: 0 }} />
      </ButtonBase>
      <Menu
        anchorEl={anchorEl}
        open={Boolean(anchorEl)}
        onClose={() => setAnchorEl(null)}
        slotProps={{ paper: { sx: { minWidth: 180 } } }}
      >
        <MenuItem
          onClick={() => {
            setAnchorEl(null);
            logout();
          }}
        >
          <ListItemIcon>
            <LogoutIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>Log out</ListItemText>
        </MenuItem>
      </Menu>
    </>
  );
};
