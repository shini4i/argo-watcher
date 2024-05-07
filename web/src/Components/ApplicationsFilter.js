import React, { useEffect, useState } from 'react';
import PropTypes from 'prop-types';
import Autocomplete from '@mui/material/Autocomplete';
import TextField from '@mui/material/TextField';

function ApplicationsFilter({ value, onChange, appNames }) {
  const [applications, setApplications] = useState([]);

  useEffect(() => {
    setApplications(appNames);
  }, [appNames]);

  const handleApplicationsChange = (_event, newValue) => {
    onChange?.(newValue);
  };

  return (
    <Autocomplete
      size={'small'}
      disablePortal
      options={applications}
      sx={{ width: 220 }}
      renderInput={params => <TextField {...params} label="Application" />}
      value={value || null}
      onChange={handleApplicationsChange}
    />
  );
}

ApplicationsFilter.propTypes = {
  value: PropTypes.any,
  onChange: PropTypes.func,
  appNames: PropTypes.array.isRequired
};

export default ApplicationsFilter;
