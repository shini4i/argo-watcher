import Box from "@mui/material/Box";
import AppBar from "@mui/material/AppBar";
import Toolbar from "@mui/material/Toolbar";
import Typography from "@mui/material/Typography";
import GitHubIcon from "@mui/icons-material/GitHub";
import Link from "@mui/material/Link";
import Button from "@mui/material/Button";
import { Link as RouterLink } from "react-router-dom";
import Stack from "@mui/material/Stack";

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
    return <Box sx={{ mb: 2}}>
        <AppBar position="static">
            <Toolbar>
                <Typography variant="h6" component="div">
                    Argo Watcher
                </Typography>
                <Stack sx={{ flexGrow: 1, px: 2 }} spacing={1} direction={"row"}>
                    <NavigationButton to={"/"}>Recent</NavigationButton>
                    <NavigationButton to={"/history"}>History</NavigationButton>
                </Stack>
                <Button endIcon={<GitHubIcon />}
                        sx={{ color: 'white', textTransform: 'unset'}}
                        size={"small"}
                        component={Link}
                        href={"https://github.com/shini4i/argo-watcher"}>
                    View on GitHub
                </Button>
            </Toolbar>
        </AppBar>
    </Box>
}

export default Navbar;
