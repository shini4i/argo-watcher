import React, { useEffect, useState } from 'react';
import PropTypes from 'prop-types';
import Autocomplete from '@mui/material/Autocomplete';
import TextField from '@mui/material/TextField';

import { fetchApplications } from '../Services/Data';

function ApplicationsFilter({ value, onChange, setError, setSuccess }) {
  const [applications, setApplications] = useState([]);

  useEffect(() => {
    fetchApplications()
      .then(items => {
        setSuccess('fetchApplications', 'Application filter dropdown fetched');
        setApplications(items);
      })
      .catch(error => {
        setError('fetchApplications', error.message);
      });
  }, []);

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
  setError: PropTypes.func.isRequired,
  setSuccess: PropTypes.func.isRequired,
};

export default ApplicationsFilter;
