{
  description = "Development environment for argo-watcher with Go tooling and pre-commit hooks";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        goToolchain = with pkgs; [
          go_1_26
          gopls
          gotools
          gosec
          golangci-lint
          mockgen
          go-swag
          toxiproxy
          goreleaser
        ];

        preCommitTools = with pkgs; [
          pre-commit
          git
        ];

        # The e2e lab and CI helpers are bash: shellcheck lints them, bats runs them.
        # bats-assert/-support are needed by test/e2e/scripts/lib.bats; yq-go by
        # test/e2e/ports.bats, which reads the lab's kind/NodePort YAML.
        # NOTE: this covers `task -d test/e2e lint` only. Running the lab itself also
        # needs kind, kubectl, helm, jq and task, which this shell does NOT provide.
        shellToolchain = with pkgs; [
          shellcheck
          yq-go
          (bats.withLibraries (p: [ p.bats-support p.bats-assert ]))
        ];

        # Security scanners, mirroring the CI security workflow so they can be
        # run locally. gosec is already part of goToolchain. nuclei is
        # intentionally absent — DAST runs only in CI against a live server.
        securityTools = with pkgs; [
          govulncheck
          trivy
          trufflehog
          zizmor
        ];

        viteShim = pkgs.writeShellApplication {
          name = "vite";
          runtimeInputs = [ pkgs.nodejs_24 ];
          text = ''
            set -euo pipefail
            if [ -x "$PWD/node_modules/.bin/vite" ]; then
              exec "$PWD/node_modules/.bin/vite" "$@"
            elif [ -x "$PWD/web/node_modules/.bin/vite" ]; then
              exec "$PWD/web/node_modules/.bin/vite" "$@"
            else
              exec npx --yes vite "$@"
            fi
          '';
        };

        frontendToolchain =
          (with pkgs; [
            nodejs_24
            pnpm
            corepack
          ]) ++ [ viteShim ];

        # Browsers for the Playwright suite (web/e2e). Playwright refuses to run
        # against a browser build it was not compiled for, so web/package.json
        # pins @playwright/test to EXACTLY this derivation's version. CI installs
        # its own browsers and would not catch a drift, so the dev shell asserts
        # the pairing here and fails to evaluate with the mismatch named.
        pinnedPlaywright =
          (builtins.fromJSON (builtins.readFile ./web/package.json)).devDependencies."@playwright/test";

        playwrightBrowsers =
          assert pkgs.lib.assertMsg (pkgs.playwright-driver.version == pinnedPlaywright) ''
            Playwright version mismatch: nixpkgs playwright-driver is ${pkgs.playwright-driver.version},
            but web/package.json pins @playwright/test to ${pinnedPlaywright}.
            Bump both together (the browsers must match the client build).
          '';
          pkgs.playwright-driver.browsers;

        # mkdocs-llmstxt is not packaged in nixpkgs, so we build it from its PyPI
        # sdist to keep the dev shell's `mkdocs build`/`serve` in sync with
        # docs/requirements.txt. Everything it needs — mkdocs, mdformat 1.0.0 and
        # mdformat-gfm (the successor to the archived mdformat-tables) — now ships
        # in nixpkgs for the default python3 interpreter, so no other custom sdist
        # builds are required.
        py = pkgs.python3Packages;

        mkdocs-llmstxt = py.buildPythonPackage rec {
          pname = "mkdocs-llmstxt";
          version = "0.5.0";
          pyproject = true;
          src = pkgs.fetchPypi {
            pname = "mkdocs_llmstxt";
            inherit version;
            hash = "sha256-svqebWjfQddGfpSKR0VyW2yZQ0o2s2IEhX29e7Pf4EE=";
          };
          build-system = [ py.pdm-backend ];
          dependencies = [
            py.mkdocs
            py.beautifulsoup4
            py.markdownify
            py.mdformat
            py.mdformat-gfm
          ];
          # mdformat-gfm supersedes the archived mdformat-tables and already provides
          # the "tables" extension mkdocs-llmstxt requests, so strip the stale pin.
          pythonRemoveDeps = [ "mdformat-tables" ];
          pythonImportsCheck = [ "mkdocs_llmstxt" ];
        };

        docsPython = pkgs.python3.withPackages (ps: (with ps; [
          mkdocs
          mkdocs-material
          mkdocs-material-extensions
          mkdocs-git-committers-plugin-2
          mkdocs-git-revision-date-localized-plugin
          mkdocs-glightbox
          mkdocs-redirects
          mkdocs-swagger-ui-tag
          pymdown-extensions
          pillow
          cairosvg
        ]) ++ [ mkdocs-llmstxt ]);

        docsToolchain = [ docsPython ] ++ (with pkgs; [
          cairo
          pango
          libffi
          freetype
          libjpeg
          libpng
          zlib
        ]);
      in
      {
        devShells.default = pkgs.mkShell {
          packages = goToolchain ++ preCommitTools ++ shellToolchain ++ securityTools ++ frontendToolchain ++ docsToolchain;
          shellHook = ''
            export GOPATH="$PWD/.go"
            export GOMODCACHE="$PWD/.gomod"
            mkdir -p "$GOPATH" "$GOMODCACHE"
            export GO111MODULE=on

            # Point Playwright at the Nix-provided browsers instead of the
            # per-user download cache, which nothing here populates.
            export PLAYWRIGHT_BROWSERS_PATH="${playwrightBrowsers}"

            export LD_LIBRARY_PATH="${pkgs.lib.makeLibraryPath [
              pkgs.cairo
              pkgs.pango
              pkgs.libffi
              pkgs.freetype
              pkgs.libjpeg
              pkgs.libpng
              pkgs.zlib
            ]}''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
          '';
        };
      }
    );
}
