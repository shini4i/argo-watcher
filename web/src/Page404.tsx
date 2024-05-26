import React, { FunctionComponent } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import './Page404.css';

const Page404: FunctionComponent = () => {
  return (
    <Box
      sx={{
        color: '#888',
        margin: 0,
        display: 'table',
        width: '100%',
        height: '100vh',
        textAlign: 'center',
      }}
    >
      <Box
        sx={{
          display: 'table-cell',
          verticalAlign: 'middle',
        }}
      >
        <Typography
          component={'h1'}
          variant={'h1'}
          sx={{
            fontSize: '50px',
            display: 'inline-block',
            paddingRight: '12px',
            animation: 'type .5s alternate infinite',
            fontFamily: "'Lato', sans-serif",
          }}
        >
          Page not found: 404
        </Typography>
      </Box>
    </Box>
  );
}

export default Page404;
