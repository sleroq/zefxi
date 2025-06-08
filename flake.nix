{
  description = "Zefxi - TDLib Telegram Client in Zig";

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
            # Zig compiler
            zig

            # TDLib - Telegram Database Library
            tdlib

            # Development tools
            gdb
            valgrind
            
            # Network debugging tools (optional)
            wireshark
            tcpdump
            netcat
          ];

          shellHook = ''
            echo "🚀 Zefxi Development Environment"
            echo "================================"
            echo "Zig version: $(zig version)"
            echo "TDLib version: $(pkg-config --modversion tdjson 2>/dev/null || echo 'installed')"
            echo ""
            echo "Available commands:"
            echo "  zig build run         - Run the TDLib client"
            echo "  zig build             - Build the project"
            echo "  zig build test        - Run tests"
            echo ""
            echo "Environment variables needed:"
            echo "  TELEGRAM_API_ID       - Your Telegram API ID"
            echo "  TELEGRAM_API_HASH     - Your Telegram API Hash"
            echo ""
            echo "Get API credentials from: https://my.telegram.org/apps"
            echo ""
            echo "This implementation uses TDLib JSON interface"
            echo "High-level, reliable Telegram client library!"
            echo ""
          '';
        };

        # Package the application
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
            description = "TDLib Telegram client in Zig";
            license = licenses.mit;
            platforms = platforms.linux ++ platforms.darwin;
          };
        };
      });
} 