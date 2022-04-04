// reference issue: https://github.com/facebook/create-react-app/issues/3783
// reference docs: https://create-react-app.dev/docs/proxying-api-requests-in-development/#configuring-the-proxy-manually
const pkg = require('../package.json');
const target = process.env.PROXY || pkg.proxy;

const { createProxyMiddleware } = require('http-proxy-middleware');

module.exports = function (app) {
    app.use(
        '/api',
        createProxyMiddleware({
            target,
            changeOrigin: true,
        })
    );
};