import Box from "@mui/material/Box";
import AppBar from "@mui/material/AppBar";
import Toolbar from "@mui/material/Toolbar";
import Typography from "@mui/material/Typography";
import GitHubIcon from "@mui/icons-material/GitHub";
import Link from "@mui/material/Link";
import { Link as RouterLink } from "react-router-dom";
import Stack from "@mui/material/Stack";
import {fetchVersion} from "../Services/Data";
import {useEffect, useState} from "react";
import LocalOfferIcon from '@mui/icons-material/LocalOffer';

function NavigationButton({to, children}) {
    return <Link
        sx={{ color: 'white', display: 'block', mx: '10px' }}
        component={RouterLink}
        to={to}
    >
        {children}
    </Link>
}

function Navbar() {

    const [version, setVersion] = useState('0.0.0');

    useEffect(() => {
        fetchVersion()
            .then(version => { setVersion(version); })
            .catch(error => { console.log(error.message); });
    }, []);

    return <Box sx={{ mb: 2 }}>
        <AppBar position="static" style={{ background: '#2E3B55' }}>
            <Toolbar>
                <Typography variant="h6" component="div">
                    Argo Watcher
                </Typography>
                <Stack sx={{ flexGrow: 1, px: 2 }} spacing={1} direction={"row"}>
                    <NavigationButton to={"/"}>Recent</NavigationButton>
                    <NavigationButton to={"/history"}>History</NavigationButton>
                </Stack>
                <Stack spacing={1} direction={"row"} alignItems={"center"}
                       sx={{
                           color: 'white', textTransform: 'unset', textDecoration: "unset",
                           '&:hover': {
                               color: 'rgba(255,255,255,.85)'
                           }
                        }}
                       component={Link}
                       href={"https://github.com/shini4i/argo-watcher/tree/" + version}
                >
                    <GitHubIcon sx={{ fontSize: "1.7em" }} />
                    <Stack>
                        <Typography fontSize={"14px"}>
                            GitHub
                        </Typography>
                        <Typography fontSize={"11px"}>
                            <LocalOfferIcon fontSize={"6px"}/> {version}
                        </Typography>
                    </Stack>
                </Stack>
            </Toolbar>
        </AppBar>
    </Box>
}

export default Navbar;
