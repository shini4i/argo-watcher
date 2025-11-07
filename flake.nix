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
          go_1_24
          gopls
          gotools
          gosec
          mockgen
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
      in
      {
        devShells.default = pkgs.mkShell {
          packages = goToolchain ++ preCommitTools ++ frontendToolchain;
          shellHook = ''
            export GOPATH="$PWD/.go"
            export GOMODCACHE="$PWD/.gomod"
            mkdir -p "$GOPATH" "$GOMODCACHE"
            export GO111MODULE=on
          '';
        };
      }
    );
}
