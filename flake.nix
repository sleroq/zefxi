{
  description = "Zefxi - Telegram Client using TDLib in Zig";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            zig

            tdlib
            
            cmake
            pkg-config
          ];

          shellHook = ''
            echo "🚀 Zefxi Development Environment"
            echo "================================"
            echo "Zig version: $(zig version)"
            echo "TDLib available: $(pkg-config --modversion tdjson 2>/dev/null || echo "installed")"
            echo ""
            echo "Available commands:"
            echo "  zig build test-tdlib  - Test TDLib installation"
            echo "  zig build run         - Run the application"
            echo "  zig build             - Build the project"
            echo ""
            echo "Environment variables needed:"
            echo "  TELEGRAM_API_ID       - Your Telegram API ID"
            echo "  TELEGRAM_API_HASH     - Your Telegram API Hash"
            echo ""
            echo "Get API credentials from: https://my.telegram.org/apps"
            echo ""
          '';

          # Environment variables for development
          PKG_CONFIG_PATH = "${pkgs.tdlib}/lib/pkgconfig:${pkgs.openssl.dev}/lib/pkgconfig";
          LD_LIBRARY_PATH = "${pkgs.tdlib}/lib:${pkgs.openssl.out}/lib";
          
          # Ensure TDLib headers are available
          C_INCLUDE_PATH = "${pkgs.tdlib}/include";
        };

        packages.default = pkgs.stdenv.mkDerivation {
          pname = "zefxi";
          version = "0.1.0";
          
          src = ./.;
          
          nativeBuildInputs = with pkgs; [
            zig
            pkg-config
          ];
          
          buildInputs = with pkgs; [
            tdlib
            openssl
            zlib
          ];
          
          buildPhase = ''
            export HOME=$TMPDIR
            zig build -Doptimize=ReleaseSafe
          '';
          
          installPhase = ''
            mkdir -p $out/bin
            cp zig-out/bin/zefxi $out/bin/
          '';
          
          meta = with pkgs.lib; {
            description = "Telegram client using TDLib in Zig";
            license = licenses.mit;
            platforms = platforms.linux ++ platforms.darwin;
          };
        };
      });
} 