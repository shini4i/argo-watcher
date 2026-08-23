// Loaded as a file rather than inlined so the page needs no script-src 'unsafe-inline'.
window.onload = function () {
    SwaggerUIBundle({
        url: "swagger.json",
        dom_id: "#swagger-ui",
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        layout: "StandaloneLayout",
        validatorUrl: null,
        defaultModelsExpandDepth: -1,
    });
};
