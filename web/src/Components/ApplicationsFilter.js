import Autocomplete from "@mui/material/Autocomplete";
import TextField from "@mui/material/TextField";
import React, {useEffect, useState} from "react";
import {fetchApplications} from "../Services/Data";

function ApplicationsFilter({ value, onChange, setLoadingError }) {
  const [applications, setApplications] = useState([]);

  useEffect(() => {
    fetchApplications()
        .then(items => { setApplications(items) })
        .catch(error => { setLoadingError(error.message); });
  }, []);

  const handleApplicationsChange = (event, newValue) => {
    onChange && onChange(newValue);
  };

  return <Autocomplete
      size={"small"}
      disablePortal
      options={applications}
      sx={{ width: 220 }}
      renderInput={(params) => <TextField {...params} label="Application" />}
      value={value || null}
      onChange={handleApplicationsChange}
  />
}

export default ApplicationsFilter;
