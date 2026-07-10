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
          mockgen
          go-swag
          toxiproxy
        ];

        preCommitTools = with pkgs; [
          pre-commit
          git
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
          packages = goToolchain ++ preCommitTools ++ securityTools ++ frontendToolchain ++ docsToolchain;
          shellHook = ''
            export GOPATH="$PWD/.go"
            export GOMODCACHE="$PWD/.gomod"
            mkdir -p "$GOPATH" "$GOMODCACHE"
            export GO111MODULE=on

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
