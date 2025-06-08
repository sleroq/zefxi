{
  description = "Zefxi - TDLib Telegram Client in Zig";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    zig.url = "github:mitchellh/zig-overlay";
  };

  outputs = { self, nixpkgs, flake-utils, zig }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            zig.packages.${system}.master
            tdlib
          ];

          shellHook = ''
            source .env

            echo "Zefxi Development Environment"
            echo "================================"
            echo "Zig version: $(zig version)"
            echo "TDLib version: $(pkg-config --modversion tdjson 2>/dev/null || echo 'installed')"
            echo ""
            echo "Available commands:"
            echo "  zig build run         - Run the bridge"
            echo "  zig build             - Build the project"
            echo ""
            echo "Environment variables needed:"
            echo "  TELEGRAM_API_ID       - Your Telegram API ID"
            echo "  TELEGRAM_API_HASH     - Your Telegram API Hash"
            echo "  DISCORD_TOKEN         - Your Discord bot token"
            echo "  DISCORD_SERVER        - Your Discord server ID"
            echo "  DISCORD_CHANNEL       - Your Discord channel ID"
            echo ""
          '';
        };

        # Package the application
        packages.default = pkgs.stdenv.mkDerivation {
          pname = "zefxi";
          version = "0.1.0";
          
          src = ./.;
          
          nativeBuildInputs = with pkgs; [
            zig.packages.${system}.master
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