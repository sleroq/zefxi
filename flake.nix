{
  description = "Zefxi - TDLib Telegram Client in Zig";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    zig.url = "github:mitchellh/zig-overlay";
  };

  outputs = { self, nixpkgs, flake-utils, zig }:
    let
      buildZefxi = pkgs: pkgs.stdenv.mkDerivation {
        pname = "zefxi";
        version = "0.1.0";
        
        src = ./.;
        
        nativeBuildInputs = with pkgs; [
          zig.packages.${pkgs.system}.master
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
          description = "TDLib Telegram client in Zig - Telegram-Discord bridge with user spoofing";
          license = licenses.mit;
          platforms = platforms.linux ++ platforms.darwin;
        };
      };
    in
    {
      # NixOS module for zefxi
      nixosModules.zefxi = { config, lib, pkgs, ... }:
        with lib;
        let
          cfg = config.services.zefxi;
        in {
          options.services.zefxi = {
            enable = mkEnableOption "Zefxi Telegram-Discord Bridge";

            package = mkOption {
              type = types.package;
              default = buildZefxi pkgs;
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
    } // flake-utils.lib.eachDefaultSystem (system:
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
            echo "  TELEGRAM_CHAT_ID      - Your Telegram chat ID"
            echo "  DISCORD_TOKEN         - Your Discord bot token"
            echo "  DISCORD_SERVER        - Your Discord server ID"
            echo "  DISCORD_CHANNEL       - Your Discord channel ID"
            echo "  DISCORD_WEBHOOK_URL   - Your Discord webhook URL (required for user spoofing)"
            echo ""
          '';
        };

        # Package the application
        packages.default = buildZefxi pkgs;
      });
} 