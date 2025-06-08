{
  description = "Zefxi - TDLib Telegram Client in Zig";

  inputs = {
    zig2nix.url = "github:Cloudef/zig2nix";
  };

  outputs = { self, zig2nix, ... }: let
    flake-utils = zig2nix.inputs.flake-utils;
    nixpkgs = zig2nix.inputs.nixpkgs;
  in {
    # NixOS module for zefxi
    nixosModules.zefxi = { config, lib, pkgs, ... }:
      with lib;
      let
        cfg = config.services.zefxi;
        # Get zig2nix environment for this system - using master version for better compatibility
        env = zig2nix.outputs.zig-env.${pkgs.system} {
          zig = zig2nix.outputs.packages.${pkgs.system}.zig-master;
        };
        # Build zefxi using zig2nix
        zefxiPackage = env.package {
          src = lib.cleanSource ./.;
          
          nativeBuildInputs = with pkgs; [
            pkg-config
          ];

          buildInputs = with pkgs; [
            tdlib
          ];

          # Prefer nix friendly settings for NixOS module
          zigPreferMusl = false;
          
          # Libraries required for runtime
          zigWrapperLibs = with pkgs; [ tdlib glibc ];
          
          # Ensure proper dynamic linking on NixOS
          zigWrapperArgs = [
            "--set LD_LIBRARY_PATH ${pkgs.lib.makeLibraryPath [ pkgs.tdlib pkgs.glibc ]}"
          ];
        };
      in {
        options.services.zefxi = {
          enable = mkEnableOption "Zefxi Telegram-Discord Bridge";

          package = mkOption {
            type = types.package;
            default = zefxiPackage;
            description = "The zefxi package to use";
          };

          user = mkOption {
            type = types.str;
            default = "zefxi";
            description = "User to run zefxi as";
          };

          group = mkOption {
            type = types.str;
            default = "zefxi";
            description = "Group to run zefxi as";
          };

          dataDir = mkOption {
            type = types.str;
            default = "/var/lib/zefxi";
            description = "Directory to store zefxi data and TDLib files";
          };

          avatarPort = mkOption {
            type = types.port;
            default = 8080;
            description = "Port for the avatar HTTP server";
          };

          environmentFile = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "File containing environment variables (recommended for secrets)";
          };

          settings = mkOption {
            type = types.attrs;
            default = { };
            description = "Environment variables for zefxi";
            example = {
              TELEGRAM_API_ID = "123456";
              TELEGRAM_API_HASH = "your_api_hash";
              TELEGRAM_CHAT_ID = "-1001234567890";
              DISCORD_TOKEN = "your_discord_bot_token";
              DISCORD_SERVER = "1234567890123456789";
              DISCORD_CHANNEL = "1234567890123456789";
              DISCORD_WEBHOOK_URL = "https://discord.com/api/webhooks/ID/TOKEN";
              DEBUG = "1";
            };
          };

          caddy = {
            enable = mkEnableOption "Caddy reverse proxy for avatar serving";
            
            domain = mkOption {
              type = types.str;
              default = "localhost";
              description = "Domain name for the avatar server";
              example = "avatars.example.com";
            };
          };
        };

        config = mkIf cfg.enable {
          # Create user and group
          users.users.${cfg.user} = {
            isSystemUser = true;
            group = cfg.group;
            home = cfg.dataDir;
            createHome = true;
            description = "Zefxi Telegram-Discord bridge user";
          };

          users.groups.${cfg.group} = { };

          # Create data directories
          systemd.tmpfiles.rules = [
            "d ${cfg.dataDir} 0755 ${cfg.user} ${cfg.group} -"
            "d ${cfg.dataDir}/tdlib 0755 ${cfg.user} ${cfg.group} -"
            "d ${cfg.dataDir}/tdlib/photos 0755 ${cfg.user} ${cfg.group} -"
          ];

          # Main zefxi service
          systemd.services.zefxi = {
            description = "Zefxi Telegram-Discord Bridge";
            wantedBy = [ "multi-user.target" ];
            after = [ "network.target" ];

            serviceConfig = {
              Type = "simple";
              User = cfg.user;
              Group = cfg.group;
              WorkingDirectory = cfg.dataDir;
              ExecStart = "${cfg.package}/bin/zefxi";
              Restart = "always";
              RestartSec = "10s";

              # Security hardening
              NoNewPrivileges = true;
              PrivateTmp = true;
              ProtectSystem = "strict";
              ProtectHome = true;
              ReadWritePaths = [ cfg.dataDir ];
              ProtectKernelTunables = true;
              ProtectKernelModules = true;
              ProtectControlGroups = true;
              ProtectHostname = true;
              ProtectClock = true;
              RestrictRealtime = true;
              RestrictSUIDSGID = true;
              RemoveIPC = true;
              PrivateDevices = true;
            } // lib.optionalAttrs (cfg.environmentFile != null) {
              EnvironmentFile = cfg.environmentFile;
            };

            environment = cfg.settings // {
              # Set data directories
              ZEFXI_DATA_DIR = cfg.dataDir;
              AVATAR_FILES_DIRECTORY = "${cfg.dataDir}/tdlib/photos";
              AVATAR_SERVER_PORT = toString cfg.avatarPort;
            };
          };

          services.caddy = mkIf cfg.caddy.enable {
            enable = true;
            
            virtualHosts."${cfg.caddy.domain}" = {
              extraConfig = ''
                reverse_proxy 127.0.0.1:${toString cfg.avatarPort}
              '';
            };
          };
        };
      };

    nixosModules.default = self.nixosModules.zefxi;
  } // (flake-utils.lib.eachDefaultSystem (system: let
    # Zig flake helper - using master version for better compatibility
    env = zig2nix.outputs.zig-env.${system} {
      zig = zig2nix.outputs.packages.${system}.zig-master;
    };
  in with builtins; with env.pkgs.lib; rec {
    # Produces clean binaries meant to be shipped outside of nix
    # nix build .#foreign
    packages.foreign = env.package {
      src = cleanSource ./.;

      # Packages required for compiling
      nativeBuildInputs = with env.pkgs; [
        pkg-config
      ];

      # Packages required for linking
      buildInputs = with env.pkgs; [
        tdlib
      ];

      # Smaller binaries and avoids shipping glibc.
      zigPreferMusl = true;
    };

    # nix build .
    packages.default = packages.foreign.override (attrs: {
      # Prefer nix friendly settings.
      zigPreferMusl = false;

      # Executables required for runtime
      # These packages will be added to the PATH
      zigWrapperBins = with env.pkgs; [];

      # Libraries required for runtime
      # These packages will be added to the LD_LIBRARY_PATH
      zigWrapperLibs = (attrs.buildInputs or []) ++ (with env.pkgs; [ glibc ]);
      
      # Ensure proper dynamic linking on NixOS
      zigWrapperArgs = [
        "--set LD_LIBRARY_PATH ${env.pkgs.lib.makeLibraryPath ([ env.pkgs.tdlib env.pkgs.glibc ] ++ (attrs.buildInputs or []))}"
      ];
    });

    # For bundling with nix bundle for running outside of nix
    # example: https://github.com/ralismark/nix-appimage
    apps.bundle = {
      type = "app";
      program = "${packages.foreign}/bin/zefxi";
    };

    # nix run .
    apps.default = env.app [] "zig build run -- \"$@\"";

    # nix run .#build
    apps.build = env.app [] "zig build \"$@\"";

    # nix run .#test
    apps.test = env.app [] "zig build test -- \"$@\"";

    # nix run .#docs
    apps.docs = env.app [] "zig build docs -- \"$@\"";

    # nix run .#zig2nix
    apps.zig2nix = env.app [] "zig2nix \"$@\"";

    # nix develop
    devShells.default = env.mkShell {
      # Packages required for compiling, linking and running
      # Libraries added here will be automatically added to the LD_LIBRARY_PATH and PKG_CONFIG_PATH
      nativeBuildInputs = []
        ++ packages.default.nativeBuildInputs
        ++ packages.default.buildInputs
        ++ packages.default.zigWrapperBins
        ++ packages.default.zigWrapperLibs;

      shellHook = ''
        source .env 2>/dev/null || true

        echo "Zefxi Development Environment"
        echo "================================"
        echo "Zig version: $(zig version)"
        echo "TDLib version: $(pkg-config --modversion tdjson 2>/dev/null || echo 'installed')"
        echo ""
        echo "Available commands:"
        echo "  nix run .                 - Run the bridge"
        echo "  nix run .#build           - Build the project"
        echo "  nix run .#test            - Run tests"
        echo "  nix run .#docs            - Generate docs"
        echo "  nix run .#zig2nix         - Run zig2nix tools"
        echo ""
        echo "Environment variables needed:"
        echo "  TELEGRAM_API_ID       - Your Telegram API ID"
        echo "  TELEGRAM_API_HASH     - Your Telegram API Hash"
        echo "  TELEGRAM_CHAT_ID      - Your Telegram chat ID"
        echo "  DISCORD_TOKEN         - Your Discord bot token"
        echo "  DISCORD_SERVER        - Your Discord server ID"
        echo "  DISCORD_CHANNEL       - Your Discord channel ID"
        echo "  DISCORD_WEBHOOK_URL   - Your Discord webhook URL (required for user spoofing)"
        echo ""
      '';
    };
  }));
} 