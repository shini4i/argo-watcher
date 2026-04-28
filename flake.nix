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
        ];

        preCommitTools = with pkgs; [
          pre-commit
          git
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

        docsPython = pkgs.python311.withPackages (ps: with ps; [
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
        ]);

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
          packages = goToolchain ++ preCommitTools ++ frontendToolchain ++ docsToolchain;
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
