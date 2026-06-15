{
  description = "A test UDR server in Go with code generated from 3GPP OpenAPI specs";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, utils }:
    utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            git
            gnumake
            nodejs
          ];

          shellHook = ''
            echo "Welcome to the UDR Test Server development shell (Nix Flakes)!"
            export GOPATH=$HOME/go
            export PATH=$GOPATH/bin:$PATH
            go version
            node --version
          '';
        };
      });
}
