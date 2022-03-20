import Box from "@mui/material/Box";
import AppBar from "@mui/material/AppBar";
import Toolbar from "@mui/material/Toolbar";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import GitHubIcon from "@mui/icons-material/GitHub";
import Link from "@mui/material/Link";

function Navbar() {
    return <Box sx={{ mb: 2}}>
        <AppBar position="static">
            <Toolbar>
                <Typography variant="h6" component="div" sx={{ flexGrow: 1 }}>
                    Argo Watcher
                </Typography>
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
