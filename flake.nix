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
          go_1_25
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
          runtimeInputs = [ pkgs.nodejs_20 ];
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
            nodejs_20
            pnpm
            corepack
          ]) ++ [ viteShim ];

        # mkdocs-llmstxt and its dependency chain (mdformat-tables, mdformat)
        # are not packaged for python311 in nixpkgs, so we build them from PyPI
        # sdists to keep the dev shell's `mkdocs build`/`serve` in sync with
        # docs/requirements.txt. mdformat is pinned to 0.7.22 — the same version
        # pip resolves on Read the Docs, since mdformat-tables caps mdformat
        # <0.8.0 (nixpkgs only ships mdformat 1.0.0, which is disabled on 3.11).
        py = pkgs.python311Packages;

        mdformat-0_7 = py.buildPythonPackage rec {
          pname = "mdformat";
          version = "0.7.22";
          pyproject = true;
          src = pkgs.fetchPypi {
            inherit pname version;
            hash = "sha256-7vhPqPIz0xYnNGg8KopiIiJ6IpuSBocuYTlljZmsseo=";
          };
          build-system = [ py.setuptools ];
          dependencies = [ py.markdown-it-py ];
          pythonImportsCheck = [ "mdformat" ];
        };

        mdformat-tables = py.buildPythonPackage rec {
          pname = "mdformat-tables";
          version = "1.0.0";
          pyproject = true;
          src = pkgs.fetchPypi {
            pname = "mdformat_tables";
            inherit version;
            hash = "sha256-pX2xrBfEoSXaeU70VTmQS7ipWS6AVX1SXh8WnJbaosg=";
          };
          build-system = [ py.flit-core ];
          dependencies = [ mdformat-0_7 py.wcwidth ];
          pythonImportsCheck = [ "mdformat_tables" ];
        };

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
            mdformat-0_7
            mdformat-tables
          ];
          pythonImportsCheck = [ "mkdocs_llmstxt" ];
        };

        docsPython = pkgs.python311.withPackages (ps: (with ps; [
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
