import React, { useEffect, useState } from 'react';
import Autocomplete from '@mui/material/Autocomplete';
import TextField from '@mui/material/TextField';

interface ApplicationsFilterProps {
  value: string | null;
  onChange: (newValue: string | null) => void;
  appNames: string[];
}

const ApplicationsFilter: React.FC<ApplicationsFilterProps> = ({ value, onChange, appNames }) => {
  const [applications, setApplications] = useState<string[]>([]);

  useEffect(() => {
    setApplications(appNames);
  }, [appNames]);

  const handleApplicationsChange = (_event: React.ChangeEvent<{}>, newValue: string | null) => {
    if (onChange) {
      onChange(newValue);
    }
  };

  return (
    <Autocomplete
      size="small"
      disablePortal
      options={applications}
      sx={{ width: 220 }}
      renderInput={params => <TextField {...params} label="Application" />}
      value={value || null}
      onChange={handleApplicationsChange}
    />
  );
};

export default ApplicationsFilter;
