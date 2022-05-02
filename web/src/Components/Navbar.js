import Box from "@mui/material/Box";
import AppBar from "@mui/material/AppBar";
import Toolbar from "@mui/material/Toolbar";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import GitHubIcon from "@mui/icons-material/GitHub";
import Link from "@mui/material/Link";
import Button from "@mui/material/Button";
import { Link as RouterLink } from "react-router-dom";
import Stack from "@mui/material/Stack";

function NavigationButton({to, children}) {
    return (
        <Button
            sx={{ color: 'white', display: 'block' }}
            size={"small"}
            component={RouterLink}
            to={to}
        >
            {children}
        </Button>
    );
}

function Navbar() {
    return <Box sx={{ mb: 2}}>
        <AppBar position="static">
            <Toolbar>
                <Typography variant="h6" component="div">
                    Argo Watcher
                </Typography>
                <Stack sx={{ flexGrow: 1, px: 2 }} spacing={1} direction={"row"}>
                    <NavigationButton to={"/"}>recent</NavigationButton>
                    <NavigationButton to={"/history"}>history</NavigationButton>
                    <NavigationButton to={"/test"}>test</NavigationButton>
                </Stack>
                <IconButton
                    edge="start"
                    color="inherit"
                    component={Link}
                    href={"https://github.com/shini4i/argo-watcher#readme"}
                >
                    <GitHubIcon />
                </IconButton>
            </Toolbar>
        </AppBar>
    </Box>
}

export default Navbar;
